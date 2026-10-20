// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

// When the interface asks for notifications, and what a tap opens. The
// notifications themselves are covered by the Android app's tests
// (NotificationTest).

import { describe, expect, test } from "vitest";
import type { Account } from "./core-api/types.gen";
import type { CoreStatus, ShellCore, ShellNotifications } from "./shell";
import { followNotifications } from "./notifications";
import { Store } from "./store";

const alice: Account = { account_id: "a1", network_id: "echo", name: "alice", state: "connected" };

// A core whose accounts the test changes, and a shell that counts its questions.
function setup(initialAccounts: Account[]) {
  let accounts = initialAccounts;
  const events = new Set<(json: string) => void>();
  const statuses = new Set<(status: CoreStatus) => void>();
  const results: Record<string, () => unknown> = {
    "core.hello": () => ({ api_version: "1.6", core_version: "test" }),
    "networks.list": () => ({ networks: [] }),
    "accounts.list": () => ({ accounts }),
    "conversations.list": () => ({ conversations: [] }),
    "persons.list": () => ({ persons: [] }),
    "messages.list": () => ({ messages: [] }),
  };
  const core: ShellCore = {
    call: (json) => {
      const request = JSON.parse(json) as { id: number; command: string };
      return Promise.resolve(JSON.stringify({ id: request.id, result: results[request.command]?.() }));
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
  const opens = new Set<(id: string) => void>();
  let asked = 0;
  const notifications: ShellNotifications = {
    ask: () => {
      asked++;
      return Promise.resolve(false);
    },
    onOpen(listener) {
      opens.add(listener);
      return () => opens.delete(listener);
    },
  };
  const store = new Store(core);
  store.start();
  return {
    store,
    notifications,
    asked: () => asked,
    ready: () => statuses.forEach((l) => l({ state: "ready" })),
    addAccount: (account: Account) => {
      accounts = [account];
      events.forEach((l) => l(JSON.stringify({ type: "account.updated", data: { account } })));
    },
    tap: (id: string) => opens.forEach((l) => l(id)),
  };
}

const settle = () => new Promise((resolve) => setTimeout(resolve, 0));

describe("followNotifications", () => {
  test("asks once the first account is added, and only once", async () => {
    const s = setup([]);
    followNotifications(s.store, s.notifications, () => undefined);
    s.ready();
    await settle();
    expect(s.asked()).toBe(0);

    s.addAccount(alice);
    await settle();
    expect(s.asked()).toBe(1);
    s.addAccount({ ...alice, account_id: "a2" });
    await settle();
    expect(s.asked()).toBe(1);
  });

  test("asks when the app starts with an account, and opens what the user taps until stopped", async () => {
    const s = setup([alice]);
    const opened: string[] = [];
    const stop = followNotifications(s.store, s.notifications, (id) => opened.push(id));
    s.ready();
    await settle();
    expect(s.asked()).toBe(1);

    s.tap("c1");
    stop();
    s.tap("c2");
    expect(opened).toEqual(["c1"]);
  });
});
