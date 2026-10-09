// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

import { useEffect, useRef, useState, type FormEvent } from "react";
import type { Message } from "../core-api/types.gen";
import { useAppState, useStore } from "./state";

const noMessages: readonly Message[] = [];

const timeFormat = new Intl.DateTimeFormat(undefined, { hour: "2-digit", minute: "2-digit" });

const STATUS_LABELS: Record<string, string> = {
  sending: "Sending…",
  sent: "Sent",
  failed: "Not sent",
};

export function Thread() {
  const selected = useAppState((s) => s.selected);
  const conversation = useAppState((s) => s.conversations.find((c) => c.conversation_id === s.selected));
  const messages = useAppState((s) => (s.selected === undefined ? noMessages : (s.messages[s.selected] ?? noMessages)));
  const end = useRef<HTMLLIElement>(null);

  // Follow the conversation as messages arrive.
  const count = messages.length;
  useEffect(() => {
    if (count > 0) {
      end.current?.scrollIntoView({ block: "end" });
    }
  }, [count]);

  if (selected === undefined || conversation === undefined) {
    return <p className="empty">Choose a conversation.</p>;
  }
  return (
    <section className="thread" aria-labelledby="thread-title">
      <h2 id="thread-title">{conversation.name || "Unnamed conversation"}</h2>
      {/* The list is a log: screen readers announce new messages. */}
      <ol className="messages" role="log" aria-live="polite">
        {messages.map((message) => (
          <li key={message.message_id} className="message" data-from-me={message.from_me} data-status={message.status}>
            {!message.from_me && message.sender_name ? <span className="sender">{message.sender_name}</span> : null}
            <span className="text" data-kind={message.kind}>
              {message.text}
            </span>
            <span className="meta">
              <time dateTime={new Date(message.timestamp_ms).toISOString()}>{timeFormat.format(message.timestamp_ms)}</time>
              {message.from_me ? <span className="delivery">{message.error || STATUS_LABELS[message.status]}</span> : null}
            </span>
          </li>
        ))}
        <li ref={end} aria-hidden="true" className="messages-end" />
      </ol>
      <Composer key={selected} />
    </section>
  );
}

function Composer() {
  const store = useStore();
  const [text, setText] = useState("");

  function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const message = text.trim();
    if (message === "") {
      return;
    }
    setText("");
    void store.send(message);
  }

  return (
    <form className="composer" onSubmit={submit}>
      <label htmlFor="composer-text" className="visually-hidden">
        Message
      </label>
      <input
        id="composer-text"
        type="text"
        autoComplete="off"
        placeholder="Write a message…"
        value={text}
        onChange={(event) => setText(event.target.value)}
      />
      <button type="submit" disabled={text.trim() === ""}>
        Send
      </button>
    </form>
  );
}
