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

/** The stored preview; everything when the file is missing, unreadable or holds an unknown value. */
export function loadPreview(dir: string): NotificationPreview {
  try {
    const stored = JSON.parse(readFileSync(join(dir, FILE), "utf8")) as { preview?: unknown };
    return parsePreview(stored.preview) ?? "name_and_text";
  } catch {
    return "name_and_text";
  }
}

/** Keeps the preview, replacing the file whole so that a crash never leaves half of it. */
export function savePreview(dir: string, preview: NotificationPreview): void {
  const path = join(dir, FILE);
  writeFileSync(`${path}.tmp`, JSON.stringify({ preview }));
  renameSync(`${path}.tmp`, path);
}
