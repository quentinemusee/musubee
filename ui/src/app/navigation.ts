// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

// Moving between the inbox and a thread. On a narrow screen (a phone) they
// take the whole screen in turn: opening a thread adds a history entry, so
// that the system's back gesture returns to the inbox (MainActivity hands
// it to the web view on Android).

import { useEffect } from "react";
import type { Store } from "../store";

/** Where the layout shows the inbox or the thread alone; styles.css uses the same width. */
export const NARROW = "(max-width: 720px)";

const THREAD = "musubee.thread";

function inThreadEntry(): boolean {
  return (history.state as { view?: string } | null)?.view === THREAD;
}

/** Opens a conversation from the inbox. */
export function openConversation(store: Store, conversationId: string): void {
  void store.select(conversationId);
  if (window.matchMedia(NARROW).matches && !inThreadEntry()) {
    history.pushState({ view: THREAD }, "");
  }
}

/** Returns to the inbox. */
export function closeConversation(store: Store): void {
  if (inThreadEntry()) {
    history.back(); // popstate deselects.
  } else {
    store.deselect();
  }
}

/** Returns to the inbox when the user goes back from a thread. */
export function useBackToInbox(store: Store): void {
  useEffect(() => {
    const onPopState = () => {
      if (!inThreadEntry()) {
        store.deselect();
      }
    };
    window.addEventListener("popstate", onPopState);
    return () => window.removeEventListener("popstate", onPopState);
  }, [store]);
}
