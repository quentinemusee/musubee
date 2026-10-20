// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

// Notifications of new messages, where the shell posts them (docs/ADR/0033).

import type { ShellNotifications } from "./shell";
import type { Store } from "./store";

/**
 * Asks for notifications once the user has an account, the first time the
 * page sees one (a notification means nothing before), and opens the
 * conversation of each notification the user taps. Returns a function that
 * stops both.
 */
export function followNotifications(store: Store, notifications: ShellNotifications, open: (conversationId: string) => void): () => void {
  let asked = false;
  const askOnce = () => {
    if (asked || store.getState().accounts.length === 0) {
      return;
    }
    asked = true;
    stopAsking();
    // A refusal is the user's answer: Android asks again only a few times.
    notifications.ask().catch(() => undefined);
  };
  const stopAsking = store.subscribe(askOnce);
  askOnce();
  const stopOpening = notifications.onOpen(open);
  return () => {
    stopAsking();
    stopOpening();
  };
}
