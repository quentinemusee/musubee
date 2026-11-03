// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

import { useId, useState, type KeyboardEvent } from "react";
import type { Conversation } from "../core-api/types.gen";
import { isMuted, MUTE_CHOICES, mutedLabel, muteUntil } from "../mute";
import { useNow } from "./Inbox";
import { useStore } from "./state";

/**
 * The thread's mute control (docs/ADR/0050): "Mute…" opens the choices;
 * a muted conversation says until when and offers "Unmute".
 */
export function MuteButton({ conversation }: { conversation: Conversation }) {
  const store = useStore();
  const now = useNow();
  const [open, setOpen] = useState(false);
  const choicesId = useId();
  const id = conversation.conversation_id;

  if (isMuted(conversation, now)) {
    return (
      <span className="mute">
        <span className="mute-state">{mutedLabel(conversation.muted_until_ms ?? 0, now)}</span>
        <button type="button" className="secondary" onClick={() => void store.mute(id, 0)}>
          Unmute
        </button>
      </span>
    );
  }
  const onKeyDown = (event: KeyboardEvent) => {
    if (event.key === "Escape") {
      setOpen(false);
    }
  };
  return (
    <span className="mute">
      <button type="button" className="secondary" aria-expanded={open} aria-controls={choicesId} onClick={() => setOpen(!open)} onKeyDown={onKeyDown}>
        Mute…
      </button>
      {open ? (
        <fieldset id={choicesId} className="mute-choices">
          <legend className="visually-hidden">Mute notifications</legend>
          {MUTE_CHOICES.map((choice) => (
            <button
              key={choice.label}
              type="button"
              className="secondary"
              onKeyDown={onKeyDown}
              onClick={() => {
                setOpen(false);
                void store.mute(id, muteUntil(choice.durationMs, Date.now()));
              }}
            >
              {choice.label}
            </button>
          ))}
        </fieldset>
      ) : null}
    </span>
  );
}
