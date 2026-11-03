// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

import { useEffect, useState } from "react";
import type { Message } from "../core-api/types.gen";
import { activityLabel } from "../inbox";
import { summary } from "../media";
import { normalizeName } from "../merge";
import { openConversation } from "./navigation";
import { useNow } from "./Inbox";
import { SEARCH_FIELD_ID } from "./Shortcuts";
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
  // The conversation the search looks in, if the thread asked for one.
  const scope = useAppState((s) => s.searchIn);
  const scopeName = useAppState((s) => (s.searchIn === undefined ? undefined : s.conversations.find((c) => c.conversation_id === s.searchIn)?.name));
  const [query, setQuery] = useState("");
  const fieldId = SEARCH_FIELD_ID;

  useEffect(() => {
    const timer = setTimeout(() => void store.searchMessages(query), query.trim() === "" ? 0 : SEARCH_DELAY_MS);
    return () => clearTimeout(timer);
  }, [store, query]);

  return (
    <div className="search-field">
      {scope === undefined ? null : (
        <button type="button" className="secondary search-scope" onClick={() => void store.setSearchScope(undefined)}>
          In {scopeName || "this conversation"}
          <span aria-hidden="true"> ✕</span>
          <span className="visually-hidden">: search every conversation instead</span>
        </button>
      )}
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

/** How many conversations a search lists by name, above the messages. */
const MAX_NAMED = 5;

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
  // Conversations whose name matches come first (T2.73): finding a person
  // is the most common search.
  const wanted = normalizeName(search.query);
  const named = conversations.filter((c) => normalizeName(c.name).includes(wanted)).slice(0, MAX_NAMED);
  return (
    <section className="search-results" aria-label={`Messages with “${search.query}”`}>
      {named.length === 0 ? null : (
        <ul className="search-conversations" aria-label="Conversations">
          {named.map((c) => (
            <li key={c.conversation_id}>
              <button type="button" className="search-result" onClick={() => openConversation(store, c.conversation_id)}>
                <span className="search-result-name">{c.name || "Unnamed conversation"}</span>
              </button>
            </li>
          ))}
        </ul>
      )}
      <output className="hint">
        {search.more === "loading" && search.results.length === 0
          ? "Searching…"
          : search.results.length === 0
            ? "No message on this device says that."
            : `${search.results.length}${search.more === "end" ? "" : "+"} found`}
      </output>
      <ul>
        {search.results.map((message) => (
          <MessageResult key={message.message_id} message={message} name={name(message)} now={now} />
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

/** A message found or starred: its conversation, time and text; opens it at the message. */
function MessageResult({ message, name, now }: { message: Message; name: string; now: number }) {
  const store = useStore();
  return (
    <li>
      <button type="button" className="search-result" onClick={() => void store.reveal(message)}>
        <span className="search-result-head">
          <span className="search-result-name">{name || "Unnamed conversation"}</span>
          <time dateTime={new Date(message.timestamp_ms).toISOString()}>{activityLabel(message.timestamp_ms, now)}</time>
        </span>
        <span className="search-result-text">
          {message.from_me ? "You: " : message.sender_name ? `${message.sender_name}: ` : ""}
          {summary(message)}
        </span>
      </button>
    </li>
  );
}

/** The starred messages (API 1.36, docs/ADR/0065), in place of the inbox. */
export function StarredMessages() {
  const store = useStore();
  const starred = useAppState((s) => s.starred);
  const conversations = useAppState((s) => s.conversations);
  const now = useNow();
  if (starred === undefined) {
    return null;
  }
  const name = (message: Message) => conversations.find((c) => c.conversation_id === message.conversation_id)?.name ?? "";
  return (
    <section className="search-results" aria-labelledby="starred-title">
      <div className="inbox-header">
        <h2 id="starred-title">Starred</h2>
        <button type="button" className="secondary" onClick={() => store.hideStarred()}>
          Back to conversations
        </button>
      </div>
      <output className="hint">{starred === "loading" ? "Reading…" : starred.length === 0 ? "No starred messages. Star one from its menu." : ""}</output>
      {starred === "loading" ? null : (
        <ul>
          {starred.map((message) => (
            <MessageResult key={message.message_id} message={message} name={name(message)} now={now} />
          ))}
        </ul>
      )}
    </section>
  );
}
