// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

// The mobile shell over a scripted plugin. The real plugin and core are
// covered by the Android app's end-to-end test (apps/mobile/e2e).

import { describe, expect, test } from "vitest";
import { pluginShell, type MusubeeCorePlugin } from "./capacitor";
import type { CoreStatus } from "./shell";

function scriptedPlugin(
  answer: (request: { id: number; command: string }) => Promise<object>,
  startOver: () => Promise<void> = () => Promise.reject(new Error("not scripted")),
) {
  const listeners = new Map<string, (event: never) => void>();
  let granted = false;
  const plugin: MusubeeCorePlugin = {
    call: ({ request }) => answer(request as { id: number; command: string }),
    startOver,
    askToNotify: () => Promise.resolve({ granted }),
    addListener: (eventName: string, listener: (event: never) => void) => {
      listeners.set(eventName, listener);
      return Promise.resolve({ remove: () => Promise.resolve() });
    },
  };
  const send = (eventName: string, event: object) => (listeners.get(eventName) as ((event: object) => void) | undefined)?.(event);
  return {
    plugin,
    emit: (event: object) => send("event", event),
    tap: (conversationId: string) => send("openConversation", { conversation_id: conversationId }),
    grant: () => (granted = true),
  };
}

const settle = () => new Promise((resolve) => setTimeout(resolve, 0));

describe("pluginShell", () => {
  test("is ready once the core answers, and carries requests and events as JSON", async () => {
    const { plugin, emit } = scriptedPlugin((request) => Promise.resolve({ id: request.id, result: { command: request.command } }));
    const shell = pluginShell(plugin);
    const statuses: CoreStatus[] = [];
    shell.core.onStatus((status) => statuses.push(status));
    expect(statuses).toEqual([{ state: "starting" }]);
    await settle();
    expect(statuses).toEqual([{ state: "starting" }, { state: "ready" }]);

    expect(JSON.parse(await shell.core.call(JSON.stringify({ id: 7, command: "networks.list" })))).toEqual({
      id: 7,
      result: { command: "networks.list" },
    });
    const events: string[] = [];
    const stop = shell.core.onEvent((event) => events.push(event));
    emit({ type: "account.updated", data: { n: 1 } });
    stop();
    emit({ type: "account.updated", data: { n: 2 } });
    expect(events).toEqual(['{"type":"account.updated","data":{"n":1}}']);
  });

  test("fails when the core is not available", async () => {
    const { plugin } = scriptedPlugin(() => Promise.reject(new Error("The core is not available: timeout")));
    const shell = pluginShell(plugin);
    await settle();
    const statuses: CoreStatus[] = [];
    shell.core.onStatus((status) => statuses.push(status));
    expect(statuses).toEqual([{ state: "failed", reason: "The core is not available: timeout" }]);
  });

  test("reports a lost key, and starts over on request", async () => {
    // Capacitor rejects with an Error that carries the plugin's code.
    const lost = Object.assign(new Error("The core's key is lost: the database was written with another key"), { code: "KEY_LOST" });
    let startedOver = false;
    const { plugin } = scriptedPlugin(
      (request) => (startedOver ? Promise.resolve({ id: request.id, result: {} }) : Promise.reject(lost)),
      () => {
        startedOver = true;
        return Promise.resolve();
      },
    );
    const shell = pluginShell(plugin);
    const statuses: CoreStatus[] = [];
    shell.core.onStatus((status) => statuses.push(status));
    await settle();
    expect(statuses.at(-1)).toEqual({ state: "failed", reason: lost.message, keyLost: true });

    await shell.core.startOver?.();
    expect(statuses.slice(-2)).toEqual([{ state: "starting" }, { state: "ready" }]);
  });

  test("opens tapped notifications, the one that started the app included, and passes the user's answer on", async () => {
    const { plugin, tap, grant } = scriptedPlugin((request) => Promise.resolve({ id: request.id, result: {} }));
    const shell = pluginShell(plugin);
    tap("c1"); // Before the interface listens: the tap that started the app.
    const opened: string[] = [];
    const stop = shell.notifications!.onOpen((id) => opened.push(id));
    tap("c2");
    stop();
    tap("c3");
    expect(opened).toEqual(["c1", "c2"]);

    expect(await shell.notifications!.ask()).toBe(false);
    grant();
    expect(await shell.notifications!.ask()).toBe(true);
  });
});
