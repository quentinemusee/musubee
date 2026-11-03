// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

import { Fragment, useCallback, useEffect, useMemo, useRef, useState, useSyncExternalStore, type FormEvent, type KeyboardEvent } from "react";
import type { Message } from "../core-api/types.gen";
import { accountLabel } from "../merge";
import { replyQuote, replyTo } from "../message";
import { MessageList } from "./MessageList";
import type { MessageHandlers } from "./MessageRow";
import { closeConversation } from "./navigation";
import { PersonPanel } from "./PersonPanel";
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
  // The conversation whose person panel is open, if any: it closes when
  // another conversation is selected.
  const [panelFor, setPanelFor] = useState<string | undefined>(undefined);
  const panelOpen = panelFor !== undefined && panelFor === selected;
  const unread = (conversation?.unread_count ?? 0) > 0 || conversation?.marked_unread === true;
  // The message the composer answers or edits, in the selected
  // conversation only.
  const [draftState, setDraft] = useState<Draft | undefined>(undefined);
  const draft = draftState?.message.conversation_id === selected ? draftState : undefined;
  const handlers = useMemo<MessageHandlers>(
    () => ({
      react: (message, key) => void store.react(message, key),
      unreact: (message, key) => void store.unreact(message, key),
      reply: (message) => setDraft({ mode: "reply", message }),
      edit: (message) => setDraft({ mode: "edit", message }),
      delete: (message) => void store.deleteMessage(message),
    }),
    [store],
  );

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
        {conversation.kind === "direct" ? (
          <button
            type="button"
            className="secondary person-button"
            aria-expanded={panelOpen}
            onClick={() => setPanelFor(panelOpen ? undefined : selected)}
          >
            {person === undefined ? "Same person…" : "Person…"}
          </button>
        ) : null}
      </div>
      {panelOpen ? <PersonPanel conversation={conversation} onClose={() => setPanelFor(undefined)} /> : null}
      {person === undefined ? null : <Switcher personId={person.person_id} name={person.name} selected={selected} />}
      {/* One key for both: siblings with the same key confuse React, which
          then leaves old rows in the DOM. */}
      <Fragment key={selected}>
        <MessageList
          messages={messages}
          history={history}
          onOlder={loadOlder}
          labelledBy="thread-title"
          onBottomChange={onBottomChange}
          actions={conversation.actions}
          handlers={handlers}
        />
        <Composer draft={draft} onDraftDone={() => setDraft(undefined)} />
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
          {accountLabel(conversation, networks, accounts)}
        </button>
      ))}
    </fieldset>
  );
}

/** A message the composer answers (reply) or replaces the text of (edit). */
interface Draft {
  mode: "reply" | "edit";
  message: Message;
}

function Composer({ draft, onDraftDone }: { draft: Draft | undefined; onDraftDone: () => void }) {
  const store = useStore();
  const [text, setText] = useState("");
  const input = useRef<HTMLInputElement>(null);
  // A new draft: an edit starts from the message's text, and leaving an
  // edit clears it (React's "adjust state on a prop change" pattern).
  const [shownDraft, setShownDraft] = useState(draft);
  if (draft !== shownDraft) {
    setShownDraft(draft);
    if (draft?.mode === "edit") {
      setText(draft.message.text);
    } else if (shownDraft?.mode === "edit") {
      setText("");
    }
  }
  // Either kind of draft takes the focus.
  useEffect(() => {
    if (draft !== undefined) {
      input.current?.focus();
    }
  }, [draft]);

  function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const message = text.trim();
    if (message === "") {
      return;
    }
    setText("");
    onDraftDone();
    if (draft?.mode === "edit") {
      if (message !== draft.message.text) {
        void store.edit(draft.message, message);
      }
    } else {
      void store.send(message, draft?.message.message_id);
    }
  }

  function cancel() {
    onDraftDone();
    input.current?.focus();
  }

  function onKeyDown(event: KeyboardEvent<HTMLInputElement>) {
    if (event.key === "Escape" && draft !== undefined) {
      event.preventDefault();
      cancel();
    }
  }

  const quote = draft === undefined ? undefined : replyQuote(replyTo(draft.message));
  return (
    <div className="composer">
      {draft !== undefined && quote !== undefined ? (
        <div className="composer-draft" data-mode={draft.mode}>
          <p className="composer-draft-text">
            <span className="composer-draft-what">{draft.mode === "edit" ? "Editing your message" : `Replying to ${quote.sender || "a message"}`}</span>
            {draft.mode === "reply" ? <span className="reply-text">{quote.text}</span> : null}
          </p>
          <button type="button" className="secondary" onClick={cancel}>
            {draft.mode === "edit" ? "Cancel edit" : "Cancel reply"}
          </button>
        </div>
      ) : null}
      <form className="composer-form" onSubmit={submit}>
        <label htmlFor="composer-text" className="visually-hidden">
          {draft?.mode === "edit" ? "New text of your message" : "Message"}
        </label>
        <input
          ref={input}
          id="composer-text"
          type="text"
          autoComplete="off"
          placeholder="Write a message…"
          value={text}
          onChange={(event) => setText(event.target.value)}
          onKeyDown={onKeyDown}
        />
        <button type="submit" disabled={text.trim() === ""}>
          {draft?.mode === "edit" ? "Save" : "Send"}
        </button>
      </form>
    </div>
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
