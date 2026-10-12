// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

import { useState } from "react";
import type { Account } from "../core-api/types.gen";
import type { CoreStatus } from "../shell";
import { AddAccount } from "./AddAccount";
import { Inbox } from "./Inbox";
import { useBackToInbox } from "./navigation";
import { useAppState, useStore } from "./state";
import { Thread } from "./Thread";

/** What the login screen does: add an account, or log in to one again. */
type Adding = { again?: { account: Account; label: string } };

export function App() {
  const store = useStore();
  const [adding, setAdding] = useState<Adding | null>(null);
  const loaded = useAppState((s) => s.loaded);
  const hasAccounts = useAppState((s) => s.accounts.length > 0);
  const view = useAppState((s) => (s.selected === undefined ? "inbox" : "thread"));
  useBackToInbox(store);
  return (
    <div className="app">
      <header className="app-header">
        <h1>Musubee</h1>
        <StatusLine />
      </header>
      <ErrorBanner />
      {adding !== null || (loaded && !hasAccounts) ? (
        <main className="app-main">
          <AddAccount
            key={adding?.again?.account.account_id ?? ""}
            onDone={() => setAdding(null)}
            canCancel={hasAccounts}
            {...(adding?.again === undefined ? {} : { again: adding.again })}
          />
        </main>
      ) : (
        // On a narrow screen, the inbox or the thread alone (data-view).
        <div className="app-body" data-view={view}>
          <nav className="sidebar" aria-label="Conversations">
            <Accounts onLogInAgain={(account, label) => setAdding({ again: { account, label } })} />
            <button type="button" className="secondary" onClick={() => setAdding({})}>
              Add an account
            </button>
            <Inbox />
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
    <output className="status" aria-label="App status" data-state={status.state}>
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

function Accounts({ onLogInAgain }: { onLogInAgain: (account: Account, label: string) => void }) {
  const accounts = useAppState((s) => s.accounts);
  const networks = useAppState((s) => s.networks);
  if (accounts.length === 0) {
    return null;
  }
  return (
    <section aria-labelledby="accounts-title">
      <h2 id="accounts-title">Accounts</h2>
      <ul className="accounts">
        {accounts.map((account) => {
          const label = `${networks.find((n) => n.network_id === account.network_id)?.name ?? account.network_id}${account.name ? `: ${account.name}` : ""}`;
          return <AccountItem key={account.account_id} account={account} label={label} onLogInAgain={() => onLogInAgain(account, label)} />;
        })}
      </ul>
    </section>
  );
}

// Logging out removes the account's conversations from the device: the
// button asks for a confirmation first. An account whose session the network
// ended offers to log in to it again, which keeps them.
function AccountItem({ account, label, onLogInAgain }: { account: Account; label: string; onLogInAgain: () => void }) {
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
        <span className="account-actions">
          {account.state === "bad_credentials" ? (
            <button type="button" aria-label={`Log in again to ${label}`} onClick={onLogInAgain}>
              Log in again
            </button>
          ) : null}
          <button type="button" className="link" aria-label={`Log out of ${label}`} onClick={() => setConfirming(true)}>
            Log out…
          </button>
        </span>
      )}
    </li>
  );
}
