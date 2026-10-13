// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

// Starting over once the core's key is lost (docs/ADR/0031). Nothing is
// deleted: the core's data directory, which holds its key file on every
// desktop system, is renamed next to where it was, with the suffix
// "-unreadable-<UTC time>", and the next core starts empty with a new key.
//
// This module does not import Electron, so that it runs under plain Node in
// the tests.

import { existsSync, renameSync } from "node:fs";

/** 2026-10-13T21:05:09.123Z → 20261013T210509Z, as on Android (StartOver.kt). */
function stamp(now: Date): string {
  return `${now.toISOString().slice(0, 19).replaceAll("-", "").replaceAll(":", "")}Z`;
}

/**
 * Renames dataDir aside and returns its new path, or null if it does not
 * exist. Throws if the rename fails: the core must not start over the old
 * data.
 */
export function setAside(dataDir: string, now: Date = new Date()): string | null {
  if (!existsSync(dataDir)) {
    return null;
  }
  const base = `${dataDir}-unreadable-${stamp(now)}`;
  let target = base;
  for (let n = 2; existsSync(target); n++) {
    target = `${base}-${n}`;
  }
  renameSync(dataDir, target);
  return target;
}
