// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

// Merging the conversations with one person into one thread, from the
// thread itself (E2.5, docs/ADR/0024): find the same person's conversation
// on another network or account, merge it, rename the person, or separate
// the conversations again.

import { useId, useMemo, useState, type FormEvent } from "react";
import type { Conversation, Person } from "../core-api/types.gen";
import { accountLabel, mergeCandidates } from "../merge";
import { useAppState, useStore } from "./state";

export function PersonPanel({ conversation, onClose }: { conversation: Conversation; onClose: () => void }) {
  const person = useAppState((s) => (conversation.person_id === undefined ? undefined : s.persons.find((p) => p.person_id === conversation.person_id)));
  const titleId = useId();
  return (
    <section className="person-panel" aria-labelledby={titleId}>
      <h3 id={titleId}>{person === undefined ? "Same person on another network?" : `Conversations with ${person.name}`}</h3>
      {/* Keyed by the name: a rename elsewhere resets the form. */}
      {person === undefined ? null : <PersonActions key={`${person.person_id}:${person.name}`} person={person} conversation={conversation} />}
      <MergeSearch conversation={conversation} onMerged={onClose} />
      <button type="button" className="secondary" onClick={onClose}>
        Done
      </button>
    </section>
  );
}

function PersonActions({ person, conversation }: { person: Person; conversation: Conversation }) {
  const store = useStore();
  const [name, setName] = useState(person.name);
  const [confirming, setConfirming] = useState(false);
  const [busy, setBusy] = useState(false);
  const nameId = useId();
  const trimmed = name.trim();

  async function rename(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setBusy(true);
    await store.renamePerson(person.person_id, trimmed);
    setBusy(false);
  }

  async function run(action: () => Promise<void>) {
    setBusy(true);
    await action();
    setBusy(false);
    setConfirming(false);
  }

  return (
    <>
      <form className="person-rename" onSubmit={(event) => void rename(event)}>
        <label htmlFor={nameId}>Name</label>
        <input id={nameId} type="text" autoComplete="off" value={name} onChange={(event) => setName(event.target.value)} />
        <button type="submit" className="secondary" disabled={busy || trimmed === "" || trimmed === person.name}>
          Rename
        </button>
      </form>
      {confirming ? (
        <fieldset className="person-actions">
          <legend className="hint">Separate all of {person.name}'s conversations? Their messages stay.</legend>
          <button type="button" className="danger" disabled={busy} onClick={() => void run(() => store.unmergePerson(person.person_id))}>
            Separate all
          </button>
          <button type="button" className="secondary" disabled={busy} onClick={() => setConfirming(false)}>
            Keep
          </button>
        </fieldset>
      ) : (
        <div className="person-actions">
          <button type="button" className="secondary" disabled={busy} onClick={() => void run(() => store.separateConversation(conversation.conversation_id))}>
            Separate this conversation
          </button>
          {person.conversation_ids.length > 1 ? (
            <button type="button" className="secondary" disabled={busy} onClick={() => setConfirming(true)}>
              Separate all…
            </button>
          ) : null}
        </div>
      )}
    </>
  );
}

function MergeSearch({ conversation, onMerged }: { conversation: Conversation; onMerged: () => void }) {
  const store = useStore();
  const conversations = useAppState((s) => s.conversations);
  const persons = useAppState((s) => s.persons);
  const networks = useAppState((s) => s.networks);
  const accounts = useAppState((s) => s.accounts);
  const inbox = useAppState((s) => s.inbox);
  const [query, setQuery] = useState("");
  const [busy, setBusy] = useState(false);
  const searchId = useId();
  const candidates = useMemo(() => mergeCandidates(conversations, persons, conversation, query), [conversations, persons, conversation, query]);

  async function merge(other: Conversation) {
    setBusy(true);
    const merged = await store.mergeWith(conversation.conversation_id, other.conversation_id);
    setBusy(false);
    if (merged) {
      onMerged();
    }
  }

  return (
    <div className="merge-search">
      <label htmlFor={searchId}>Find their conversation on another network or account</label>
      <input id={searchId} type="search" autoComplete="off" placeholder="Name…" value={query} onChange={(event) => setQuery(event.target.value)} />
      {candidates.length === 0 ? (
        <p className="hint">No other direct conversation matches.</p>
      ) : (
        <ul className="merge-candidates">
          {candidates.map(({ conversation: other, likely }) => {
            const name = other.name || "Unnamed conversation";
            const label = accountLabel(other, networks, accounts);
            return (
              <li key={other.conversation_id}>
                <span className="candidate-name">{name}</span>
                <span className="hint">
                  {label}
                  {likely ? " · same name" : ""}
                </span>
                <button type="button" className="secondary" disabled={busy} aria-label={`Merge with ${name} (${label})`} onClick={() => void merge(other)}>
                  Merge
                </button>
              </li>
            );
          })}
        </ul>
      )}
      {/* Only the conversations read so far are searched. */}
      {inbox !== "end" ? (
        <button type="button" className="link" disabled={inbox === "loading"} onClick={() => void store.loadMoreConversations()}>
          Search more conversations
        </button>
      ) : null}
    </div>
  );
}
