// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

import { useLayoutEffect, useRef, useState } from "react";
import { VList, type VListHandle } from "virtua";
import type { Message } from "../core-api/types.gen";
import type { HistoryState } from "../store";
import { MessageRow } from "./MessageRow";

export interface MessageListProps {
  /** The messages of one conversation, oldest first. */
  messages: readonly Message[];
  history: HistoryState | undefined;
  /** Reads older messages; called when the user nears the top. */
  onOlder: () => void;
  /** The ID of the element that names the conversation, which names the list. */
  labelledBy?: string;
}

/**
 * How far beyond the viewport rows are rendered, in pixels. virtua's
 * default (200) left blank areas in fast flings on a slow Android; 800 did
 * not, for the same frame times (docs/ADR/0020).
 */
const BUFFER_PX = 800;

/** What the list last showed, to tell older messages from new ones. */
interface Shown {
  first: string | undefined;
  last: string | undefined;
  count: number;
  /** Whether the last change added messages above (virtua's shift). */
  shift: boolean;
  /** The last message received from someone else while the list was shown. */
  news: Message | undefined;
}

function shownOf(messages: readonly Message[], shift: boolean, news: Message | undefined): Shown {
  return { first: messages[0]?.message_id, last: messages.at(-1)?.message_id, count: messages.length, shift, news };
}

/**
 * The messages of a thread, virtualized (docs/ADR/0020): only the rows near
 * the viewport are in the DOM. It opens at the newest message, follows new
 * ones while the user is at the bottom, reads older ones as the user nears
 * the top, and keeps the message being read in place when they arrive.
 * Render one per conversation (key it by conversation ID).
 */
export function MessageList({ messages, history, onOlder, labelledBy }: MessageListProps) {
  const ref = useRef<VListHandle>(null);
  const atBottom = useRef(true);

  // Compare with the last render (React's "previous props" pattern).
  const [shown, setShown] = useState(() => shownOf(messages, false, undefined));
  let current = shown;
  const first = messages[0]?.message_id;
  const last = messages.at(-1)?.message_id;
  if (first !== shown.first || last !== shown.last || messages.length !== shown.count) {
    const older = shown.count > 0 && first !== shown.first && last === shown.last;
    const newest = messages.at(-1);
    const arrived = shown.count > 0 && last !== shown.last && newest !== undefined && !newest.from_me;
    current = shownOf(messages, older, arrived ? newest : shown.news);
    setShown(current);
  }

  // Follow the conversation while at the bottom; older messages keep the
  // position (shift) instead.
  useLayoutEffect(() => {
    if (!current.shift && atBottom.current && messages.length > 0) {
      ref.current?.scrollToIndex(messages.length - 1, { align: "end" });
    }
  }, [messages, current.shift]);

  const canLoad = typeof history === "object";
  function loadIfNeeded() {
    const handle = ref.current;
    // Near the top, or a short conversation that does not fill the
    // viewport (it cannot scroll): read older messages.
    if (canLoad && handle !== null && (handle.scrollOffset < handle.viewportSize || handle.scrollSize <= handle.viewportSize)) {
      onOlder();
    }
  }

  return (
    <div className="messages-frame">
      {/* Over the list, so that showing it does not move the messages. */}
      {history === "loading" ? <output className="messages-loading">Loading older messages…</output> : null}
      {/* A log, but not a live region: rows mount as the user scrolls, and
          that is not news. The status below announces new messages. */}
      <VList
        ref={ref}
        className="messages"
        role="log"
        aria-labelledby={labelledBy}
        aria-live="off"
        tabIndex={0}
        bufferSize={BUFFER_PX}
        data={messages as Message[]}
        shift={current.shift}
        onScroll={(offset) => {
          const handle = ref.current;
          if (handle === null) {
            return;
          }
          atBottom.current = handle.scrollSize - offset - handle.viewportSize < 2;
          loadIfNeeded();
        }}
        onResize={loadIfNeeded}
      >
        {(message) => <MessageRow key={message.message_id} message={message} />}
      </VList>
      <output className="visually-hidden">
        {current.news ? `${current.news.sender_name || "New message"}: ${current.news.text}` : ""}
      </output>
    </div>
  );
}
