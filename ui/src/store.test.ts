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
      return JSON.stringify({ id: request.id, result: await result(request.params) });
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
  "core.hello": () => ({ api_version: "1.4", core_version: "test" }),
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

  test("keeps a status that the core's events gave before the send's answer", async () => {
    // A message to an account without network fails at once: the core's
    // events can reach the store before the answer of messages.send, which
    // still says "sending" and must not undo the failure.
    const shell = scriptedShell({
      ...coreState([conversation("c1", "Adam")]),
      "messages.send": () => {
        const sending = message("m3", 3, { from_me: true, status: "sending", text: "hello" });
        shell.emit("message.added", { message: sending });
        shell.emit("message.updated", { message: { ...sending, status: "failed", error: "You're not logged in" } });
        return { message: sending };
      },
    });
    const store = new Store(shell.core);
    store.start();
    shell.status({ state: "ready" });
    await settle();
    await store.select("c1");

    await store.send("hello");
    expect(ids(store.getState().messages["c1"] ?? [])).toEqual(["m1:1:received", "m2:2:received", "m3:3:failed"]);
  });

  test("deselects a conversation, and keeps its messages", async () => {
    const shell = scriptedShell(coreState([conversation("c1", "Adam")]));
    const store = new Store(shell.core);
    store.start();
    shell.status({ state: "ready" });
    await settle();
    await store.select("c1");
    store.deselect();
    expect(store.getState().selected).toBeUndefined();
    expect(store.getState().messages["c1"]?.map((m) => m.message_id)).toEqual(["m1", "m2"]);
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

  test("starts over: forgets what it showed, and reads the new core once it is ready", async () => {
    let conversations = [conversation("c1", "Adam")];
    const shell = scriptedShell({ ...coreState(), "conversations.list": () => ({ conversations }) });
    const store = new Store(shell.core);
    expect(store.canStartOver).toBe(false);
    expect(await store.startOver()).toBe(false);

    let startedOver = 0;
    shell.core.startOver = () => {
      startedOver++;
      conversations = [];
      shell.status({ state: "starting" });
      shell.status({ state: "ready" });
      return Promise.resolve();
    };
    store.start();
    shell.status({ state: "ready" });
    await settle();
    expect(store.getState().conversations).toHaveLength(1);
    shell.status({ state: "failed", reason: "lost", keyLost: true });

    expect(store.canStartOver).toBe(true);
    expect(await store.startOver()).toBe(true);
    await settle();
    expect(startedOver).toBe(1);
    expect(store.getState()).toMatchObject({ status: { state: "ready" }, loaded: true, conversations: [] });

    shell.core.startOver = () => Promise.reject(new Error("refused: the core's key is not lost"));
    expect(await store.startOver()).toBe(false);
    expect(store.getState().error).toBe("Could not start over: refused: the core's key is not lost");
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

  test("marks a conversation read at once, then asks the core", async () => {
    const unread = { ...conversation("c1", "Adam", 2), last_message: message("m2", 2), unread_count: 2 };
    const marked = { ...conversation("c2", "Bea", 1), marked_unread: true };
    const calls: unknown[] = [];
    let answer: (() => void) | undefined;
    const shell = scriptedShell({
      ...coreState([unread, marked, conversation("c3", "Cy", 0)]),
      "conversations.markRead": (params) => {
        calls.push(params);
        return new Promise<object>((resolve) => (answer = () => resolve({})));
      },
    });
    const store = new Store(shell.core);
    store.start();
    shell.status({ state: "ready" });
    await settle();

    const pending = store.markRead("c1");
    // The badge goes before the core answers.
    const read = store.getState().conversations.find((c) => c.conversation_id === "c1");
    expect(read?.unread_count).toBeUndefined();
    expect(read?.last_message?.message_id).toBe("m2");
    await settle();
    expect(calls).toEqual([{ conversation_id: "c1", message_id: "m2" }]);
    answer?.();
    await pending;

    // No last message: the core reads up to its own last one.
    const second = store.markRead("c2");
    await settle();
    answer?.();
    await second;
    expect(store.getState().conversations.find((c) => c.conversation_id === "c2")?.marked_unread).toBeUndefined();
    expect(calls).toEqual([{ conversation_id: "c1", message_id: "m2" }, { conversation_id: "c2" }]);

    // A conversation already read, or unknown: no call.
    await store.markRead("c3");
    await store.markRead("c9");
    expect(calls).toHaveLength(2);
    expect(store.getState().error).toBeUndefined();
  });

  test("reports a failed mark as read", async () => {
    const shell = scriptedShell(coreState([{ ...conversation("c1", "Adam", 1), unread_count: 1 }]));
    const store = new Store(shell.core);
    store.start();
    shell.status({ state: "ready" });
    await settle();
    await store.markRead("c1");
    expect(store.getState().error).toMatch(/^Could not mark the conversation as read/);
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

describe("Store's events", () => {
  test("applies again the events received while it reads the state", async () => {
    let answer: (() => void) | undefined;
    const shell = scriptedShell({
      ...coreState([conversation("c1", "Adam", 1)]),
      // The account's state is read before its event arrives.
      "accounts.list": () =>
        new Promise((resolve) => {
          answer = () => resolve({ accounts: [{ account_id: "a1", network_id: "echo", name: "me", state: "connecting" }] });
        }),
    });
    const store = new Store(shell.core);
    store.start();
    shell.status({ state: "ready" });
    await settle();
    shell.emit("account.updated", { account: { account_id: "a1", network_id: "echo", name: "me", state: "transient_disconnect", error: "No network" } });
    shell.emit("conversation.updated", { conversation: conversation("c2", "Zoe", 2) });
    answer!();
    await settle();
    expect(store.getState().accounts).toEqual([{ account_id: "a1", network_id: "echo", name: "me", state: "transient_disconnect", error: "No network" }]);
    expect(store.getState().conversations.map((c) => c.name)).toEqual(["Zoe", "Adam"]);
  });

  test("keeps messages only for the conversations it read", async () => {
    const shell = scriptedShell(coreState([conversation("c1", "Adam", 1), conversation("c2", "Zoe", 2)]));
    const store = new Store(shell.core);
    store.start();
    shell.status({ state: "ready" });
    await settle();
    // c1 was never shown: its message moves it first, but its messages are
    // read, with their history, when it is.
    shell.emit("message.added", { message: message("m9", 9) });
    expect(store.getState().messages["c1"]).toBeUndefined();
    expect(store.getState().conversations[0]?.conversation_id).toBe("c1");
    // A message received while they are read is kept.
    const selecting = store.select("c1");
    shell.emit("message.added", { message: message("m10", 10) });
    await selecting;
    expect(ids(store.getState().messages["c1"]!)).toEqual(["m1:1:received", "m2:2:received", "m10:10:received"]);
    expect(store.getState().history["c1"]).toBe("start");
  });
});

describe("Store's changed messages", () => {
  test("quotes an edited or deleted message anew in its answers", async () => {
    const shell = scriptedShell({
      ...coreState([conversation("c1", "Adam", 3)]),
      "messages.list": () => ({
        messages: [
          message("m1", 1, { sender_name: "Adam" }),
          message("m2", 2, { reply_to: { message_id: "m1", sender_name: "Adam", text: "m1" } }),
          message("m3", 3),
        ],
      }),
    });
    const store = await started(shell);
    await store.select("c1");
    const replies = () => store.getState().messages["c1"]!.map((m) => m.reply_to);

    shell.emit("message.updated", { message: message("m1", 1, { sender_name: "Adam", text: "m1, edited", edited: true }) });
    expect(replies()).toEqual([undefined, { message_id: "m1", sender_name: "Adam", text: "m1, edited" }, undefined]);
    expect(store.getState().messages["c1"]![0]!.edited).toBe(true);

    shell.emit("message.updated", { message: message("m1", 1, { sender_name: "Adam", text: "", deleted: true }) });
    expect(replies()[1]).toEqual({ message_id: "m1", sender_name: "Adam", deleted: true });

    // A change to a message that nothing answers leaves the others as they are.
    const before = store.getState().messages["c1"]![1];
    shell.emit("message.updated", { message: message("m3", 3, { reactions: [{ key: "👍", count: 1 }] }) });
    expect(store.getState().messages["c1"]![1]).toBe(before);
  });
});

// A core that keeps the persons and answers persons.* like the real one
// (docs/ADR/0017): a conversation belongs to one person, linking moves it.
function personsCore(conversations: Conversation[], initial: Person[] = []) {
  let persons = initial.map((p) => ({ ...p }));
  const params: unknown[] = [];
  const take = (id: string) => {
    for (const p of persons) {
      p.conversation_ids = p.conversation_ids.filter((c) => c !== id);
    }
    persons = persons.filter((p) => p.conversation_ids.length > 0);
  };
  const shell = scriptedShell({
    ...coreState(conversations, initial),
    "persons.create": (p) => {
      params.push(p);
      const { conversation_ids } = p as { conversation_ids: string[] };
      conversation_ids.forEach(take);
      const person = { person_id: `h${persons.length + 10}`, name: "New", conversation_ids };
      persons.push(person);
      return { person };
    },
    "persons.link": (p) => {
      params.push(p);
      const { person_id, conversation_id } = p as { person_id: string; conversation_id: string };
      take(conversation_id);
      const person = persons.find((x) => x.person_id === person_id)!;
      person.conversation_ids.push(conversation_id);
      return { person: { ...person, conversation_ids: [...person.conversation_ids] } };
    },
    "persons.rename": (p) => {
      params.push(p);
      const { person_id, name } = p as { person_id: string; name: string };
      if (name.trim() === "") {
        throw new Error("invalid person name");
      }
      const person = persons.find((x) => x.person_id === person_id)!;
      person.name = name;
      return { person: { ...person } };
    },
    "persons.unlink": (p) => {
      params.push(p);
      return {};
    },
    "persons.delete": (p) => {
      params.push(p);
      return {};
    },
  });
  return { shell, params };
}

async function started(shell: ReturnType<typeof scriptedShell>) {
  const store = new Store(shell.core);
  store.start();
  shell.status({ state: "ready" });
  await settle();
  return store;
}

describe("Store's persons", () => {
  test("merges two conversations of no one into a new person, at once", async () => {
    const { shell, params } = personsCore([conversation("c1", "Alice"), conversation("c2", "Alice T.")]);
    const store = await started(shell);
    expect(await store.mergeWith("c1", "c2")).toBe(true);
    expect(params).toEqual([{ conversation_ids: ["c1", "c2"] }]);
    // Before the core's events: the person and its conversations are known.
    expect(store.getState().persons.map((p) => `${p.person_id}:${p.conversation_ids.join(",")}`)).toEqual(["h10:c1,c2"]);
    expect(store.getState().conversations.map((c) => c.person_id)).toEqual(["h10", "h10"]);
  });

  test("brings another person's conversations into the thread's person", async () => {
    const persons: Person[] = [
      { person_id: "h1", name: "Alice", conversation_ids: ["c1"] },
      { person_id: "h2", name: "Alice T.", conversation_ids: ["c2", "c3"] },
    ];
    const { shell, params } = personsCore(
      [conversation("c1", "Alice"), conversation("c2", "Alice T."), conversation("c3", "alice")].map((c, i) => ({ ...c, person_id: i === 0 ? "h1" : "h2" })),
      persons,
    );
    const store = await started(shell);
    expect(await store.mergeWith("c1", "c3")).toBe(true);
    expect(params).toEqual([
      { person_id: "h1", conversation_id: "c2" },
      { person_id: "h1", conversation_id: "c3" },
    ]);
    expect(store.getState().conversations.map((c) => c.person_id)).toEqual(["h1", "h1", "h1"]);
    expect(store.getState().persons.find((p) => p.person_id === "h1")?.conversation_ids).toEqual(["c1", "c2", "c3"]);
  });

  test("reports a merge the core refuses, and an unknown conversation", async () => {
    const shell = scriptedShell(coreState([conversation("c1", "Alice"), conversation("c2", "Bob")]));
    const store = await started(shell);
    expect(await store.mergeWith("c1", "nope")).toBe(false);
    expect(store.getState().error).toBeUndefined();
    expect(await store.mergeWith("c1", "c2")).toBe(false);
    expect(store.getState().error).toMatch(/^Could not merge the conversations: /);
  });

  test("renames, separates one conversation, and separates them all", async () => {
    const alice: Person = { person_id: "h1", name: "Alice", conversation_ids: ["c1", "c2"] };
    const { shell, params } = personsCore([conversation("c1", "Alice"), conversation("c2", "Alice T.")].map((c) => ({ ...c, person_id: "h1" })), [alice]);
    const store = await started(shell);
    expect(await store.renamePerson("h1", "Alice Martin")).toBe(true);
    expect(store.getState().persons[0]?.name).toBe("Alice Martin");
    expect(await store.renamePerson("h1", " ")).toBe(false);
    expect(store.getState().error).toMatch(/^Could not rename: /);
    await store.separateConversation("c2");
    await store.unmergePerson("h1");
    expect(params.slice(2)).toEqual([{ conversation_id: "c2" }, { person_id: "h1" }]);
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
