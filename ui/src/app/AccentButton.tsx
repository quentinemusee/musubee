// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

import { useId, useState, type KeyboardEvent } from "react";
import { ACCENT_NAMES, ACCENTS } from "../appearance";
import { useAppState, useStore } from "./state";

/**
 * The thread's color (docs/ADR/0063): the app's accent, or one of the
 * same accents for this conversation, or for this person in all their
 * conversations.
 */
export function AccentButton({ accentKey }: { accentKey: string }) {
  const store = useStore();
  const accent = useAppState((s) => s.accents?.[accentKey]);
  const [open, setOpen] = useState(false);
  const choicesId = useId();
  const onKeyDown = (event: KeyboardEvent) => {
    if (event.key === "Escape") {
      setOpen(false);
    }
  };
  return (
    <span className="mute">
      <button type="button" className="secondary" aria-expanded={open} aria-controls={choicesId} onClick={() => setOpen(!open)} onKeyDown={onKeyDown}>
        Color…
      </button>
      {open ? (
        <fieldset id={choicesId} className="mute-choices">
          <legend className="visually-hidden">Color of this conversation</legend>
          <label className="settings-choice">
            <input type="radio" name={choicesId} checked={accent === undefined} onChange={() => store.setAccent(accentKey, undefined)} onKeyDown={onKeyDown} />
            Same as the app
          </label>
          {ACCENTS.map((value) => (
            <label key={value} className="settings-choice">
              <input type="radio" name={choicesId} checked={accent === value} onChange={() => store.setAccent(accentKey, value)} onKeyDown={onKeyDown} />
              <span className="accent-swatch" data-accent={value} aria-hidden="true" />
              {ACCENT_NAMES[value]}
            </label>
          ))}
        </fieldset>
      ) : null}
    </span>
  );
}
