// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

// The shell of the mobile app (apps/mobile): the core runs in the app's
// process, behind the MusubeeCore Capacitor plugin (ADR 0011). This module
// gives the interface the same window.musubee as the desktop's preload.

import { registerPlugin, type PluginListenerHandle } from "@capacitor/core";
import type { CoreStatus, Shell } from "./shell";

/** The plugin of apps/mobile/android (MusubeeCorePlugin.kt). */
export interface MusubeeCorePlugin {
  /** Resolves with the core's response, or rejects when the core is not available. */
  call(options: { request: object }): Promise<object>;
  addListener(eventName: "event", listener: (event: object) => void): Promise<PluginListenerHandle>;
}

/**
 * Builds the shell over the plugin. The core starts with the app and lives
 * in its process: it is "starting" until it answers a first request, then
 * "ready" (or "failed"), and never restarts on its own.
 */
export function pluginShell(plugin: MusubeeCorePlugin): Shell {
  const events = new Set<(event: string) => void>();
  const statuses = new Set<(status: CoreStatus) => void>();
  let status: CoreStatus = { state: "starting" };

  function setStatus(next: CoreStatus) {
    status = next;
    for (const listener of statuses) {
      listener(status);
    }
  }

  // One listener for the page's whole life, from the start: the store reads
  // the state again once the core is ready, so earlier events need no replay.
  void plugin.addListener("event", (event) => {
    const json = JSON.stringify(event);
    for (const listener of events) {
      listener(json);
    }
  });

  const call = async (request: string) => JSON.stringify(await plugin.call({ request: JSON.parse(request) as object }));

  // The plugin waits for the core's service: a first answer means it is ready.
  call(JSON.stringify({ id: 0, command: "core.hello" })).then(
    () => setStatus({ state: "ready" }),
    (error: unknown) => setStatus({ state: "failed", reason: error instanceof Error ? error.message : String(error) }),
  );

  return {
    core: {
      call,
      onEvent(listener) {
        events.add(listener);
        return () => events.delete(listener);
      },
      onStatus(listener) {
        statuses.add(listener);
        listener(status);
        return () => statuses.delete(listener);
      },
    },
  };
}

/** The shell of the mobile app. */
export function capacitorShell(): Shell {
  return pluginShell(registerPlugin<MusubeeCorePlugin>("MusubeeCore"));
}
