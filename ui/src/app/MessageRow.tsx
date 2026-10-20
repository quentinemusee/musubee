// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

import { memo } from "react";
import type { Message, MessageReply } from "../core-api/types.gen";
import { DELETED_TEXT, reactionLabel, replyQuote } from "../message";

const timeFormat = new Intl.DateTimeFormat(undefined, { hour: "2-digit", minute: "2-digit" });

const STATUS_LABELS: Record<string, string> = {
  sending: "Sending…",
  sent: "Sent",
  failed: "Not sent",
};

/** The answered message, quoted above an answer. */
function ReplyQuote({ reply }: { reply: MessageReply }) {
  const { sender, text } = replyQuote(reply);
  return (
    <blockquote className="reply" data-deleted={reply.deleted === true}>
      <span className="visually-hidden">In reply to </span>
      {sender !== "" ? <span className="reply-sender">{sender}</span> : null}
      <span className="reply-text">{text}</span>
    </blockquote>
  );
}

/**
 * One message of a thread: a full-width row holding the bubble. The row has
 * no margin, so that a virtualized list can measure it whole.
 */
export const MessageRow = memo(function MessageRow({ message }: { message: Message }) {
  const deleted = message.deleted === true;
  return (
    <div className="message-row" data-message-id={message.message_id}>
      <div className="message" data-from-me={message.from_me} data-status={message.status} data-deleted={deleted}>
        {!message.from_me && message.sender_name ? <span className="sender">{message.sender_name}</span> : null}
        {message.reply_to && !deleted ? <ReplyQuote reply={message.reply_to} /> : null}
        {deleted ? (
          <span className="text" data-kind="deleted">
            {DELETED_TEXT}
          </span>
        ) : (
          <span className="text" data-kind={message.kind}>
            {message.text}
          </span>
        )}
        {message.reactions && !deleted ? (
          <ul className="reactions" aria-label="Reactions">
            {message.reactions.map((r) => (
              <li key={r.key} className="reaction" data-from-me={r.from_me === true} aria-label={reactionLabel(r)}>
                <span aria-hidden="true">
                  {r.key} {r.count}
                </span>
              </li>
            ))}
          </ul>
        ) : null}
        <span className="meta">
          {message.edited && !deleted ? <span className="edited">Edited</span> : null}
          <time dateTime={new Date(message.timestamp_ms).toISOString()}>{timeFormat.format(message.timestamp_ms)}</time>
          {message.from_me ? <span className="delivery">{message.error || STATUS_LABELS[message.status]}</span> : null}
        </span>
      </div>
    </div>
  );
});
