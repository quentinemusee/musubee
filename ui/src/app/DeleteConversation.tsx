// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

import { useEffect, useRef, useState } from "react";
import { closeConversation } from "./navigation";
import { useStore } from "./state";

/**
 * Asks before deleting a conversation (docs/ADR/0053), in a modal dialog:
 * what goes, and where. It closes once the network deleted it, or says why
 * it could not.
 */
export function DeleteConversation({ conversationId, name, network, onClose }: { conversationId: string; name: string; network: string; onClose: () => void }) {
  const store = useStore();
  const dialog = useRef<HTMLDialogElement>(null);
  const [deleting, setDeleting] = useState(false);
  const [error, setError] = useState<string | undefined>(undefined);

  useEffect(() => {
    const element = dialog.current;
    // As AppearanceSettings: no close() on cleanup (StrictMode).
    if (element !== null && !element.open) {
      element.showModal();
    }
  }, []);

  async function remove() {
    setDeleting(true);
    setError(undefined);
    try {
      await store.deleteConversation(conversationId);
      closeConversation(store);
      dialog.current?.close();
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : String(reason));
      setDeleting(false);
    }
  }

  return (
    <dialog ref={dialog} className="settings" aria-labelledby="delete-title" aria-describedby="delete-what" onClose={onClose}>
      <h2 id="delete-title">Delete the conversation with {name}?</h2>
      <p id="delete-what">
        Its messages go from Musubee and, as {network} allows, from your other apps. {name} keeps their copy. This cannot be undone.
      </p>
      {error === undefined ? null : <p role="alert">Musubee could not delete it: {error}</p>}
      <div className="settings-buttons">
        <button type="button" className="secondary" disabled={deleting} onClick={() => dialog.current?.close()}>
          Cancel
        </button>
        <button type="button" className="danger" disabled={deleting} onClick={() => void remove()}>
          {deleting ? "Deleting…" : "Delete"}
        </button>
      </div>
    </dialog>
  );
}
