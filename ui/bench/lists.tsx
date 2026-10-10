// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

// The thread lists compared by the benchmark (docs/ADR/0020), and the
// app's own (MessageList, "app"). Each one shows the messages oldest first
// in a scroll container of class "bench-scroller" (role "log" for the
// app's), starts at the bottom, follows new messages while at the
// bottom, and keeps the visible messages in place when older ones are
// added above, each with its library's own means.

import { useVirtualizer } from "@tanstack/react-virtual";
import { useEffect, useLayoutEffect, useRef, useState, type RefObject } from "react";
import { Virtuoso } from "react-virtuoso";
import { VList, type VListHandle } from "virtua";
import { MessageList } from "../src/app/MessageList";
import { MessageRow } from "../src/app/MessageRow";
import type { Message } from "../src/core-api/types.gen";
import type { HistoryState } from "../src/store";

export interface ListProps {
  messages: readonly Message[];
  /** How many messages were added above since the list was shown. */
  prepended: number;
  /** The history as the store keeps it, and its loader: only the app's list reads more. */
  history: HistoryState;
  onOlder: () => void;
}

/** Whether a scroll container is at its bottom. */
function atBottom(el: HTMLElement): boolean {
  return el.scrollHeight - el.scrollTop - el.clientHeight < 2;
}

/**
 * Every message in the DOM. Chromium's scroll anchoring (overflow-anchor)
 * keeps the visible messages in place when older ones are added. With
 * `deferred`, rows use content-visibility: auto, so that the browser skips
 * the layout and paint of the rows off screen.
 */
export function PlainList({ messages, deferred }: ListProps & { deferred: boolean }) {
  const ref = useRef<HTMLDivElement>(null);
  const content = useRef<HTMLDivElement>(null);
  const stick = useStickToBottom(ref, content);
  useLayoutEffect(() => {
    const el = ref.current;
    if (el !== null && stick.current && messages.length > 0) {
      el.scrollTop = el.scrollHeight;
    }
  }, [messages, stick]);
  return (
    <div ref={ref} className="bench-scroller" onScroll={(event) => stick.onScroll(event.currentTarget)}>
      <div ref={content} className={deferred ? "deferred" : undefined}>
        {messages.map((message) => (
          <MessageRow key={message.message_id} message={message} />
        ))}
      </div>
    </div>
  );
}

/**
 * Keeps a scroll container at its bottom while its content grows (rows
 * that measure themselves, deferred rows rendered late), until the user
 * scrolls up; reaching the bottom again sticks again.
 */
function useStickToBottom(scroller: RefObject<HTMLElement | null>, content: RefObject<HTMLElement | null>) {
  // A stable object that the scroll handler and the observer share.
  const [state] = useState(() => ({
    current: true,
    top: 0,
    height: 0,
    onScroll(el: HTMLElement) {
      // Scrolling up with an unchanged content is the user's doing.
      if (el.scrollTop < this.top - 1 && el.scrollHeight === this.height) {
        this.current = false;
      }
      if (atBottom(el)) {
        this.current = true;
      }
      this.top = el.scrollTop;
      this.height = el.scrollHeight;
    },
  }));
  useEffect(() => {
    const el = scroller.current;
    const inner = content.current;
    if (el === null || inner === null) {
      return;
    }
    const observer = new ResizeObserver(() => {
      if (state.current) {
        el.scrollTop = el.scrollHeight;
      }
      state.height = el.scrollHeight;
    });
    observer.observe(inner);
    return () => observer.disconnect();
  }, [scroller, content, state]);
  return state;
}

/** Where react-virtuoso numbers the first message, so that prepending lowers it. */
const FIRST_INDEX = 1_000_000;

function virtuosoItem(_: number, message: Message) {
  return <MessageRow message={message} />;
}

export function VirtuosoList({ messages, prepended }: ListProps) {
  return (
    <Virtuoso
      className="bench-scroller"
      data={messages}
      firstItemIndex={FIRST_INDEX - prepended}
      initialTopMostItemIndex={{ index: "LAST", align: "end" }}
      followOutput={(isAtBottom) => (isAtBottom ? "auto" : false)}
      computeItemKey={(_, message) => message.message_id}
      itemContent={virtuosoItem}
    />
  );
}

const ESTIMATED_ROW = 64;

export function TanStackList({ messages, prepended }: ListProps) {
  const ref = useRef<HTMLDivElement>(null);
  const bottom = useRef(true);
  const shown = useRef({ count: messages.length, prepended });
  // oxlint-disable-next-line react/incompatible-library -- the React Compiler skips this component; the benchmark does not use it.
  const virtualizer = useVirtualizer({
    count: messages.length,
    getScrollElement: () => ref.current,
    estimateSize: () => ESTIMATED_ROW,
    overscan: 6,
    getItemKey: (index) => messages[index]!.message_id,
  });
  useLayoutEffect(() => {
    const previous = shown.current;
    shown.current = { count: messages.length, prepended };
    if (prepended > previous.prepended) {
      // No built-in support: move down by the estimated size of the new
      // messages; the virtualizer corrects the offset as it measures them.
      virtualizer.scrollToOffset((virtualizer.scrollOffset ?? 0) + (prepended - previous.prepended) * ESTIMATED_ROW);
    } else if (bottom.current) {
      virtualizer.scrollToIndex(messages.length - 1, { align: "end" });
    }
  }, [messages, prepended, virtualizer]);
  const items = virtualizer.getVirtualItems();
  return (
    <div
      ref={ref}
      className="bench-scroller"
      onScroll={(event) => {
        bottom.current = atBottom(event.currentTarget);
      }}
    >
      <div className="bench-sizer" style={{ height: virtualizer.getTotalSize() }}>
        <div className="bench-window" style={{ transform: `translateY(${items[0]?.start ?? 0}px)` }}>
          {items.map((item) => (
            <div key={item.key} data-index={item.index} ref={virtualizer.measureElement}>
              <MessageRow message={messages[item.index]!} />
            </div>
          ))}
        </div>
      </div>
    </div>
  );
}

export function VirtuaList({ messages, prepended }: ListProps) {
  const ref = useRef<VListHandle>(null);
  const bottom = useRef(true);
  // shift keeps the position from the end of the list after messages were
  // added above, until the next change (as MessageList does).
  const [seen, setSeen] = useState({ prepended, count: messages.length, shift: false });
  let shift = seen.shift;
  if (seen.prepended !== prepended || seen.count !== messages.length) {
    shift = prepended !== seen.prepended;
    setSeen({ prepended, count: messages.length, shift });
  }
  useLayoutEffect(() => {
    if (!shift && bottom.current && messages.length > 0) {
      ref.current?.scrollToIndex(messages.length - 1, { align: "end" });
    }
  }, [messages, shift]);
  return (
    <VList
      ref={ref}
      className="bench-scroller"
      data={messages as Message[]}
      shift={shift}
      onScroll={(offset) => {
        const handle = ref.current;
        if (handle !== null) {
          bottom.current = handle.scrollSize - offset - handle.viewportSize < 2;
        }
      }}
    >
      {(message) => <MessageRow key={message.message_id} message={message} />}
    </VList>
  );
}

export const LISTS = {
  plain: (props: ListProps) => <PlainList {...props} deferred={false} />,
  "content-visibility": (props: ListProps) => <PlainList {...props} deferred />,
  "react-virtuoso": VirtuosoList,
  "tanstack-virtual": TanStackList,
  virtua: VirtuaList,
  app: ({ messages, history, onOlder }: ListProps) => <MessageList messages={messages} history={history} onOlder={onOlder} />,
} as const;

export type ListName = keyof typeof LISTS;
