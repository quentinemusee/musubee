// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

import { memo } from "react";
import type { Message } from "../core-api/types.gen";

const timeFormat = new Intl.DateTimeFormat(undefined, { hour: "2-digit", minute: "2-digit" });

const STATUS_LABELS: Record<string, string> = {
  sending: "Sending…",
  sent: "Sent",
  failed: "Not sent",
};

/**
 * One message of a thread: a full-width row holding the bubble. The row has
 * no margin, so that a virtualized list can measure it whole.
 */
export const MessageRow = memo(function MessageRow({ message }: { message: Message }) {
  return (
    <div className="message-row" data-message-id={message.message_id}>
      <div className="message" data-from-me={message.from_me} data-status={message.status}>
        {!message.from_me && message.sender_name ? <span className="sender">{message.sender_name}</span> : null}
        <span className="text" data-kind={message.kind}>
          {message.text}
        </span>
        <span className="meta">
          <time dateTime={new Date(message.timestamp_ms).toISOString()}>{timeFormat.format(message.timestamp_ms)}</time>
          {message.from_me ? <span className="delivery">{message.error || STATUS_LABELS[message.status]}</span> : null}
        </span>
      </div>
    </div>
  );
});
