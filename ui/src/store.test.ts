// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

// Unit tests of the store, with a scripted shell. The store against the real
// core is covered by the desktop app's end-to-end tests (apps/desktop/e2e).

import { describe, expect, test } from "vitest";
import type { Conversation, Message } from "./core-api/types.gen";
import type { CoreStatus, ShellCore } from "./shell";
import { mergeMessages, Store } from "./store";

function conversation(id: string, name: string): Conversation {
  return { conversation_id: id, account_id: "a1", network_id: "echo", name, kind: "direct" };
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

const coreState = (conversations: Conversation[] = []) => ({
  "core.hello": () => ({ api_version: "1.0", core_version: "test" }),
  "networks.list": () => ({ networks: [{ network_id: "echo", name: "Echo", login_flows: [] }] }),
  "accounts.list": () => ({ accounts: [] }),
  "conversations.list": () => ({ conversations }),
  "messages.list": () => ({ messages: [message("m1", 1), message("m2", 2)] }),
  "messages.send": (params: unknown) => ({
    message: message("m3", 3, { from_me: true, status: "sending", text: (params as { text: string }).text }),
  }),
});

// Waits for the store's pending promises.
const settle = () => new Promise((resolve) => setTimeout(resolve, 0));

describe("Store", () => {
  test("reads the state when the core is ready", async () => {
    const shell = scriptedShell(coreState([conversation("c2", "Zoe"), conversation("c1", "Adam")]));
    const store = new Store(shell.core);
    store.start();
    expect(store.getState()).toMatchObject({ loaded: false, status: { state: "starting" } });
    shell.status({ state: "ready" });
    await settle();
    expect(store.getState()).toMatchObject({ loaded: true, coreVersion: "test", status: { state: "ready" } });
    expect(store.getState().conversations.map((c) => c.name)).toEqual(["Adam", "Zoe"]);
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
    expect(state.conversations.map((c) => c.name)).toEqual(["Aaron", "Adam"]);
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
    expect(shell.commands).toEqual(["core.hello", "networks.list", "accounts.list", "conversations.list", "messages.list"]);
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
});

describe("mergeMessages", () => {
  test("orders by time and lets the newer version win", () => {
    const merged = mergeMessages([message("b", 2), message("a", 1, { status: "sending" })], [message("a", 1, { status: "sent" }), message("c", 3)]);
    expect(merged.map((m) => `${m.message_id}:${m.status}`)).toEqual(["a:sent", "b:received", "c:received"]);
  });
});
