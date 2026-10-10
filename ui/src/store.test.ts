// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

// Unit tests of the store, with a scripted shell. The store against the real
// core is covered by the desktop app's end-to-end tests (apps/desktop/e2e).

import { describe, expect, test } from "vitest";
import type { Conversation, Message, Person } from "./core-api/types.gen";
import type { CoreStatus, ShellCore } from "./shell";
import { INBOX_PAGE, mergeConversations, mergeMessages, OLDER_PAGE, Store } from "./store";

function conversation(id: string, name: string, activity?: number): Conversation {
  return { conversation_id: id, account_id: "a1", network_id: "echo", name, kind: "direct", ...(activity === undefined ? {} : { activity_ms: activity }) };
}

function message(id: string, timestamp: number, patch: Partial<Message> = {}): Message {
  return {
    message_id: id,
    conversation_id: "c1",
    from_me: false,
    sender_name: "",
    timestamp_ms: timestamp,
    kind: "text",
    text: id,
    status: "received",
    ...patch,
  };
}

// A shell whose core answers from a table of results, and whose events and
// status the test emits.
function scriptedShell(results: Record<string, (params: unknown) => unknown>) {
  const events = new Set<(json: string) => void>();
  const statuses = new Set<(status: CoreStatus) => void>();
  const commands: string[] = [];
  const core: ShellCore = {
    async call(json) {
      const request = JSON.parse(json) as { id: number; command: string; params?: unknown };
      commands.push(request.command);
      const result = results[request.command];
      if (!result) {
        return JSON.stringify({ id: request.id, error: { code: "unknown_command", message: request.command } });
      }
      return JSON.stringify({ id: request.id, result: result(request.params) });
    },
    onEvent(listener) {
      events.add(listener);
      return () => events.delete(listener);
    },
    onStatus(listener) {
      statuses.add(listener);
      return () => statuses.delete(listener);
    },
  };
  return {
    core,
    commands,
    emit: (type: string, data: unknown) => events.forEach((l) => l(JSON.stringify({ type, data }))),
    status: (status: CoreStatus) => statuses.forEach((l) => l(status)),
  };
}

const coreState = (conversations: Conversation[] = [], persons: Person[] = []) => ({
  "core.hello": () => ({ api_version: "1.3", core_version: "test" }),
  "networks.list": () => ({ networks: [{ network_id: "echo", name: "Echo", login_flows: [] }] }),
  "accounts.list": () => ({ accounts: [] }),
  "conversations.list": () => ({ conversations }),
  "persons.list": () => ({ persons }),
  "messages.list": () => ({ messages: [message("m1", 1), message("m2", 2)] }),
  "messages.send": (params: unknown) => ({
    message: message("m3", 3, { from_me: true, status: "sending", text: (params as { text: string }).text }),
  }),
});

// Waits for the store's pending promises.
/** Each message as "id:time:status". */
const ids = (ms: readonly Message[]) => ms.map((m) => `${m.message_id}:${m.timestamp_ms}:${m.status}`);

const settle = () => new Promise((resolve) => setTimeout(resolve, 0));

describe("Store", () => {
  test("reads the state when the core is ready", async () => {
    const shell = scriptedShell(coreState([conversation("c2", "Zoe", 1), conversation("c1", "Adam", 2)]));
    const store = new Store(shell.core);
    store.start();
    expect(store.getState()).toMatchObject({ loaded: false, status: { state: "starting" } });
    shell.status({ state: "ready" });
    await settle();
    expect(store.getState()).toMatchObject({ loaded: true, coreVersion: "test", status: { state: "ready" } });
    // The most recently active first.
    expect(store.getState().conversations.map((c) => c.name)).toEqual(["Adam", "Zoe"]);
    expect(store.getState().inbox).toBe("end");
  });

  test("refuses a core of another major version", async () => {
    const shell = scriptedShell({ ...coreState(), "core.hello": () => ({ api_version: "2.0", core_version: "x" }) });
    const store = new Store(shell.core);
    store.start();
    shell.status({ state: "ready" });
    await settle();
    expect(store.getState().loaded).toBe(false);
    expect(store.getState().error).toMatch(/API 2\.0/);
  });

  test("applies events, and keeps one copy of each message", async () => {
    const shell = scriptedShell(coreState([conversation("c1", "Adam")]));
    const store = new Store(shell.core);
    store.start();
    shell.status({ state: "ready" });
    await settle();
    await store.select("c1");
    expect(store.getState().messages["c1"]?.map((m) => m.message_id)).toEqual(["m1", "m2"]);

    await store.send("hello");
    shell.emit("message.added", { message: message("m3", 3, { from_me: true, status: "sending", text: "hello" }) });
    shell.emit("message.updated", { message: message("m3", 3, { from_me: true, status: "sent", text: "hello" }) });
    shell.emit("message.added", { message: message("m4", 4, { text: "hello" }) });
    shell.emit("conversation.updated", { conversation: conversation("c0", "Aaron") });
    shell.emit("future.event", {});

    const state = store.getState();
    expect(state.messages["c1"]?.map((m) => `${m.message_id}:${m.status}`)).toEqual(["m1:received", "m2:received", "m3:sent", "m4:received"]);
    // Adam's conversation has a new last message: it comes first.
    expect(state.conversations.map((c) => c.name)).toEqual(["Adam", "Aaron"]);
    expect(state.conversations[0]).toMatchObject({ activity_ms: 4, last_message: { message_id: "m4" } });
  });

  test("reads the persons, and applies their events", async () => {
    const alice: Person = { person_id: "h1", name: "Alice", conversation_ids: ["c1"] };
    const shell = scriptedShell(coreState([conversation("c1", "Alice"), conversation("c2", "Alice")], [alice]));
    const store = new Store(shell.core);
    store.start();
    shell.status({ state: "ready" });
    await settle();
    expect(store.getState().persons).toEqual([alice]);

    shell.emit("person.updated", { person: { ...alice, conversation_ids: ["c1", "c2"] } });
    shell.emit("person.updated", { person: { person_id: "h2", name: "Bob", conversation_ids: ["c3"] } });
    shell.emit("conversation.updated", { conversation: { ...conversation("c2", "Alice"), person_id: "h1" } });
    expect(store.getState().persons.map((p) => `${p.person_id}:${p.conversation_ids.join(",")}`)).toEqual(["h1:c1,c2", "h2:c3"]);
    expect(store.getState().conversations.find((c) => c.conversation_id === "c2")?.person_id).toBe("h1");

    shell.emit("person.deleted", { person_id: "h1" });
    expect(store.getState().persons.map((p) => p.person_id)).toEqual(["h2"]);
  });

  test("logs out, and forgets a logged-out account", async () => {
    const alice = { account_id: "a1", network_id: "echo", name: "alice", state: "connected" };
    let accounts = [alice];
    const shell = scriptedShell({
      ...coreState([conversation("c1", "Adam")]),
      "accounts.list": () => ({ accounts }),
      "accounts.logout": () => {
        accounts = [];
        return {};
      },
    });
    const store = new Store(shell.core);
    store.start();
    shell.status({ state: "ready" });
    await settle();
    expect(store.getState().accounts).toHaveLength(1);

    shell.commands.length = 0;
    await store.logout("a1");
    expect(shell.commands.slice(0, 2)).toEqual(["accounts.logout", "core.hello"]);
    expect(store.getState().accounts).toEqual([]);

    // The event of the logout may come after the state was read again.
    shell.emit("account.updated", { account: { ...alice, state: "connecting" } });
    expect(store.getState().accounts).toHaveLength(1);
    shell.emit("account.updated", { account: { ...alice, state: "logged_out" } });
    expect(store.getState().accounts).toEqual([]);
  });

  test("reads everything again after a restart or when events were dropped", async () => {
    const shell = scriptedShell(coreState([conversation("c1", "Adam")]));
    const store = new Store(shell.core);
    store.start();
    shell.status({ state: "ready" });
    await settle();
    await store.select("c1");
    shell.status({ state: "restarting", attempt: 1 });
    expect(store.getState().status).toEqual({ state: "restarting", attempt: 1 });
    shell.commands.length = 0;
    shell.status({ state: "ready" });
    await settle();
    expect(shell.commands).toEqual(["core.hello", "networks.list", "accounts.list", "conversations.list", "persons.list", "messages.list"]);
    expect(store.getState().selected).toBe("c1");

    shell.commands.length = 0;
    shell.emit("resync.required", {});
    await settle();
    expect(shell.commands).toContain("conversations.list");
  });

  test("reports a failed send", async () => {
    const { "messages.send": _, ...withoutSend } = coreState([conversation("c1", "Adam")]);
    const shell = scriptedShell(withoutSend);
    const store = new Store(shell.core);
    store.start();
    shell.status({ state: "ready" });
    await settle();
    await store.select("c1");
    await store.send("hello");
    expect(store.getState().error).toMatch(/^Could not send the message/);
    store.dismissError();
    expect(store.getState().error).toBeUndefined();
  });

  test("reads older messages page by page, one page at a time", async () => {
    // 250 messages, m0 to m249; the core pages them from the newest.
    const all = Array.from({ length: 250 }, (_, i) => message(`m${i}`, i));
    const requests: unknown[] = [];
    const shell = scriptedShell({
      ...coreState([conversation("c1", "Adam")]),
      "messages.list": (params) => {
        requests.push(params);
        const { before, limit = 50 } = params as { before?: string; limit?: number };
        const end = before === undefined ? all.length : Number(before);
        const start = Math.max(0, end - limit);
        return { messages: all.slice(start, end), ...(start > 0 ? { before: String(start) } : {}) };
      },
    });
    const store = new Store(shell.core);
    store.start();
    shell.status({ state: "ready" });
    await settle();
    await store.select("c1");
    expect(store.getState().messages["c1"]).toHaveLength(50);
    expect(store.getState().history["c1"]).toEqual({ cursor: "200" });

    const first = store.loadOlder();
    expect(store.getState().history["c1"]).toBe("loading");
    await store.loadOlder(); // Ignored while the first page is read.
    await first;
    expect(store.getState().messages["c1"]?.[0]?.message_id).toBe("m100");
    expect(store.getState().messages["c1"]).toHaveLength(150);

    await store.loadOlder();
    const messages = store.getState().messages["c1"]!;
    expect(messages.map((m) => m.message_id)).toEqual(all.map((m) => m.message_id));
    expect(store.getState().history["c1"]).toBe("start");
    await store.loadOlder();
    expect(requests).toEqual([
      { conversation_id: "c1" },
      { conversation_id: "c1", before: "200", limit: OLDER_PAGE },
      { conversation_id: "c1", before: "100", limit: OLDER_PAGE },
    ]);
  });

  test("keeps the history cursor when reading older messages fails", async () => {
    let fail = false;
    const shell = scriptedShell({
      ...coreState([conversation("c1", "Adam")]),
      "messages.list": () => {
        if (fail) {
          throw new Error("unreachable");
        }
        return { messages: [message("m9", 9)], before: "9" };
      },
    });
    const store = new Store(shell.core);
    store.start();
    shell.status({ state: "ready" });
    await settle();
    await store.select("c1");
    fail = true;
    await store.loadOlder();
    expect(store.getState().history["c1"]).toEqual({ cursor: "9" });
    expect(store.getState().error).toMatch(/older messages/);
  });
});

// A core with n conversations, c1 the most recent, listed in pages.
function pagedCore(n: number) {
  let conversations = Array.from({ length: n }, (_, i) => conversation(`c${i + 1}`, `Contact ${i + 1}`, 1000 - i));
  const pages: (string | undefined)[] = [];
  const shell = scriptedShell({
    ...coreState(),
    "conversations.list": (params) => {
      const { limit, before } = params as { limit?: number; before?: string };
      pages.push(before);
      const from = before === undefined ? 0 : Number(before);
      const end = limit === undefined ? conversations.length : from + limit;
      return { conversations: conversations.slice(from, end), ...(end < conversations.length ? { before: String(end) } : {}) };
    },
  });
  return { shell, pages, setConversations: (next: Conversation[]) => (conversations = next) };
}

describe("Store's conversation list", () => {
  test("reads the conversations page by page, one page at a time", async () => {
    const { shell, pages } = pagedCore(120);
    const store = new Store(shell.core);
    store.start();
    shell.status({ state: "ready" });
    await settle();
    expect(store.getState().conversations).toHaveLength(INBOX_PAGE);
    expect(store.getState().inbox).toEqual({ cursor: "50" });

    const first = store.loadMoreConversations();
    void store.loadMoreConversations(); // Ignored: a page is being read.
    expect(store.getState().inbox).toBe("loading");
    await first;
    await store.loadMoreConversations();
    const state = store.getState();
    expect(state.conversations.map((c) => c.conversation_id)).toEqual(Array.from({ length: 120 }, (_, i) => `c${i + 1}`));
    expect(state.inbox).toBe("end");
    expect(pages).toEqual([undefined, "50", "100"]);
    await store.loadMoreConversations(); // Nothing more.
    expect(pages).toHaveLength(3);
  });

  test("reads as many conversations again after a restart", async () => {
    const { shell } = pagedCore(120);
    const store = new Store(shell.core);
    store.start();
    shell.status({ state: "ready" });
    await settle();
    await store.loadMoreConversations();
    expect(store.getState().conversations).toHaveLength(100);
    await store.select("c90");
    await store.reload();
    expect(store.getState().conversations).toHaveLength(100);
    expect(store.getState().selected).toBe("c90");
  });

  test("moves a conversation first on a new message, and reads one it did not have", async () => {
    const { shell, pages, setConversations } = pagedCore(60);
    const store = new Store(shell.core);
    store.start();
    shell.status({ state: "ready" });
    await settle();
    const conversationIds = () => store.getState().conversations.map((c) => c.conversation_id);

    shell.emit("message.added", { message: message("m1", 2000, { conversation_id: "c3" }) });
    expect(conversationIds().slice(0, 3)).toEqual(["c3", "c1", "c2"]);
    shell.emit("message.updated", { message: message("m1", 2000, { conversation_id: "c3", text: "edited" }) });
    expect(store.getState().conversations[0]?.last_message?.text).toBe("edited");
    // An update of another message leaves the last one.
    shell.emit("message.updated", { message: message("m0", 10, { conversation_id: "c3", text: "older" }) });
    expect(store.getState().conversations[0]?.last_message?.text).toBe("edited");

    // c60 is beyond the first page: its message makes the store read the
    // first page again, where it now is.
    setConversations([conversation("c60", "Contact 60", 3000), conversation("c3", "Contact 3", 2000), conversation("c1", "Contact 1", 1000)]);
    shell.emit("message.added", { message: message("m2", 3000, { conversation_id: "c60" }) });
    await settle();
    expect(pages).toEqual([undefined, undefined]);
    expect(conversationIds().slice(0, 3)).toEqual(["c60", "c3", "c1"]);
    expect(conversationIds()).toHaveLength(51);
  });
});

describe("mergeConversations", () => {
  test("keeps the most recent version of each conversation, sorted by activity", () => {
    const merged = mergeConversations(
      [conversation("c1", "One", 5), conversation("c2", "Two", 3)],
      [conversation("c2", "Two (page)", 9), conversation("c1", "One (stale page)", 4), conversation("c3", "Three", 1)],
    );
    expect(merged.map((c) => c.name)).toEqual(["Two (page)", "One", "Three"]);
  });
});

describe("mergeMessages", () => {
  test("orders by time and lets the newer version win", () => {
    const merged = mergeMessages([message("b", 2), message("a", 1, { status: "sending" })], [message("a", 1, { status: "sent" }), message("c", 3)]);
    expect(merged.map((m) => `${m.message_id}:${m.status}`)).toEqual(["a:sent", "b:received", "c:received"]);
  });

  test("handles one message, as events bring them", () => {
    const base = [message("a", 1), message("b", 2), message("c", 3)];
    // A new message, the newest.
    expect(ids(mergeMessages(base, [message("d", 4)]))).toEqual(["a:1:received", "b:2:received", "c:3:received", "d:4:received"]);
    // Of the same millisecond as the newest: after it, as the core sent it.
    expect(ids(mergeMessages(base, [message("d", 3)]))).toEqual(["a:1:received", "b:2:received", "c:3:received", "d:3:received"]);
    // An update.
    expect(ids(mergeMessages(base, [message("b", 2, { status: "sent" })]))).toEqual(["a:1:received", "b:2:sent", "c:3:received"]);
    // An update that changes the time, and an older message.
    expect(ids(mergeMessages(base, [message("c", 0)]))).toEqual(["c:0:received", "a:1:received", "b:2:received"]);
    expect(ids(mergeMessages(base, [message("z", 0)]))).toEqual(["z:0:received", "a:1:received", "b:2:received", "c:3:received"]);
    expect(ids(mergeMessages([], [message("a", 1)]))).toEqual(["a:1:received"]);
    // The input is never changed.
    expect(ids(base)).toEqual(["a:1:received", "b:2:received", "c:3:received"]);
  });

  test("finds an update far from the end", () => {
    const base = Array.from({ length: 500 }, (_, i) => message(`m${i}`, i));
    const merged = mergeMessages(base, [message("m3", 3, { status: "failed" })]);
    expect(merged).toHaveLength(500);
    expect(merged[3]?.status).toBe("failed");
  });
});
