// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

import { useId, useState } from "react";
import type { Conversation } from "../core-api/types.gen";
import { privacyWords } from "../privacy";
import { useAppState } from "./state";

/**
 * Who can read a conversation, under the thread's header (docs/ADR/0039):
 * a short label that opens its explanation. A label that does not reassure
 * is marked so, in words and not only in color.
 */
export function PrivacyNote({ conversation }: { conversation: Conversation }) {
  const network = useAppState((s) => s.networks.find((n) => n.network_id === conversation.network_id));
  const account = useAppState((s) => s.accounts.find((a) => a.account_id === conversation.account_id));
  const [open, setOpen] = useState(false);
  const detailId = useId();
  const words = privacyWords(conversation.privacy, network?.name || conversation.network_id, account?.connection);
  return (
    <div className="privacy-note" data-safe={words.safe}>
      <button type="button" className="privacy-label" aria-expanded={open} aria-controls={detailId} onClick={() => setOpen(!open)}>
        <span aria-hidden="true">{words.safe ? "🔒" : "⚠️"}</span> {words.label}
      </button>
      <p id={detailId} className="privacy-detail" hidden={!open}>
        {words.detail}
      </p>
    </div>
  );
}
