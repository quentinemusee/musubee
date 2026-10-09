// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

import { useState } from "react";
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
          <li key={account.account_id}>
            <span className="account-name">
              {networks.find((n) => n.network_id === account.network_id)?.name ?? account.network_id}
              {account.name ? `: ${account.name}` : ""}
            </span>
            <span className="account-state" data-state={account.state}>
              {account.error || (STATE_LABELS[account.state] ?? account.state)}
            </span>
          </li>
        ))}
      </ul>
    </section>
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
