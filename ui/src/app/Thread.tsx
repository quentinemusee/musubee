// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

import { Fragment, useCallback, useEffect, useMemo, useState, useSyncExternalStore, type FormEvent } from "react";
import type { Conversation, Message } from "../core-api/types.gen";
import { MessageList } from "./MessageList";
import { closeConversation } from "./navigation";
import { useAppState, useStore } from "./state";

const noMessages: readonly Message[] = [];

export function Thread() {
  const store = useStore();
  const selected = useAppState((s) => s.selected);
  const conversation = useAppState((s) => s.conversations.find((c) => c.conversation_id === s.selected));
  const messages = useAppState((s) => (s.selected === undefined ? noMessages : (s.messages[s.selected] ?? noMessages)));
  const history = useAppState((s) => (s.selected === undefined ? undefined : s.history[s.selected]));
  const person = useAppState((s) => (conversation?.person_id === undefined ? undefined : s.persons.find((p) => p.person_id === conversation.person_id)));
  const loadOlder = useCallback(() => void store.loadOlder(), [store]);
  // Whether the newest message is in view, per conversation: each opens there.
  const [bottom, setBottom] = useState({ conversation: selected, atBottom: true });
  const atBottom = bottom.conversation !== selected || bottom.atBottom;
  const onBottomChange = useCallback((value: boolean) => setBottom({ conversation: selected, atBottom: value }), [selected]);
  const visible = useDocumentVisible();
  const unread = (conversation?.unread_count ?? 0) > 0 || conversation?.marked_unread === true;

  // The user reads what the thread shows: the newest message, in a window
  // they can see (API 1.4). markRead clears the state at once, so a message
  // that arrives later makes it unread again and runs this anew.
  useEffect(() => {
    if (selected !== undefined && unread && visible && atBottom) {
      void store.markRead(selected);
    }
  }, [store, selected, unread, visible, atBottom]);

  if (selected === undefined || conversation === undefined) {
    return <p className="empty">Choose a conversation.</p>;
  }
  return (
    <section className="thread" aria-labelledby="thread-title">
      <div className="thread-header">
        {/* Shown on a narrow screen only, where the thread hides the inbox. */}
        <button type="button" className="secondary back" aria-label="Back to conversations" onClick={() => closeConversation(store)}>
          ‹
        </button>
        <h2 id="thread-title">{person?.name ?? (conversation.name || "Unnamed conversation")}</h2>
      </div>
      {person === undefined ? null : <Switcher personId={person.person_id} name={person.name} selected={selected} />}
      {/* One key for both: siblings with the same key confuse React, which
          then leaves old rows in the DOM. */}
      <Fragment key={selected}>
        <MessageList messages={messages} history={history} onOlder={loadOlder} labelledBy="thread-title" onBottomChange={onBottomChange} />
        <Composer />
      </Fragment>
    </section>
  );
}

/**
 * The conversations merged into a person's thread (docs/ADR/0021), one per
 * network account: the user picks the one to read and write in. Lists the
 * conversations read so far.
 */
function Switcher({ personId, name, selected }: { personId: string; name: string; selected: string }) {
  const store = useStore();
  const conversations = useAppState((s) => s.conversations);
  const accounts = useAppState((s) => s.accounts);
  const networks = useAppState((s) => s.networks);
  const merged = useMemo(() => conversations.filter((c) => c.person_id === personId), [conversations, personId]);
  if (merged.length < 2) {
    return null;
  }
  function label(conversation: Conversation): string {
    const network = networks.find((n) => n.network_id === conversation.network_id)?.name ?? conversation.network_id;
    const account = accounts.find((a) => a.account_id === conversation.account_id)?.name;
    return account ? `${network}: ${account}` : network;
  }
  return (
    <fieldset className="switcher">
      <legend className="visually-hidden">Conversations with {name}</legend>
      {merged.map((conversation) => (
        <button
          key={conversation.conversation_id}
          type="button"
          className="secondary"
          aria-pressed={conversation.conversation_id === selected}
          onClick={() => void store.select(conversation.conversation_id)}
        >
          {label(conversation)}
        </button>
      ))}
    </fieldset>
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

function subscribeVisibility(listener: () => void): () => void {
  document.addEventListener("visibilitychange", listener);
  return () => document.removeEventListener("visibilitychange", listener);
}

/** Whether the page can be seen: not in a minimized window or a hidden tab. */
function useDocumentVisible(): boolean {
  return useSyncExternalStore(subscribeVisibility, () => document.visibilityState === "visible");
}
