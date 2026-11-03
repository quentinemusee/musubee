// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

import { useEffect, useRef, useState } from "react";
import { closeConversation } from "./navigation";
import { useStore } from "./state";

/** What the dialog asks: deleting a direct conversation (docs/ADR/0053), or leaving a group (0056). */
export type Removal = "delete" | "leave";

const WORDS: Record<Removal, { title: (name: string) => string; what: (name: string, network: string) => string; verb: string; doing: string }> = {
  delete: {
    title: (name) => `Delete the conversation with ${name}?`,
    what: (name, network) => `Its messages go from Musubee and, as ${network} allows, from your other apps. ${name} keeps their copy. This cannot be undone.`,
    verb: "Delete",
    doing: "Deleting…",
  },
  leave: {
    title: (name) => `Leave ${name}?`,
    what: (_, network) =>
      `You leave the group on ${network}, and its messages go from Musubee. The other members keep theirs. To come back, someone must add you again.`,
    verb: "Leave",
    doing: "Leaving…",
  },
};

/**
 * Asks before deleting a conversation or leaving a group, in a modal
 * dialog: what goes, and where. It closes once the network answered, or
 * says why it could not.
 */
export function DeleteConversation({
  conversationId,
  name,
  network,
  removal = "delete",
  onClose,
}: {
  conversationId: string;
  name: string;
  network: string;
  removal?: Removal;
  onClose: () => void;
}) {
  const store = useStore();
  const dialog = useRef<HTMLDialogElement>(null);
  const [deleting, setDeleting] = useState(false);
  const [error, setError] = useState<string | undefined>(undefined);
  const words = WORDS[removal];

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
      await (removal === "leave" ? store.leaveConversation(conversationId) : store.deleteConversation(conversationId));
      closeConversation(store);
      dialog.current?.close();
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : String(reason));
      setDeleting(false);
    }
  }

  return (
    <dialog ref={dialog} className="settings" aria-labelledby="delete-title" aria-describedby="delete-what" onClose={onClose}>
      <h2 id="delete-title">{words.title(name)}</h2>
      <p id="delete-what">{words.what(name, network)}</p>
      {error === undefined ? null : (
        <p role="alert">
          Musubee could not {words.verb.toLowerCase()} it: {error}
        </p>
      )}
      <div className="settings-buttons">
        <button type="button" className="secondary" disabled={deleting} onClick={() => dialog.current?.close()}>
          Cancel
        </button>
        <button type="button" className="danger" disabled={deleting} onClick={() => void remove()}>
          {deleting ? words.doing : words.verb}
        </button>
      </div>
    </dialog>
  );
}
