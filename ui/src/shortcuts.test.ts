// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

import { describe, expect, test } from "vitest";
import type { Conversation } from "./core-api/types.gen";
import type { InboxEntry } from "./inbox";
import { adjacentConversation, shortcutOf } from "./shortcuts";

const press = (key: string, mods: Partial<{ ctrlKey: boolean; metaKey: boolean; altKey: boolean; shiftKey: boolean }> = {}) => ({
  key,
  ctrlKey: false,
  metaKey: false,
  altKey: false,
  shiftKey: false,
  ...mods,
});

describe("shortcutOf", () => {
  test("Ctrl+K, or Cmd+K on macOS, goes to the search", () => {
    expect(shortcutOf(press("k", { ctrlKey: true }), false)).toBe("search");
    expect(shortcutOf(press("K", { ctrlKey: true }), false)).toBe("search");
    expect(shortcutOf(press("k", { metaKey: true }), true)).toBe("search");
    expect(shortcutOf(press("k", { metaKey: true }), false)).toBeNull();
    expect(shortcutOf(press("k", { ctrlKey: true }), true)).toBeNull();
    expect(shortcutOf(press("k", { ctrlKey: true, shiftKey: true }), false)).toBeNull();
    expect(shortcutOf(press("k"), false)).toBeNull();
  });

  test("Alt+Up and Alt+Down move between conversations", () => {
    expect(shortcutOf(press("ArrowUp", { altKey: true }), false)).toBe("previous");
    expect(shortcutOf(press("ArrowDown", { altKey: true }), true)).toBe("next");
    expect(shortcutOf(press("ArrowDown"), false)).toBeNull();
    expect(shortcutOf(press("ArrowDown", { altKey: true, ctrlKey: true }), false)).toBeNull();
  });
});

function entry(...ids: string[]): InboxEntry {
  const conversations = ids.map((id): Conversation => ({ conversation_id: id, account_id: "a1", network_id: "echo", name: id, kind: "direct" }));
  return { key: `c.${ids[0]}`, name: ids[0]!, conversations, unread: 0, markedUnread: false };
}

describe("adjacentConversation", () => {
  const entries = [entry("c1"), entry("c2", "c2b"), entry("c3")];

  test("opens the entry above or below, by its latest conversation", () => {
    expect(adjacentConversation(entries, "c1", "next")).toBe("c2");
    expect(adjacentConversation(entries, "c2b", "next")).toBe("c3");
    expect(adjacentConversation(entries, "c3", "previous")).toBe("c2");
  });

  test("starts at the first entry, and stops at the ends", () => {
    expect(adjacentConversation(entries, undefined, "next")).toBe("c1");
    expect(adjacentConversation(entries, "gone", "previous")).toBe("c1");
    expect(adjacentConversation(entries, "c1", "previous")).toBeUndefined();
    expect(adjacentConversation(entries, "c3", "next")).toBeUndefined();
    expect(adjacentConversation([], undefined, "next")).toBeUndefined();
  });
});
