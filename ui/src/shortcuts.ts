// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

// Keyboard shortcuts of the app (docs/ADR/0058): pure functions, so that
// the tests need no DOM; app/Shortcuts.tsx listens for them.

import type { InboxEntry } from "./inbox";

export type Shortcut = "search" | "previous" | "next";

/** The parts of a keydown event the shortcuts look at. */
export interface KeyPress {
  key: string;
  ctrlKey: boolean;
  metaKey: boolean;
  altKey: boolean;
  shiftKey: boolean;
}

/**
 * The shortcut a key press asks for, or null: Ctrl+K (Cmd+K on macOS) goes
 * to the search field; Alt+Up and Alt+Down open the previous and next
 * conversation of the inbox. Alt with an arrow moves nothing in a text
 * field, so they work while writing.
 */
export function shortcutOf(press: KeyPress, mac: boolean): Shortcut | null {
  const command = mac ? press.metaKey && !press.ctrlKey : press.ctrlKey && !press.metaKey;
  if (command && !press.altKey && !press.shiftKey && press.key.toLowerCase() === "k") {
    return "search";
  }
  if (press.altKey && !press.ctrlKey && !press.metaKey && !press.shiftKey) {
    if (press.key === "ArrowUp") {
      return "previous";
    }
    if (press.key === "ArrowDown") {
      return "next";
    }
  }
  return null;
}

/**
 * The conversation to open for "previous" or "next": the most recently
 * active conversation of the entry above or below the current one, in the
 * inbox's order; the first entry when none is open; nothing past the ends.
 */
export function adjacentConversation(entries: readonly InboxEntry[], selected: string | undefined, shortcut: "previous" | "next"): string | undefined {
  const index = selected === undefined ? -1 : entries.findIndex((e) => e.conversations.some((c) => c.conversation_id === selected));
  const target = index < 0 ? entries[0] : entries[shortcut === "next" ? index + 1 : index - 1];
  return target?.conversations[0]?.conversation_id;
}
