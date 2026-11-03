// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

import { createContext, memo, use, useEffect, useId, useMemo, useRef, useState } from "react";
import { Virtualizer, type CustomItemComponentProps, type VirtualizerHandle } from "virtua";
import { activityLabel, entryOf, inboxEntries, isArchived, preview, unreadBadge, type InboxEntry } from "../inbox";
import { typingText } from "../message";
import { Avatar } from "./Avatar";
import { isMuted } from "../mute";
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

  const all = useMemo(() => inboxEntries(conversations, persons), [conversations, persons]);
  // Archived entries (API 1.31) have a list of their own.
  const [archiveShown, setArchiveShown] = useState(false);
  const archived = useMemo(() => all.filter(isArchived), [all]);
  const entries = useMemo(() => (archiveShown ? archived : all.filter((e) => !isArchived(e))), [all, archived, archiveShown]);
  const anyUnread = entries.some((e) => e.unread > 0 || e.markedUnread);
  const networkNames = useMemo(() => new Map(networks.map((n) => [n.network_id, n.name])), [networks]);
  const current = entryOf(all, selected)?.key;

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
      <div className="inbox-header">
        <h2 id="conversations-title">{archiveShown ? "Archived" : "Conversations"}</h2>
        {anyUnread ? (
          <button type="button" className="secondary" onClick={() => void store.markAllRead()}>
            Mark all read
          </button>
        ) : null}
      </div>
      {archiveShown || archived.length > 0 ? (
        <button type="button" className="secondary archive-switch" aria-pressed={archiveShown} onClick={() => setArchiveShown(!archiveShown)}>
          {archiveShown ? "Back to conversations" : `Archived (${archived.length})`}
        </button>
      ) : null}
      {entries.length === 0 ? (
        <p className="empty">{archiveShown ? "No archived conversations." : "No conversations yet."}</p>
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
  // Someone typing shows in place of the preview (API 1.20).
  const typing = useAppState((s) => s.typing?.[latest.conversation_id]);
  const mentions = entry.conversations.reduce((sum, c) => sum + (c.unread_mentions ?? 0), 0);
  // An entry is muted when all its conversations are (ADR 0050).
  const muted = entry.conversations.every((c) => isMuted(c, now));
  return (
    <div className="inbox-row">
      {/* Named by the name alone, so that the preview does not make names
          ambiguous; the preview describes the button. aria-label rather than
          aria-labelledby: Android System WebView 153 on Android 17 computes
          a name from aria-labelledby but leaves it out of Android's
          accessibility tree, where the row then has no name (ADR 0029). */}
      <button
        type="button"
        className={badge === undefined ? "conversation" : "conversation unread"}
        aria-current={current ? "true" : undefined}
        aria-label={entry.name}
        aria-describedby={[mentions > 0 ? `${id}-mentions` : "", badge === undefined ? "" : `${id}-unread`, `${id}-meta`, `${id}-preview`]
          .filter((x) => x !== "")
          .join(" ")}
        onClick={() => onOpen(latest.conversation_id)}
      >
        <Avatar mediaId={latest.avatar_id} name={entry.name} />
        <span className="conversation-top">
          <span className="conversation-name">{entry.name}</span>
          {activity === undefined ? null : (
            <time className="conversation-time" dateTime={new Date(activity).toISOString()}>
              {activityLabel(activity, now)}
            </time>
          )}
        </span>
        <span id={`${id}-meta`} className="conversation-meta">
          {networks.join(" · ")}
          {latest.kind === "group" ? " · Group" : ""}
          {entry.conversations.some((c) => c.pinned === true) ? " · Pinned" : ""}
          {muted ? " · Muted" : ""}
        </span>
        <span className="conversation-bottom">
          <span id={`${id}-preview`} className="conversation-preview" data-typing={typing !== undefined && typing.length > 0}>
            {typing !== undefined && typing.length > 0 ? typingText(typing) : preview(entry)}
          </span>
          {mentions > 0 ? (
            // An unread message mentions the user (API 1.34, docs/ADR/0062).
            <>
              <span className="mention-badge" aria-hidden="true">
                @
              </span>
              <span id={`${id}-mentions`} className="visually-hidden">
                {mentions === 1 ? "1 mention of you" : `${mentions} mentions of you`}
              </span>
            </>
          ) : null}
          {badge === undefined ? null : (
            // The count is what the eye reads; the label, what a screen
            // reader says: "3 unread messages", not "3".
            <>
              <span className={muted ? "unread-badge muted" : "unread-badge"} aria-hidden="true">
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
export function useNow(): number {
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    const timer = setInterval(() => setNow(Date.now()), 60_000);
    return () => clearInterval(timer);
  }, []);
  return now;
}
