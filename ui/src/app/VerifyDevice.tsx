// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

import { useEffect, useId, useRef, useState, type FormEvent } from "react";
import { useStore } from "./state";

type Outcome = { state: "idle" } | { state: "busy" } | { state: "done"; backupRestored: boolean } | { state: "failed"; reason: string };

/**
 * Verifies this device for a Matrix account with its recovery key
 * (docs/ADR/0044), in a modal dialog. The key field is a password field:
 * not shown over a shoulder, not remembered by the browser, not checked
 * for spelling; it is cleared once used.
 */
export function VerifyDevice({ accountId, label, onClose }: { accountId: string; label: string; onClose: () => void }) {
  const store = useStore();
  const dialog = useRef<HTMLDialogElement>(null);
  const [key, setKey] = useState("");
  const [outcome, setOutcome] = useState<Outcome>({ state: "idle" });
  const fieldId = useId();
  const hintId = useId();

  useEffect(() => {
    // No close() on cleanup (see AppearanceSettings).
    const element = dialog.current;
    if (element !== null && !element.open) {
      element.showModal();
    }
  }, []);

  async function verify(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const typed = key.trim();
    if (typed === "") {
      return;
    }
    setOutcome({ state: "busy" });
    const result = await store.verifyAccount(accountId, typed);
    setKey("");
    setOutcome(result.ok ? { state: "done", backupRestored: result.backupRestored } : { state: "failed", reason: result.reason });
  }

  return (
    <dialog ref={dialog} className="settings" aria-labelledby={`${fieldId}-title`} onClose={onClose}>
      <h2 id={`${fieldId}-title`}>Verify this device</h2>
      {outcome.state === "done" ? (
        <>
          <output className="verify-outcome">
            This device is verified for {label}.{" "}
            {outcome.backupRestored
              ? "Your key backup is restored: older encrypted messages can be read, and new keys are backed up."
              : "This account has no key backup: older encrypted messages stay unreadable."}
          </output>
          <div className="settings-buttons">
            <button type="button" onClick={() => dialog.current?.close()}>
              Done
            </button>
          </div>
        </>
      ) : (
        <form onSubmit={(event) => void verify(event)}>
          <p id={hintId} className="hint">
            Type the recovery key that your Matrix app gave you when you set up secure backup for {label}. Other people's apps will then trust this device, and
            your encrypted history comes back. Musubee does not keep the key.
          </p>
          <label htmlFor={fieldId}>Recovery key</label>
          <input
            id={fieldId}
            type="password"
            autoComplete="off"
            spellCheck={false}
            aria-describedby={hintId}
            value={key}
            onChange={(event) => setKey(event.target.value)}
          />
          {outcome.state === "failed" ? (
            <p role="alert" className="error-text">
              {outcome.reason}
            </p>
          ) : null}
          <div className="settings-buttons">
            <button type="button" className="secondary" onClick={() => dialog.current?.close()}>
              Cancel
            </button>
            <button type="submit" disabled={outcome.state === "busy" || key.trim() === ""}>
              {outcome.state === "busy" ? "Verifying…" : "Verify"}
            </button>
          </div>
        </form>
      )}
    </dialog>
  );
}
