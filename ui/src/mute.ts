// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

// Muting a conversation (API 1.23, docs/ADR/0050): its unread messages
// still count, but no notification announces them.

import type { Conversation } from "./core-api/types.gen";

/** muted_until_ms of a conversation muted until the user unmutes it. */
export const MUTED_FOREVER = -1;

const HOUR_MS = 3_600_000;

/** What the interface offers, in this order. */
export const MUTE_CHOICES: readonly { label: string; durationMs: number }[] = [
  { label: "For 1 hour", durationMs: HOUR_MS },
  { label: "For 8 hours", durationMs: 8 * HOUR_MS },
  { label: "For 1 week", durationMs: 7 * 24 * HOUR_MS },
  { label: "Until I unmute it", durationMs: MUTED_FOREVER },
];

/** The muted_until_ms of a choice made now. */
export function muteUntil(durationMs: number, now: number): number {
  return durationMs === MUTED_FOREVER ? MUTED_FOREVER : now + durationMs;
}

/** Whether a conversation is muted as of now: a mute that ended is none. */
export function isMuted(conversation: Pick<Conversation, "muted_until_ms">, now: number): boolean {
  const until = conversation.muted_until_ms ?? 0;
  return until === MUTED_FOREVER || until > now;
}

/**
 * What the thread says of a mute: "Muted", "Muted until 14:30" today,
 * "Muted until Mon 14:30" within a week, else with the date.
 */
export function mutedLabel(untilMs: number, now: number, locale?: string): string {
  if (untilMs === MUTED_FOREVER) {
    return "Muted";
  }
  const until = new Date(untilMs);
  const time = until.toLocaleTimeString(locale, {
    hour: "2-digit",
    minute: "2-digit",
  });
  if (until.toDateString() === new Date(now).toDateString()) {
    return `Muted until ${time}`;
  }
  if (untilMs - now < 6 * 24 * HOUR_MS) {
    return `Muted until ${until.toLocaleDateString(locale, { weekday: "short" })} ${time}`;
  }
  return `Muted until ${until.toLocaleDateString(locale, { day: "numeric", month: "short" })} ${time}`;
}
