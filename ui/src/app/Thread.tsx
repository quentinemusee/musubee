// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

import { useCallback, useEffect, useMemo, useRef, useState, useSyncExternalStore, type ClipboardEvent, type FormEvent, type KeyboardEvent } from "react";
import type { ConversationActions, MediaActions, Message } from "../core-api/types.gen";
import { accepts, formatSize } from "../media";
import { canRecord, VoiceRecorder, type Recording } from "../recorder";
import { accountLabel } from "../merge";
import { replyQuote, replyTo } from "../message";
import { MessageList } from "./MessageList";
import { VoiceRecording } from "./VoiceRecording";
import type { MessageHandlers } from "./MessageRow";
import { closeConversation } from "./navigation";
import { PersonPanel } from "./PersonPanel";
import { PrivacyNote } from "./PrivacyNote";
import { togetherView } from "../together";
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
  // A person's conversations as one thread (docs/ADR/0042).
  const together = useAppState((s) => (s.together !== undefined && s.together === conversation?.person_id ? s.together : undefined));
  const allConversations = useAppState((s) => s.conversations);
  const allMessages = useAppState((s) => s.messages);
  const allHistory = useAppState((s) => s.history);
  const networks = useAppState((s) => s.networks);
  const accounts = useAppState((s) => s.accounts);
  const view = useMemo(() => {
    if (together === undefined) {
      return undefined;
    }
    const merged = allConversations.filter((c) => c.person_id === together);
    const thread = togetherView(
      merged.map((c) => ({ conversationId: c.conversation_id, messages: allMessages[c.conversation_id], history: allHistory[c.conversation_id] })),
    );
    const names = merged.map((c) => networks.find((n) => n.network_id === c.network_id)?.name ?? c.network_id);
    const via: Record<string, string> = {};
    const actions: Record<string, ConversationActions | undefined> = {};
    merged.forEach((c, i) => {
      const name = names[i] ?? c.network_id;
      // Two accounts on one network: say which.
      via[c.conversation_id] = names.filter((n) => n === name).length > 1 ? accountLabel(c, networks, accounts) : name;
      actions[c.conversation_id] = c.actions;
    });
    return { thread, list: { via, actions }, merged };
  }, [together, allConversations, allMessages, allHistory, networks, accounts]);
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
      // In a person's thread, answering or editing a message writes where
      // it came from.
      reply: (message) => {
        if (message.conversation_id !== store.getState().selected) {
          void store.select(message.conversation_id);
        }
        setDraft({ mode: "reply", message });
      },
      edit: (message) => {
        if (message.conversation_id !== store.getState().selected) {
          void store.select(message.conversation_id);
        }
        setDraft({ mode: "edit", message });
      },
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
  // In a person's thread, every conversation shown is read.
  useEffect(() => {
    if (view === undefined || !visible || !atBottom) {
      return;
    }
    for (const c of view.merged) {
      if ((c.unread_count ?? 0) > 0 || c.marked_unread === true) {
        void store.markRead(c.conversation_id);
      }
    }
  }, [store, view, visible, atBottom]);

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
          <button type="button" className="secondary person-button" aria-expanded={panelOpen} onClick={() => setPanelFor(panelOpen ? undefined : selected)}>
            {person === undefined ? "Same person…" : "Person…"}
          </button>
        ) : null}
      </div>
      <PrivacyNote conversation={conversation} />
      {panelOpen ? <PersonPanel conversation={conversation} onClose={() => setPanelFor(undefined)} /> : null}
      {person === undefined ? null : <Switcher personId={person.person_id} name={person.name} selected={selected} together={together !== undefined} />}
      {/* Distinct keys: siblings with the same key confuse React, which then
          leaves old rows in the DOM. A person's thread keeps its list when
          the user writes elsewhere. */}
      <MessageList
        key={together === undefined ? `conversation:${selected}` : `person:${together}`}
        messages={view?.thread.messages ?? messages}
        history={view?.thread.history ?? history}
        onOlder={loadOlder}
        labelledBy="thread-title"
        onBottomChange={onBottomChange}
        actions={conversation.actions}
        handlers={handlers}
        together={view?.list}
      />
      <Composer
        key={`composer:${selected}`}
        draft={draft}
        onDraftDone={() => setDraft(undefined)}
        media={conversation.actions?.media}
        writingWith={view === undefined ? undefined : (view.list.via[selected] ?? conversation.network_id)}
      />
    </section>
  );
}

/**
 * The conversations merged into a person's thread (docs/ADR/0021), one per
 * network account: the user picks the one to read and write in. Lists the
 * conversations read so far.
 */
function Switcher({ personId, name, selected, together }: { personId: string; name: string; selected: string; together: boolean }) {
  const store = useStore();
  const conversations = useAppState((s) => s.conversations);
  const accounts = useAppState((s) => s.accounts);
  const networks = useAppState((s) => s.networks);
  const merged = useMemo(() => conversations.filter((c) => c.person_id === personId), [conversations, personId]);
  if (merged.length < 2) {
    return null;
  }
  return (
    <div className="switcher">
      {/* Every conversation in one thread, or one at a time (docs/ADR/0042). */}
      <button
        type="button"
        className="secondary switcher-together"
        aria-pressed={together}
        onClick={() => (together ? store.showSeparately() : void store.showTogether())}
      >
        All together
      </button>
      <fieldset>
        <legend className="visually-hidden">{together ? `Write to ${name} with` : `Conversations with ${name}`}</legend>
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
    </div>
  );
}

/** A message the composer answers (reply) or replaces the text of (edit). */
interface Draft {
  mode: "reply" | "edit";
  message: Message;
}

interface ComposerProps {
  draft: Draft | undefined;
  onDraftDone: () => void;
  /** The media the conversation takes (API 1.10); none: no attaching. */
  media: MediaActions | undefined;
  /** In a person's thread: the network the user writes with (docs/ADR/0042). */
  writingWith?: string | undefined;
}

function Composer({ draft, onDraftDone, media, writingWith }: ComposerProps) {
  const store = useStore();
  const [text, setText] = useState("");
  const input = useRef<HTMLInputElement>(null);
  const picker = useRef<HTMLInputElement>(null);
  // The file the user attached, sent with the text as its caption.
  const [file, setFile] = useState<File>();
  const upload = useAppState((s) => s.upload);
  const canAttach = media !== undefined && draft?.mode !== "edit";
  const [recorder, setRecorder] = useState<VoiceRecorder>();
  const [starting, setStarting] = useState(false);
  // The microphone button stands in for Send while there is nothing to send.
  const canRecordHere = canAttach && file === undefined && text.trim() === "" && canRecord();

  function startRecording() {
    setStarting(true);
    // A type the network takes for voice messages, else for audio.
    const action = media?.voice ?? media?.audio;
    VoiceRecorder.start((type) => accepts(action, type)).then(
      (started) => {
        setStarting(false);
        setRecorder(started);
      },
      (error: unknown) => {
        setStarting(false);
        const refused = error instanceof DOMException && (error.name === "NotAllowedError" || error.name === "SecurityError");
        store.showError(
          refused
            ? "Musubee may not use the microphone: allow it in the system's settings, then try again."
            : `Could not record a voice message: ${error instanceof Error ? error.message : String(error)}`,
        );
      },
    );
  }

  function sendRecording(recording: Recording, name: string) {
    const answered = draft?.message.message_id;
    setRecorder(undefined);
    const voice = new File([recording.blob], name, { type: recording.type });
    void store.sendMedia(voice, "", answered, { voice: true, durationMs: recording.durationMs }).then((sent) => {
      if (sent) {
        onDraftDone();
      }
    });
    input.current?.focus();
  }
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
    if (file !== undefined && canAttach) {
      if (upload !== undefined) {
        return;
      }
      const answered = draft?.message.message_id;
      void store.sendMedia(file, message, answered).then((sent) => {
        if (sent) {
          setFile(undefined);
          setText("");
          onDraftDone();
        }
      });
      return;
    }
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

  function attach(picked: File | undefined) {
    if (picked !== undefined) {
      setFile(picked);
      input.current?.focus();
    }
  }

  // A pasted image or file is attached, as chat apps do.
  function onPaste(event: ClipboardEvent<HTMLInputElement>) {
    const pasted = event.clipboardData.files[0];
    if (pasted !== undefined && canAttach) {
      event.preventDefault();
      attach(pasted);
    }
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
      {file !== undefined && canAttach ? (
        <div className="composer-file">
          <p className="composer-draft-text">
            <span className="composer-draft-what">
              {upload === undefined ? "Attached" : `Sending… ${Math.round((upload.sent / Math.max(upload.size, 1)) * 100)}%`}
            </span>
            <span className="media-file-name">{file.name || "file"}</span>
            <span className="media-file-size">{formatSize(file.size)}</span>
          </p>
          {upload === undefined ? (
            <button type="button" className="secondary" onClick={() => setFile(undefined)}>
              Remove<span className="visually-hidden"> {file.name}</span>
            </button>
          ) : (
            <progress max={upload.size} value={upload.sent} aria-label={`Sending ${file.name}`} />
          )}
        </div>
      ) : null}
      {recorder !== undefined ? (
        <VoiceRecording
          recorder={recorder}
          onSend={sendRecording}
          onCancel={() => {
            setRecorder(undefined);
            input.current?.focus();
          }}
        />
      ) : (
        <form className="composer-form" onSubmit={submit}>
          {canAttach ? (
            <>
              <button type="button" className="secondary composer-attach" aria-label="Attach a file" onClick={() => picker.current?.click()}>
                <span aria-hidden="true">📎</span>
              </button>
              <input
                ref={picker}
                type="file"
                hidden
                tabIndex={-1}
                onChange={(event) => {
                  attach(event.target.files?.[0]);
                  event.target.value = "";
                }}
              />
            </>
          ) : null}
          <label htmlFor="composer-text" className="visually-hidden">
            {draft?.mode === "edit" ? "New text of your message" : "Message"}
          </label>
          <input
            ref={input}
            id="composer-text"
            type="text"
            autoComplete="off"
            placeholder={file !== undefined ? "Add a caption…" : writingWith === undefined ? "Write a message…" : `Write with ${writingWith}…`}
            value={text}
            onChange={(event) => setText(event.target.value)}
            onKeyDown={onKeyDown}
            onPaste={onPaste}
          />
          {canRecordHere ? (
            <button
              type="button"
              className="secondary composer-record"
              aria-label="Record a voice message"
              disabled={starting || upload !== undefined}
              onClick={startRecording}
            >
              <span aria-hidden="true">🎤</span>
            </button>
          ) : (
            <button type="submit" disabled={upload !== undefined || (text.trim() === "" && (file === undefined || !canAttach))}>
              {draft?.mode === "edit" ? "Save" : "Send"}
            </button>
          )}
        </form>
      )}
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
