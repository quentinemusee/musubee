// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

// Adds an account by walking the steps of a network's login flow, as the
// core describes them (login.* commands): the interface knows no network.
// Given an account, logs in to it again instead, with the flows of its
// network only (login.start's account_id).

import { useEffect, useState, type FormEvent } from "react";
import type { Account, LoginField, LoginFlow, LoginStep, Network } from "../core-api/types.gen";
import { QrCode } from "./QrCode";
import { useAppState, useStore } from "./state";

interface Props {
  onDone: () => void;
  canCancel: boolean;
  /** The account to log in to again, with its label. */
  again?: { account: Account; label: string };
  /** Says what Musubee is first: the user has no account yet. */
  welcome?: boolean;
}

export function AddAccount({ onDone, canCancel, again, welcome = false }: Props) {
  const store = useStore();
  const allNetworks = useAppState((s) => s.networks);
  const networks = again === undefined ? allNetworks : allNetworks.filter((n) => n.network_id === again.account.network_id);
  const [step, setStep] = useState<LoginStep | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  async function run(action: () => Promise<LoginStep>) {
    setBusy(true);
    setError(null);
    try {
      const next = await action();
      if (next.type === "complete") {
        onDone();
        return;
      }
      setStep(next);
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
    } finally {
      setBusy(false);
    }
  }

  // A display_and_wait step ends by itself (a code confirmed elsewhere).
  useEffect(() => {
    if (step?.type !== "display_and_wait") {
      return;
    }
    let current = true;
    store.client.call("login.wait", { process_id: step.process_id }).then(
      (next) => {
        if (!current) {
          return;
        }
        if (next.type === "complete") {
          onDone();
        } else {
          setStep(next);
        }
      },
      (e: unknown) => {
        if (current) {
          setError(e instanceof Error ? e.message : String(e));
          setStep(null);
        }
      },
    );
    return () => {
      current = false;
    };
  }, [step, store, onDone]);

  function cancel() {
    if (step !== null) {
      void store.client.call("login.cancel", { process_id: step.process_id }).catch(() => {});
    }
    setStep(null);
    if (canCancel) {
      onDone();
    }
  }

  return (
    <section className="add-account" aria-labelledby="add-account-title">
      {welcome && step === null ? <Welcome /> : null}
      <h2 id="add-account-title">{again === undefined ? "Add an account" : `Log in again to ${again.label}`}</h2>
      {again !== undefined && step === null ? (
        <p className="hint">Its conversations stay on this device while you log in.</p>
      ) : null}
      {error !== null ? (
        <p className="form-error" role="alert">
          {error}
        </p>
      ) : null}
      {step === null ? (
        <ul className="networks">
          {networks.map((network) => {
            const flows = network.login_flows.filter((flow) => flow.advanced !== true);
            const advanced = network.login_flows.filter((flow) => flow.advanced === true);
            const flowButton = (flow: LoginFlow) => (
              <FlowButton
                key={flow.flow_id}
                network={network}
                flow={flow}
                disabled={busy}
                onClick={() =>
                  void run(() =>
                    store.client.call("login.start", {
                      network_id: network.network_id,
                      flow_id: flow.flow_id,
                      ...(again === undefined ? {} : { account_id: again.account.account_id }),
                    }),
                  )
                }
              />
            );
            return (
              <li key={network.network_id} className="network">
                <h3>{network.name}</h3>
                {network.description ? <p className="hint">{network.description}</p> : null}
                <div className="flows">{flows.map(flowButton)}</div>
                {advanced.length > 0 ? (
                  <details className="advanced-flows">
                    <summary>Other ways to log in</summary>
                    <div className="flows">{advanced.map(flowButton)}</div>
                  </details>
                ) : null}
              </li>
            );
          })}
        </ul>
      ) : step.type === "user_input" ? (
        <StepForm
          key={step.process_id + step.step_id}
          step={step}
          busy={busy}
          onSubmit={(values) => void run(() => store.client.call("login.submit", { process_id: step.process_id, values }))}
        />
      ) : (
        <div className="login-display">
          {step.instructions ? <p>{step.instructions}</p> : null}
          {!step.display?.data ? null : step.display.type === "qr" ? (
            <QrCode data={step.display.data} />
          ) : (
            <output className="login-code">{step.display.data}</output>
          )}
          <p className="hint">Waiting for the network…</p>
        </div>
      )}
      {step !== null || canCancel ? (
        <button type="button" className="secondary" onClick={cancel}>
          Cancel
        </button>
      ) : null}
    </section>
  );
}

// The first screen of an app without accounts: what Musubee does, and where
// the messages go, in words that need no knowledge of Matrix or bridges.
// "No Musubee server" holds while every network runs on the device
// (CLAUDE.md, on-device mode); a hosted bridge must change it (docs/ADR/0032).
function Welcome() {
  return (
    <div className="welcome">
      <h2>Welcome to Musubee</h2>
      <p>Add the accounts you already have on other messaging apps, and talk to everyone from one place.</p>
      <p className="hint">
        Musubee connects from this device straight to each network: your messages do not go through a Musubee server.
      </p>
    </div>
  );
}

// A way to log in: its name, then what it asks of the user. Screen readers
// hear the network's name first ("Telegram: With your phone number"), as the
// button stands among those of other networks.
function FlowButton({ network, flow, disabled, onClick }: { network: Network; flow: LoginFlow; disabled: boolean; onClick: () => void }) {
  const descriptionId = `flow-${network.network_id}-${flow.flow_id}`;
  return (
    <button
      type="button"
      className="flow"
      disabled={disabled}
      aria-label={`${network.name}: ${flow.name}`}
      {...(flow.description ? { "aria-describedby": descriptionId } : {})}
      onClick={onClick}
    >
      <span className="flow-name">{flow.name}</span>
      {flow.description ? (
        <span id={descriptionId} className="flow-description">
          {flow.description}
        </span>
      ) : null}
    </button>
  );
}

const INPUT_TYPES: Record<string, string> = {
  password: "password",
  // A token is a password: never shown, never kept by the system.
  token: "password",
  email: "email",
  phone_number: "tel",
  url: "url",
};

function StepForm({ step, busy, onSubmit }: { step: LoginStep; busy: boolean; onSubmit: (values: Record<string, string>) => void }) {
  const fields = step.fields ?? [];
  const [values, setValues] = useState(() => Object.fromEntries(fields.map((f) => [f.field_id, f.default_value ?? ""])));

  function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    onSubmit(values);
  }

  return (
    <form className="step-form" onSubmit={submit}>
      {step.instructions ? <p>{step.instructions}</p> : null}
      {fields.map((field) => (
        <Field
          key={field.field_id}
          field={field}
          value={values[field.field_id] ?? ""}
          onChange={(value) => setValues((v) => ({ ...v, [field.field_id]: value }))}
        />
      ))}
      <button type="submit" disabled={busy}>
        Continue
      </button>
    </form>
  );
}

function Field({ field, value, onChange }: { field: LoginField; value: string; onChange: (value: string) => void }) {
  const id = `field-${field.field_id}`;
  return (
    <div className="field">
      <label htmlFor={id}>{field.name}</label>
      {field.type === "select" ? (
        <select id={id} value={value} required onChange={(event) => onChange(event.target.value)}>
          {(field.options ?? []).map((option) => (
            <option key={option}>{option}</option>
          ))}
        </select>
      ) : (
        <input
          id={id}
          type={INPUT_TYPES[field.type] ?? "text"}
          value={value}
          required
          autoComplete={field.type === "password" ? "current-password" : field.type === "token" ? "new-password" : "off"}
          {...(field.pattern ? { pattern: field.pattern } : {})}
          onChange={(event) => onChange(event.target.value)}
        />
      )}
      {field.description ? <p className="hint">{field.description}</p> : null}
    </div>
  );
}
