// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

// Adds an account by walking the steps of a network's login flow, as the
// core describes them (login.* commands): the interface knows no network.

import { useEffect, useState, type FormEvent } from "react";
import type { LoginField, LoginStep } from "../core-api/types.gen";
import { useAppState, useStore } from "./state";

interface Props {
  onDone: () => void;
  canCancel: boolean;
}

export function AddAccount({ onDone, canCancel }: Props) {
  const store = useStore();
  const networks = useAppState((s) => s.networks);
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
      <h2 id="add-account-title">Add an account</h2>
      {error !== null ? (
        <p className="form-error" role="alert">
          {error}
        </p>
      ) : null}
      {step === null ? (
        <ul className="networks">
          {networks.map((network) => (
            <li key={network.network_id}>
              <h3>{network.name}</h3>
              {network.login_flows.map((flow) => (
                <button
                  key={flow.flow_id}
                  type="button"
                  disabled={busy}
                  title={flow.description}
                  onClick={() =>
                    void run(() => store.client.call("login.start", { network_id: network.network_id, flow_id: flow.flow_id }))
                  }
                >
                  {`${network.name}: ${flow.name}`}
                </button>
              ))}
            </li>
          ))}
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
          {step.display?.data ? <output className="login-code">{step.display.data}</output> : null}
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

const INPUT_TYPES: Record<string, string> = {
  password: "password",
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
          autoComplete={field.type === "password" ? "current-password" : "off"}
          {...(field.pattern ? { pattern: field.pattern } : {})}
          onChange={(event) => onChange(event.target.value)}
        />
      )}
      {field.description ? <p className="hint">{field.description}</p> : null}
    </div>
  );
}
