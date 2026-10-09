// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

// What the app shell gives the interface as window.musubee: the Electron
// preload on the desktop (apps/desktop, docs/ADR/0013), later the Capacitor
// plugin on mobile. The interface never sees how the core is reached.

/** The state of the core as the shell sees it. */
export type CoreStatus =
  | { state: "starting" }
  | { state: "ready" }
  /** The core stopped unexpectedly; the shell starts it again. */
  | { state: "restarting"; attempt: number }
  /** The shell gave up starting the core. */
  | { state: "failed"; reason: string };

export interface ShellCore {
  /** Sends one JSON request of the core API and resolves with the JSON response. */
  call(request: string): Promise<string>;
  /** Calls listener with each JSON event of the core; returns a function that stops it. */
  onEvent(listener: (event: string) => void): () => void;
  /** Calls listener with the current status, then with every change; returns a function that stops it. */
  onStatus(listener: (status: CoreStatus) => void): () => void;
}

export interface Shell {
  core: ShellCore;
}

declare global {
  interface Window {
    musubee?: Shell;
  }
}
