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
  /**
   * The shell gave up starting the core. keyLost: the core's key is lost
   * for good (docs/ADR/0031), and only startOver can give a working core.
   */
  | { state: "failed"; reason: string; keyLost?: true };

export interface ShellCore {
  /** Sends one JSON request of the core API and resolves with the JSON response. */
  call(request: string): Promise<string>;
  /** Calls listener with each JSON event of the core; returns a function that stops it. */
  onEvent(listener: (event: string) => void): () => void;
  /** Calls listener with the current status, then with every change; returns a function that stops it. */
  onStatus(listener: (status: CoreStatus) => void): () => void;
  /**
   * Once the core's key is lost: sets the core's unreadable data aside
   * (never deleted) and starts a new, empty core with a new key. Resolves
   * once it is done; the status then goes through "starting" again.
   */
  startOver?(): Promise<void>;
}

/**
 * Notifications of new messages, where the shell posts them (the Android
 * app, docs/ADR/0033). The shell decides which messages to announce; the
 * interface asks for the permission and opens what the user taps.
 */
export interface ShellNotifications {
  /** Asks the user to allow notifications, once they have an account; resolves with their answer. */
  ask(): Promise<boolean>;
  /** Calls listener with the conversation of each notification the user taps; returns a function that stops it. */
  onOpen(listener: (conversationId: string) => void): () => void;
}

export interface Shell {
  core: ShellCore;
  notifications?: ShellNotifications;
}

declare global {
  interface Window {
    musubee?: Shell;
  }
}
