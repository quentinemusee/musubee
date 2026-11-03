// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

import { useEffect, useId, useRef, useState, type FormEvent } from "react";
import type { Contact } from "../core-api/types.gen";
import { normalizeName } from "../merge";
import { openConversation } from "./navigation";
import { useAppState, useStore } from "./state";

/**
 * Starts a conversation (API 1.37, docs/ADR/0069), in a modal dialog: the
 * account to write from, and who, as its network finds people.
 */
export function NewConversation({ onClose }: { onClose: () => void }) {
  const store = useStore();
  const accounts = useAppState((s) => s.accounts);
  const networks = useAppState((s) => s.networks);
  const usable = accounts.filter((a) => a.starts_conversations === true);
  const dialog = useRef<HTMLDialogElement>(null);
  const accountField = useId();
  const whoField = useId();
  const [accountId, setAccountId] = useState(usable[0]?.account_id ?? "");
  const [who, setWho] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | undefined>(undefined);

  useEffect(() => {
    const element = dialog.current;
    // As AppearanceSettings: no close() on cleanup (StrictMode).
    if (element !== null && !element.open) {
      element.showModal();
    }
  }, []);

  // The account's contacts (API 1.38), when its network lists them.
  const listsContacts = usable.find((a) => a.account_id === accountId)?.lists_contacts === true;
  const [contacts, setContacts] = useState<{ account: string; list: readonly Contact[] }>({ account: "", list: [] });
  useEffect(() => {
    if (!listsContacts) {
      return;
    }
    let current = true;
    store.contacts(accountId).then(
      (list) => current && setContacts({ account: accountId, list }),
      () => undefined,
    );
    return () => {
      current = false;
    };
  }, [store, accountId, listsContacts]);
  const wanted = normalizeName(who);
  const suggestions = contacts.account === accountId ? contacts.list.filter((c) => normalizeName(c.name).includes(wanted)).slice(0, 8) : [];

  async function startWith(contact: Contact) {
    setBusy(true);
    setError(undefined);
    try {
      const conversationId = await store.startConversation(accountId, { contact_id: contact.contact_id });
      dialog.current?.close();
      openConversation(store, conversationId);
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : String(reason));
      setBusy(false);
    }
  }

  const label = (account: (typeof accounts)[number]) =>
    `${networks.find((n) => n.network_id === account.network_id)?.name ?? account.network_id}: ${account.name}`;

  async function start(event: FormEvent) {
    event.preventDefault();
    setBusy(true);
    setError(undefined);
    try {
      const conversationId = await store.startConversation(accountId, { identifier: who.trim() });
      dialog.current?.close();
      openConversation(store, conversationId);
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : String(reason));
      setBusy(false);
    }
  }

  return (
    <dialog ref={dialog} className="settings" aria-labelledby="new-conversation-title" onClose={onClose}>
      <h2 id="new-conversation-title">New conversation</h2>
      {usable.length === 0 ? (
        <p className="settings-note">None of your accounts can find people yet.</p>
      ) : (
        <form className="new-conversation" onSubmit={(event) => void start(event)}>
          <label htmlFor={accountField}>From</label>
          <select id={accountField} value={accountId} onChange={(event) => setAccountId(event.target.value)}>
            {usable.map((account) => (
              <option key={account.account_id} value={account.account_id}>
                {label(account)}
              </option>
            ))}
          </select>
          <label htmlFor={whoField}>To</label>
          <input
            id={whoField}
            value={who}
            placeholder="A username, a phone number or a name"
            autoComplete="off"
            onChange={(event) => setWho(event.target.value)}
          />
          {suggestions.length === 0 ? null : (
            <ul className="contact-suggestions" aria-label="Contacts">
              {suggestions.map((contact) => (
                <li key={contact.contact_id}>
                  <button type="button" className="secondary" disabled={busy} onClick={() => void startWith(contact)}>
                    {contact.name || "Unnamed contact"}
                  </button>
                </li>
              ))}
            </ul>
          )}
          {error === undefined ? null : <p role="alert">Musubee could not start it: {error}</p>}
          <div className="settings-buttons">
            <button type="button" className="secondary" onClick={() => dialog.current?.close()}>
              Cancel
            </button>
            <button type="submit" disabled={busy || who.trim() === "" || accountId === ""}>
              {busy ? "Starting…" : "Start"}
            </button>
          </div>
        </form>
      )}
      {usable.length === 0 ? (
        <div className="settings-buttons">
          <button type="button" onClick={() => dialog.current?.close()}>
            Done
          </button>
        </div>
      ) : null}
    </dialog>
  );
}
