// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

import { createContext, memo, use, useEffect, useId, useMemo, useRef, useState } from "react";
import { Virtualizer, type CustomItemComponentProps, type VirtualizerHandle } from "virtua";
import { activityLabel, entryOf, inboxEntries, preview, unreadBadge, type InboxEntry } from "../inbox";
import { openConversation } from "./navigation";
import { useAppState, useStore } from "./state";

/**
 * The unified inbox (docs/ADR/0021): the conversations of every account,
 * merged per person, the most recently active first. Virtualized like the
 * thread (docs/ADR/0020); reads the next page of conversations as the user
 * nears the end.
 */
export function Inbox() {
  const store = useStore();
  const conversations = useAppState((s) => s.conversations);
  const persons = useAppState((s) => s.persons);
  const networks = useAppState((s) => s.networks);
  const inbox = useAppState((s) => s.inbox);
  const selected = useAppState((s) => s.selected);
  const ref = useRef<VirtualizerHandle>(null);
  const now = useNow();

  const entries = useMemo(() => inboxEntries(conversations, persons), [conversations, persons]);
  const networkNames = useMemo(() => new Map(networks.map((n) => [n.network_id, n.name])), [networks]);
  const current = entryOf(entries, selected)?.key;

  const canLoad = typeof inbox === "object";
  function loadIfNeeded() {
    const handle = ref.current;
    // Near the end, or a list too short to scroll: read the next page.
    if (canLoad && handle !== null && handle.scrollOffset + 2 * handle.viewportSize >= handle.scrollSize) {
      void store.loadMoreConversations();
    }
  }

  return (
    <section className="inbox" aria-labelledby="conversations-title">
      <h2 id="conversations-title">Conversations</h2>
      {entries.length === 0 ? (
        <p className="empty">No conversations yet.</p>
      ) : (
        // The virtualizer renders a ul of li in its parent, which scrolls.
        <div className="inbox-list">
          <EntryCount value={entries.length}>
            <Virtualizer ref={ref} as="ul" item={InboxItem} data={entries} onScroll={loadIfNeeded} onResize={loadIfNeeded}>
              {(entry) => (
                <InboxRow
                  key={entry.key}
                  entry={entry}
                  current={entry.key === current}
                  now={now}
                  networkNames={networkNames}
                  onOpen={(conversationId) => openConversation(store, conversationId)}
                />
              )}
            </Virtualizer>
          </EntryCount>
        </div>
      )}
      {inbox === "loading" ? <output className="inbox-loading">Loading more conversations…</output> : null}
    </section>
  );
}

/** How many entries the inbox has read, for the rows it renders. */
const EntryCount = createContext(0);

/**
 * A row of the virtualized list. Only the rows near the viewport are in the
 * DOM: each says where it stands, so that assistive technologies can tell
 * the size of the list.
 */
function InboxItem({ style, index, children, ref }: CustomItemComponentProps) {
  const count = use(EntryCount);
  return (
    <li ref={ref} style={style} aria-setsize={count} aria-posinset={index + 1}>
      {children}
    </li>
  );
}

interface InboxRowProps {
  entry: InboxEntry;
  current: boolean;
  now: number;
  networkNames: ReadonlyMap<string, string>;
  onOpen: (conversationId: string) => void;
}

const InboxRow = memo(function InboxRow({ entry, current, now, networkNames, onOpen }: InboxRowProps) {
  const id = useId();
  const latest = entry.conversations[0]!;
  const networks = [...new Set(entry.conversations.map((c) => networkNames.get(c.network_id) ?? c.network_id))];
  const activity = latest.activity_ms;
  const badge = unreadBadge(entry);
  return (
    <div className="inbox-row">
      {/* Named by the name alone, so that the preview does not make names
          ambiguous; the preview describes the button. */}
      <button
        type="button"
        className={badge === undefined ? "conversation" : "conversation unread"}
        aria-current={current ? "true" : undefined}
        aria-labelledby={`${id}-name`}
        aria-describedby={badge === undefined ? `${id}-meta ${id}-preview` : `${id}-unread ${id}-meta ${id}-preview`}
        onClick={() => onOpen(latest.conversation_id)}
      >
        <span className="conversation-top">
          <span id={`${id}-name`} className="conversation-name">
            {entry.name}
          </span>
          {activity === undefined ? null : (
            <time className="conversation-time" dateTime={new Date(activity).toISOString()}>
              {activityLabel(activity, now)}
            </time>
          )}
        </span>
        <span id={`${id}-meta`} className="conversation-meta">
          {networks.join(" · ")}
          {latest.kind === "group" ? " · Group" : ""}
        </span>
        <span className="conversation-bottom">
          <span id={`${id}-preview`} className="conversation-preview">
            {preview(entry)}
          </span>
          {badge === undefined ? null : (
            // The count is what the eye reads; the label, what a screen
            // reader says: "3 unread messages", not "3".
            <>
              <span className="unread-badge" aria-hidden="true">
                {badge.text}
              </span>
              <span id={`${id}-unread`} className="visually-hidden">
                {badge.label}
              </span>
            </>
          )}
        </span>
      </button>
    </div>
  );
});

/** The time, updated every minute: "today" changes at midnight. */
function useNow(): number {
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    const timer = setInterval(() => setNow(Date.now()), 60_000);
    return () => clearInterval(timer);
  }, []);
  return now;
}
