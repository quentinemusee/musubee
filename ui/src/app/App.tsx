// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

import { useState } from "react";
import type { Account } from "../core-api/types.gen";
import type { CoreStatus } from "../shell";
import { AddAccount } from "./AddAccount";
import { useAppState, useStore } from "./state";
import { Thread } from "./Thread";

export function App() {
  const [addingAccount, setAddingAccount] = useState(false);
  const loaded = useAppState((s) => s.loaded);
  const hasAccounts = useAppState((s) => s.accounts.length > 0);
  return (
    <div className="app">
      <header className="app-header">
        <h1>Musubee</h1>
        <StatusLine />
      </header>
      <ErrorBanner />
      {addingAccount || (loaded && !hasAccounts) ? (
        <main className="app-main">
          <AddAccount onDone={() => setAddingAccount(false)} canCancel={hasAccounts} />
        </main>
      ) : (
        <div className="app-body">
          <nav className="sidebar" aria-label="Conversations">
            <Accounts />
            <button type="button" className="secondary" onClick={() => setAddingAccount(true)}>
              Add an account
            </button>
            <ConversationList />
          </nav>
          <main className="app-main">
            <Thread />
          </main>
        </div>
      )}
    </div>
  );
}

function statusText(status: CoreStatus, loaded: boolean): string {
  switch (status.state) {
    case "starting":
      return "Starting…";
    case "ready":
      return loaded ? "Ready" : "Loading your conversations…";
    case "restarting":
      return `Something went wrong; restarting (attempt ${status.attempt})…`;
    case "failed":
      return `Musubee could not start: ${status.reason}`;
  }
}

function StatusLine() {
  const status = useAppState((s) => s.status);
  const loaded = useAppState((s) => s.loaded);
  return (
    <output className="status" data-state={status.state}>
      {statusText(status, loaded)}
    </output>
  );
}

function ErrorBanner() {
  const store = useStore();
  const error = useAppState((s) => s.error);
  if (error === undefined) {
    return null;
  }
  return (
    <div className="error-banner" role="alert">
      <span>{error}</span>
      <button type="button" className="secondary" onClick={() => store.dismissError()}>
        Dismiss
      </button>
    </div>
  );
}

const STATE_LABELS: Record<string, string> = {
  connecting: "Connecting…",
  connected: "Connected",
  transient_disconnect: "Reconnecting…",
  bad_credentials: "Needs you to log in again",
  logged_out: "Logged out",
  unknown_error: "Not working",
};

function Accounts() {
  const accounts = useAppState((s) => s.accounts);
  const networks = useAppState((s) => s.networks);
  if (accounts.length === 0) {
    return null;
  }
  return (
    <section aria-labelledby="accounts-title">
      <h2 id="accounts-title">Accounts</h2>
      <ul className="accounts">
        {accounts.map((account) => (
          <AccountItem
            key={account.account_id}
            account={account}
            label={`${networks.find((n) => n.network_id === account.network_id)?.name ?? account.network_id}${account.name ? `: ${account.name}` : ""}`}
          />
        ))}
      </ul>
    </section>
  );
}

// Logging out removes the account's conversations from the device: the
// button asks for a confirmation first.
function AccountItem({ account, label }: { account: Account; label: string }) {
  const store = useStore();
  const [confirming, setConfirming] = useState(false);
  const [busy, setBusy] = useState(false);

  async function logout() {
    setBusy(true);
    await store.logout(account.account_id);
    setBusy(false);
    setConfirming(false);
  }

  return (
    <li>
      <span className="account-name">{label}</span>
      <span className="account-state" data-state={account.state}>
        {account.error || (STATE_LABELS[account.state] ?? account.state)}
      </span>
      {confirming ? (
        <fieldset className="account-actions">
          <legend className="hint">Log out? Its conversations will be removed from this device.</legend>
          <button type="button" className="danger" disabled={busy} onClick={() => void logout()}>
            Log out
          </button>
          <button type="button" className="secondary" disabled={busy} onClick={() => setConfirming(false)}>
            Keep
          </button>
        </fieldset>
      ) : (
        <button type="button" className="link" aria-label={`Log out of ${label}`} onClick={() => setConfirming(true)}>
          Log out…
        </button>
      )}
    </li>
  );
}

function ConversationList() {
  const store = useStore();
  const conversations = useAppState((s) => s.conversations);
  const selected = useAppState((s) => s.selected);
  return (
    <section aria-labelledby="conversations-title">
      <h2 id="conversations-title">Conversations</h2>
      {conversations.length === 0 ? (
        <p className="empty">No conversations yet.</p>
      ) : (
        <ul className="conversations">
          {conversations.map((conversation) => (
            <li key={conversation.conversation_id}>
              <button
                type="button"
                className="conversation"
                aria-current={conversation.conversation_id === selected ? "true" : undefined}
                onClick={() => void store.select(conversation.conversation_id)}
              >
                {conversation.name || "Unnamed conversation"}
              </button>
            </li>
          ))}
        </ul>
      )}
    </section>
  );
}
