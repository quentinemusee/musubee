// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

import { useEffect, useLayoutEffect, useRef, useState } from "react";
import { VList, type VListHandle } from "virtua";
import type { ConversationActions, Message } from "../core-api/types.gen";
import { summary } from "../media";
import type { HistoryState } from "../store";
import { MessageRow, type MessageHandlers } from "./MessageRow";
import { useAppState, useStore } from "./state";

export interface MessageListProps {
  /** The messages of one conversation, oldest first. */
  messages: readonly Message[];
  history: HistoryState | undefined;
  /** Reads older messages; called when the user nears the top. */
  onOlder: () => void;
  /** The ID of the element that names the conversation, which names the list. */
  labelledBy?: string;
  /** Called when the user scrolls to the newest message, or away from it. The list opens there. */
  onBottomChange?: (atBottom: boolean) => void;
  /** What the conversation's network lets the user do to messages (API 1.8). */
  actions?: ConversationActions | undefined;
  /** What the user does to messages; without, the messages offer no action. Keep it stable. */
  handlers?: MessageHandlers | undefined;
  /**
   * For a person's thread (docs/ADR/0042), by conversation ID: what each
   * network allows, instead of actions, and the network each message came
   * through, said on it.
   */
  together?: { actions: Readonly<Record<string, ConversationActions | undefined>>; via: Readonly<Record<string, string>> } | undefined;
  /** By message ID: what the user's read messages say instead of "Sent" (API 1.21). */
  seen?: Readonly<Record<string, string>> | undefined;
  /** The first message that was unread when the thread opened: "New messages" goes above it. */
  firstUnread?: string | undefined;
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
export function MessageList({ messages, history, onOlder, labelledBy, onBottomChange, actions, handlers, together, seen, firstUnread }: MessageListProps) {
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

  // A message to show (a search result): scrolled to the middle and
  // highlighted for a moment; following the bottom stops until the user
  // goes back down.
  const store = useStore();
  const focus = useAppState((s) => s.focus);
  const focusIndex = focus === undefined ? -1 : messages.findIndex((m) => m.message_id === focus);
  const highlighted = focusIndex < 0 ? undefined : focus;
  const scrolledTo = useRef<string>(undefined);
  useLayoutEffect(() => {
    if (highlighted === undefined || scrolledTo.current === highlighted) {
      return;
    }
    scrolledTo.current = highlighted;
    atBottom.current = false;
    ref.current?.scrollToIndex(focusIndex, { align: "center" });
  }, [highlighted, focusIndex]);
  // The highlight lasts two seconds.
  useEffect(() => {
    if (highlighted === undefined) {
      return;
    }
    const timer = setTimeout(() => store.focused(), 2000);
    return () => clearTimeout(timer);
  }, [highlighted, store]);

  // Follow the conversation while at the bottom; older messages keep the
  // position (shift) instead.
  useLayoutEffect(() => {
    if (!current.shift && atBottom.current && messages.length > 0) {
      ref.current?.scrollToIndex(messages.length - 1, { align: "end" });
    }
  }, [messages, current.shift]);

  // Older messages on the device, or the network's (API 1.13).
  const canLoad = typeof history === "object" || history === "network";
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
          const bottom = handle.scrollSize - offset - handle.viewportSize < 2;
          if (bottom !== atBottom.current) {
            atBottom.current = bottom;
            onBottomChange?.(bottom);
          }
          loadIfNeeded();
        }}
        onResize={loadIfNeeded}
      >
        {(message) =>
          together === undefined ? (
            <MessageRow
              key={message.message_id}
              message={message}
              actions={actions}
              handlers={handlers}
              seen={seen?.[message.message_id]}
              highlighted={message.message_id === highlighted}
              unreadFromHere={message.message_id === firstUnread}
            />
          ) : (
            <MessageRow
              key={message.message_id}
              message={message}
              actions={together.actions[message.conversation_id]}
              handlers={handlers}
              via={together.via[message.conversation_id]}
              seen={seen?.[message.message_id]}
              highlighted={message.message_id === highlighted}
            />
          )
        }
      </VList>
      <output className="visually-hidden">
        {current.news ? `${current.news.sender_name || "New message"}: ${summary(current.news)}` : ""}
      </output>
    </div>
  );
}
