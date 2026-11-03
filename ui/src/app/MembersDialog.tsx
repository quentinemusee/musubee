// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

import { useEffect, useRef, useState } from "react";
import type { ConversationMember } from "../core-api/types.gen";
import { Avatar } from "./Avatar";
import { useStore } from "./state";

/** Who is in a conversation (API 1.27), in a modal dialog: the user first, then the others by name. */
export function MembersDialog({ conversationId, name, onClose }: { conversationId: string; name: string; onClose: () => void }) {
  const store = useStore();
  const dialog = useRef<HTMLDialogElement>(null);
  const [members, setMembers] = useState<readonly ConversationMember[] | undefined>(undefined);
  const [failed, setFailed] = useState(false);

  useEffect(() => {
    const element = dialog.current;
    // As AppearanceSettings: no close() on cleanup (StrictMode).
    if (element !== null && !element.open) {
      element.showModal();
    }
    let current = true;
    store.members(conversationId).then(
      (list) => current && setMembers(list),
      () => current && setFailed(true),
    );
    return () => {
      current = false;
    };
  }, [store, conversationId]);

  return (
    <dialog ref={dialog} className="settings" aria-labelledby="members-title" onClose={onClose}>
      <h2 id="members-title">Members of {name}</h2>
      {failed ? (
        <p role="alert">Musubee could not read the members.</p>
      ) : members === undefined ? (
        <p className="settings-note">Reading the members…</p>
      ) : (
        <>
          <p className="settings-note">{members.length === 1 ? "1 member" : `${members.length} members`}</p>
          <ul className="members">
            {members.map((member, index) => (
              // Names repeat, and members have no ID here: the order is stable.
              // oxlint-disable-next-line react/no-array-index-key
              <li key={index}>
                <Avatar mediaId={member.avatar_id} name={member.name || "?"} size="tiny" />
                {member.me === true ? `${member.name || "You"} (you)` : member.name || "Unnamed member"}
              </li>
            ))}
          </ul>
        </>
      )}
      <div className="settings-buttons">
        <button type="button" onClick={() => dialog.current?.close()}>
          Done
        </button>
      </div>
    </dialog>
  );
}
