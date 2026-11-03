// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

import { useEffect, useRef, useState } from "react";
import { NOTIFICATION_PREVIEW_NAMES, NOTIFICATION_PREVIEWS, pauseChoices } from "../notification-preview";
import { useNow } from "./Inbox";
import type { NotificationPreview, ShellNotifications } from "../shell";

/**
 * The notification settings, in a modal dialog (docs/ADR/0051): what a
 * notification shows of a message, kept by the shell, which posts them.
 */
export function NotificationSettings({ notifications, onClose }: { notifications: ShellNotifications; onClose: () => void }) {
  const dialog = useRef<HTMLDialogElement>(null);
  const [preview, setPreview] = useState<NotificationPreview | undefined>(undefined);
  const [failed, setFailed] = useState(false);
  // Until when notifications wait (docs/ADR/0067); undefined while read.
  const [pausedUntil, setPausedUntil] = useState<number | undefined>(undefined);
  const now = useNow();

  useEffect(() => {
    const element = dialog.current;
    // As AppearanceSettings: no close() on cleanup (StrictMode).
    if (element !== null && !element.open) {
      element.showModal();
    }
    let current = true;
    if (notifications.preview === undefined) {
      return;
    }
    notifications.preview().then(
      (value) => current && setPreview(value),
      () => current && setFailed(true),
    );
    notifications.pausedUntil?.().then(
      (value) => current && setPausedUntil(value),
      () => current && setFailed(true),
    );
    return () => {
      current = false;
    };
  }, [notifications]);

  function pause(until: number) {
    const before = pausedUntil;
    setPausedUntil(until);
    notifications.pauseUntil?.(until).catch(() => {
      setPausedUntil(before);
      setFailed(true);
    });
  }

  function change(next: NotificationPreview) {
    const before = preview;
    setPreview(next);
    setFailed(false);
    (notifications.setPreview?.(next) ?? Promise.reject(new Error("unsupported"))).catch(() => {
      setPreview(before);
      setFailed(true);
    });
  }

  return (
    <dialog ref={dialog} className="settings" aria-labelledby="notification-settings-title" onClose={onClose}>
      <h2 id="notification-settings-title">Notifications</h2>
      <fieldset disabled={preview === undefined}>
        <legend>What a notification shows</legend>
        {NOTIFICATION_PREVIEWS.map((value) => (
          <label key={value} className="settings-choice">
            <input type="radio" name="notification-preview" value={value} checked={preview === value} onChange={() => change(value)} />
            {NOTIFICATION_PREVIEW_NAMES[value]}
          </label>
        ))}
      </fieldset>
      <p className="settings-note">Anyone who sees your screen can read what your notifications show.</p>
      {notifications.pauseUntil === undefined || pausedUntil === undefined ? null : (
        <fieldset>
          <legend>Pause notifications</legend>
          {pausedUntil > now ? (
            <>
              <p className="settings-note">
                Paused until {new Date(pausedUntil).toLocaleString(undefined, { weekday: "short", hour: "2-digit", minute: "2-digit" })}.
              </p>
              <button type="button" className="secondary" onClick={() => pause(0)}>
                Resume now
              </button>
            </>
          ) : (
            pauseChoices(now).map((choice) => (
              <button key={choice.label} type="button" className="secondary" onClick={() => pause(choice.until)}>
                {choice.label}
              </button>
            ))
          )}
        </fieldset>
      )}
      {failed ? <p role="alert">Musubee could not read or keep this setting.</p> : null}
      <div className="settings-buttons">
        <button type="button" onClick={() => dialog.current?.close()}>
          Done
        </button>
      </div>
    </dialog>
  );
}
