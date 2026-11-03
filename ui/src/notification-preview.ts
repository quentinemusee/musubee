// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

// What notifications show of a message (docs/ADR/0051). The shells keep
// the choice and apply it: the interface only offers it.

import type { NotificationPreview } from "./shell";

export const NOTIFICATION_PREVIEWS: readonly NotificationPreview[] = ["name_and_text", "name_only", "nothing"];

export const NOTIFICATION_PREVIEW_NAMES: Record<NotificationPreview, string> = {
  name_and_text: "Name and message",
  name_only: "Name only",
  nothing: "No name or message",
};

/** A value from a shell's storage: anything else is the default, which shows everything. */
export function parseNotificationPreview(value: unknown): NotificationPreview {
  return NOTIFICATION_PREVIEWS.includes(value as NotificationPreview) ? (value as NotificationPreview) : "name_and_text";
}

const HOUR_MS = 3_600_000;

/**
 * The pauses offered (docs/ADR/0067): an hour, eight hours, or until 8:00
 * the next morning, in the device's time.
 */
export function pauseChoices(now: number): { label: string; until: number }[] {
  const morning = new Date(now);
  morning.setDate(morning.getDate() + 1);
  morning.setHours(8, 0, 0, 0);
  return [
    { label: "For 1 hour", until: now + HOUR_MS },
    { label: "For 8 hours", until: now + 8 * HOUR_MS },
    { label: "Until tomorrow morning", until: morning.getTime() },
  ];
}
