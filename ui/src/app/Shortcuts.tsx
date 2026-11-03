// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

import { useEffect } from "react";
import { inboxEntries } from "../inbox";
import { adjacentConversation, shortcutOf } from "../shortcuts";
import { openConversation } from "./navigation";
import { useStore } from "./state";

/** The id of the search field, which Ctrl+K focuses. */
export const SEARCH_FIELD_ID = "search-messages";

/**
 * Listens for the app's keyboard shortcuts (docs/ADR/0058) while it runs.
 * A shortcut does nothing while a modal dialog is open: the dialog keeps
 * the keyboard.
 */
export function Shortcuts() {
  const store = useStore();
  useEffect(() => {
    const mac = /mac|iphone|ipad/i.test(navigator.platform);
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.defaultPrevented || document.querySelector("dialog[open]") !== null) {
        return;
      }
      const shortcut = shortcutOf(event, mac);
      if (shortcut === null) {
        return;
      }
      event.preventDefault();
      if (shortcut === "search") {
        document.getElementById(SEARCH_FIELD_ID)?.focus();
        return;
      }
      const state = store.getState();
      const target = adjacentConversation(inboxEntries(state.conversations, state.persons), state.selected, shortcut);
      if (target !== undefined) {
        openConversation(store, target);
      }
    };
    window.addEventListener("keydown", onKeyDown);
    return () => window.removeEventListener("keydown", onKeyDown);
  }, [store]);
  return null;
}
