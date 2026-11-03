// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

import { useEffect, useMemo, useRef, useState } from "react";
import type { Message } from "../core-api/types.gen";
import { normalizeName } from "../merge";
import { useAppState, useStore } from "./state";

/**
 * Chooses where to forward a message (docs/ADR/0054), in a modal dialog:
 * every conversation the inbox has read, any network, filtered by name.
 * The copy goes at once; the dialog says where it went, or why it could
 * not.
 */
export function ForwardMessage({ message, onClose }: { message: Message; onClose: () => void }) {
  const store = useStore();
  const conversations = useAppState((s) => s.conversations);
  const networks = useAppState((s) => s.networks);
  const dialog = useRef<HTMLDialogElement>(null);
  const [filter, setFilter] = useState("");
  const [sending, setSending] = useState<string | undefined>(undefined);
  const [result, setResult] = useState<{ ok: boolean; text: string } | undefined>(undefined);

  useEffect(() => {
    const element = dialog.current;
    // As AppearanceSettings: no close() on cleanup (StrictMode).
    if (element !== null && !element.open) {
      element.showModal();
    }
  }, []);

  const shown = useMemo(() => {
    const wanted = normalizeName(filter);
    return conversations.filter((c) => wanted === "" || normalizeName(c.name).includes(wanted));
  }, [conversations, filter]);
  const networkName = (networkId: string) => networks.find((n) => n.network_id === networkId)?.name ?? networkId;

  async function send(conversationId: string, name: string) {
    setSending(conversationId);
    setResult(undefined);
    try {
      await store.forward(message.message_id, conversationId);
      setResult({ ok: true, text: `Forwarded to ${name}.` });
    } catch (reason) {
      setResult({ ok: false, text: `Could not forward it to ${name}: ${reason instanceof Error ? reason.message : String(reason)}` });
    } finally {
      setSending(undefined);
    }
  }

  return (
    <dialog ref={dialog} className="settings forward" aria-labelledby="forward-title" onClose={onClose}>
      <h2 id="forward-title">Forward the message</h2>
      <label className="forward-filter">
        Conversation
        <input type="search" value={filter} placeholder="Search by name" onChange={(event) => setFilter(event.target.value)} />
      </label>
      <ul className="forward-list">
        {shown.map((c) => {
          const name = c.name || "Unnamed conversation";
          return (
            <li key={c.conversation_id}>
              <button type="button" className="secondary" disabled={sending !== undefined} onClick={() => void send(c.conversation_id, name)}>
                {name}
                <span className="forward-network"> · {networkName(c.network_id)}</span>
              </button>
            </li>
          );
        })}
      </ul>
      {shown.length === 0 ? <p className="settings-note">No conversation has this name.</p> : null}
      {result === undefined ? null : result.ok ? <output>{result.text}</output> : <p role="alert">{result.text}</p>}
      <div className="settings-buttons">
        <button type="button" onClick={() => dialog.current?.close()}>
          Done
        </button>
      </div>
    </dialog>
  );
}
