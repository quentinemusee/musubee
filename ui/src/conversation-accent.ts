// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

// A conversation's or a person's own accent color (docs/ADR/0063), over
// the app's (ADR 0040): kept per device in the page's storage, by opaque
// IDs only, like the appearance.

import { ACCENTS, type Accent, type AppearanceStorage } from "./appearance";

/** The key of the page's storage that holds the accents. */
export const ACCENTS_KEY = "musubee.conversation-accents";

/** What a thread's accent belongs to: its person when it has one, else the conversation. */
export function accentKey(conversationId: string, personId: string | undefined): string {
  return personId === undefined ? `c.${conversationId}` : `p.${personId}`;
}

/** The accents from stored JSON: anything unknown or malformed is dropped. */
export function parseAccents(json: string | null): Record<string, Accent> {
  let value: unknown;
  try {
    value = json === null ? undefined : JSON.parse(json);
  } catch {
    value = undefined;
  }
  const accents: Record<string, Accent> = {};
  if (typeof value !== "object" || value === null) {
    return accents;
  }
  for (const [key, accent] of Object.entries(value)) {
    if (/^[cp]\./u.test(key) && ACCENTS.includes(accent as Accent)) {
      accents[key] = accent as Accent;
    }
  }
  return accents;
}

export function loadAccents(storage: AppearanceStorage | undefined): Record<string, Accent> {
  try {
    return parseAccents(storage?.getItem(ACCENTS_KEY) ?? null);
  } catch {
    return {};
  }
}

/** Keeps an accent, or forgets it (undefined: the app's); a storage that refuses keeps it for this run only. */
export function saveAccent(
  storage: AppearanceStorage | undefined,
  accents: Record<string, Accent>,
  key: string,
  accent: Accent | undefined,
): Record<string, Accent> {
  const { [key]: _, ...rest } = accents;
  const next = accent === undefined ? rest : { ...rest, [key]: accent };
  try {
    storage?.setItem(ACCENTS_KEY, JSON.stringify(next));
  } catch {
    // Applies until the app closes.
  }
  return next;
}
