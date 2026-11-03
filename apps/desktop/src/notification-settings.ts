// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

// What the desktop's notifications show of a message (docs/ADR/0051), kept
// in the app's data directory: nothing in it is private, and the main
// process, which shows the notifications, reads it before any window.
//
// No Electron import, so that it runs under plain Node in the tests.

import { readFileSync, renameSync, writeFileSync } from "node:fs";
import { join } from "node:path";
import { parsePreview, type NotificationPreview } from "./notification-rules.js";

const FILE = "notifications.json";

/** The notification settings the main process keeps. */
export interface NotificationSettings {
  preview: NotificationPreview;
  /** Until when every notification waits, in Unix milliseconds; 0: not paused (docs/ADR/0067). */
  pausedUntil: number;
}

/** The stored settings; each one the default when missing, unreadable or unknown. */
export function loadSettings(dir: string): NotificationSettings {
  let stored: { preview?: unknown; paused_until?: unknown } = {};
  try {
    stored = JSON.parse(readFileSync(join(dir, FILE), "utf8")) as typeof stored;
  } catch {
    // The defaults.
  }
  const pausedUntil = stored.paused_until;
  return {
    preview: parsePreview(stored.preview) ?? "name_and_text",
    pausedUntil: typeof pausedUntil === "number" && Number.isSafeInteger(pausedUntil) && pausedUntil > 0 ? pausedUntil : 0,
  };
}

/** Keeps the settings, replacing the file whole so that a crash never leaves half of it. */
export function saveSettings(dir: string, settings: NotificationSettings): void {
  const path = join(dir, FILE);
  writeFileSync(`${path}.tmp`, JSON.stringify({ preview: settings.preview, paused_until: settings.pausedUntil }));
  renameSync(`${path}.tmp`, path);
}

/** Whether notifications wait, as of now. */
export function paused(settings: NotificationSettings, now: number): boolean {
  return settings.pausedUntil > now;
}
