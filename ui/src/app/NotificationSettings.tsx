// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

import { useEffect, useRef, useState } from "react";
import { NOTIFICATION_PREVIEW_NAMES, NOTIFICATION_PREVIEWS } from "../notification-preview";
import type { NotificationPreview, ShellNotifications } from "../shell";

/**
 * The notification settings, in a modal dialog (docs/ADR/0051): what a
 * notification shows of a message, kept by the shell, which posts them.
 */
export function NotificationSettings({ notifications, onClose }: { notifications: ShellNotifications; onClose: () => void }) {
  const dialog = useRef<HTMLDialogElement>(null);
  const [preview, setPreview] = useState<NotificationPreview | undefined>(undefined);
  const [failed, setFailed] = useState(false);

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
    return () => {
      current = false;
    };
  }, [notifications]);

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
      {failed ? <p role="alert">Musubee could not read or keep this setting.</p> : null}
      <div className="settings-buttons">
        <button type="button" onClick={() => dialog.current?.close()}>
          Done
        </button>
      </div>
    </dialog>
  );
}
