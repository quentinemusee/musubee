// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

import { useCallback, useState, type FormEvent } from "react";
import type { Message } from "../core-api/types.gen";
import { MessageList } from "./MessageList";
import { useAppState, useStore } from "./state";

const noMessages: readonly Message[] = [];

export function Thread() {
  const store = useStore();
  const selected = useAppState((s) => s.selected);
  const conversation = useAppState((s) => s.conversations.find((c) => c.conversation_id === s.selected));
  const messages = useAppState((s) => (s.selected === undefined ? noMessages : (s.messages[s.selected] ?? noMessages)));
  const history = useAppState((s) => (s.selected === undefined ? undefined : s.history[s.selected]));
  const loadOlder = useCallback(() => void store.loadOlder(), [store]);

  if (selected === undefined || conversation === undefined) {
    return <p className="empty">Choose a conversation.</p>;
  }
  return (
    <section className="thread" aria-labelledby="thread-title">
      <h2 id="thread-title">{conversation.name || "Unnamed conversation"}</h2>
      <MessageList key={selected} messages={messages} history={history} onOlder={loadOlder} labelledBy="thread-title" />
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
