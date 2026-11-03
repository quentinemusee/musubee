// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

import { useEffect, useId, useState } from "react";
import type { Message } from "../core-api/types.gen";
import { activityLabel } from "../inbox";
import { summary } from "../media";
import { useNow } from "./Inbox";
import { useAppState, useStore } from "./state";

/** How long typing pauses before the search runs. */
const SEARCH_DELAY_MS = 300;

/**
 * The search field of the sidebar (API 1.17, docs/ADR/0046). While it holds
 * a query, the sidebar lists the messages found instead of the inbox; a
 * result opens its conversation. Escape or clearing the field ends the
 * search.
 */
export function SearchField() {
  const store = useStore();
  const [query, setQuery] = useState("");
  const fieldId = useId();

  useEffect(() => {
    const timer = setTimeout(() => void store.searchMessages(query), query.trim() === "" ? 0 : SEARCH_DELAY_MS);
    return () => clearTimeout(timer);
  }, [store, query]);

  return (
    <div className="search-field">
      <label htmlFor={fieldId} className="visually-hidden">
        Search messages
      </label>
      <input
        id={fieldId}
        type="search"
        placeholder="Search messages"
        autoComplete="off"
        value={query}
        onChange={(event) => setQuery(event.target.value)}
        onKeyDown={(event) => {
          if (event.key === "Escape" && query !== "") {
            event.preventDefault();
            setQuery("");
          }
        }}
      />
    </div>
  );
}

/** The messages found, in place of the inbox while the user searches. */
export function SearchResults() {
  const store = useStore();
  const search = useAppState((s) => s.search);
  const conversations = useAppState((s) => s.conversations);
  const now = useNow();
  if (search === undefined) {
    return null;
  }
  const name = (message: Message) => conversations.find((c) => c.conversation_id === message.conversation_id)?.name ?? "";
  return (
    <section className="search-results" aria-label={`Messages with “${search.query}”`}>
      <output className="hint">
        {search.more === "loading" && search.results.length === 0
          ? "Searching…"
          : search.results.length === 0
            ? "No message on this device says that."
            : `${search.results.length}${search.more === "end" ? "" : "+"} found`}
      </output>
      <ul>
        {search.results.map((message) => (
          <li key={message.message_id}>
            <button type="button" className="search-result" onClick={() => void store.select(message.conversation_id)}>
              <span className="search-result-head">
                <span className="search-result-name">{name(message) || "Unnamed conversation"}</span>
                <time dateTime={new Date(message.timestamp_ms).toISOString()}>{activityLabel(message.timestamp_ms, now)}</time>
              </span>
              <span className="search-result-text">
                {message.from_me ? "You: " : message.sender_name ? `${message.sender_name}: ` : ""}
                {summary(message)}
              </span>
            </button>
          </li>
        ))}
      </ul>
      {typeof search.more === "object" ? (
        <button type="button" className="secondary" onClick={() => void store.searchMore()}>
          More results
        </button>
      ) : null}
    </section>
  );
}
