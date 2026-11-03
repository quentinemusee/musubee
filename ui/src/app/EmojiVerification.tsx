// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

import { useEffect, useId, useRef } from "react";
import { useAppState, useStore } from "./state";

/**
 * Verifies this device by comparing emoji with another device of the
 * account (API 1.22, docs/ADR/0049), in a modal dialog that follows the
 * verification's steps. Opened by the user (who starts it) or by a request
 * from another device (which the user accepts here).
 */
export function EmojiVerification({ accountId, label, onClose, start }: { accountId: string; label: string; onClose: () => void; start: boolean }) {
  const store = useStore();
  const dialog = useRef<HTMLDialogElement>(null);
  const titleId = useId();
  const state = useAppState((s) => s.verifications?.[accountId]);

  useEffect(() => {
    // No close() on cleanup (see AppearanceSettings).
    const element = dialog.current;
    if (element !== null && !element.open) {
      element.showModal();
    }
    if (start) {
      void store.verification(accountId, "start");
    }
  }, [store, accountId, start]);

  const ended = state?.step === "done" || state?.step === "cancelled";
  function close() {
    if (!ended && state !== undefined) {
      void store.verification(accountId, "cancel");
    }
    dialog.current?.close();
  }

  return (
    <dialog ref={dialog} className="settings verification" aria-labelledby={titleId} onClose={onClose}>
      <h2 id={titleId}>Compare emoji</h2>
      {state === undefined || state.step === "waiting" ? (
        <p>Open your Matrix app on another device signed in to {label}, and accept the verification request there.</p>
      ) : state.step === "incoming" ? (
        <>
          <p>Another device signed in to {label} asks to verify this one.</p>
          <div className="settings-buttons">
            <button type="button" className="secondary" onClick={close}>
              Decline
            </button>
            <button type="button" onClick={() => void store.verification(accountId, "accept")}>
              Accept
            </button>
          </div>
        </>
      ) : state.step === "emoji" ? (
        <>
          <p>Check that the other device shows the same emoji, in the same order.</p>
          <ol className="verification-emoji">
            {state.emoji?.map((e, i) => (
              <li key={i}>
                <span className="verification-emoji-glyph" aria-hidden="true">
                  {e.emoji}
                </span>
                <span>{e.name}</span>
              </li>
            ))}
          </ol>
          <div className="settings-buttons">
            <button type="button" className="secondary" onClick={close}>
              They do not match
            </button>
            <button type="button" onClick={() => void store.verification(accountId, "confirm")}>
              They match
            </button>
          </div>
        </>
      ) : state.step === "confirmed" ? (
        <p>Waiting for the other device to confirm…</p>
      ) : state.step === "done" ? (
        <output className="verify-outcome">This device is verified for {label}. Other people's apps now trust it.</output>
      ) : (
        <p role="alert" className="error-text">
          The verification ended without verifying this device{state.reason ? `: ${state.reason}` : "."}
        </p>
      )}
      {state?.step === "incoming" || state?.step === "emoji" ? null : (
        <div className="settings-buttons">
          <button type="button" onClick={close}>
            {ended ? "Done" : "Cancel"}
          </button>
        </div>
      )}
    </dialog>
  );
}
