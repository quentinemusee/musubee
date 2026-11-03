// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

import { expect, test } from "vitest";
import { NOTIFICATION_PREVIEW_NAMES, NOTIFICATION_PREVIEWS, parseNotificationPreview, pauseChoices } from "./notification-preview";

test("a stored preview is one of the choices, or the default", () => {
  for (const value of NOTIFICATION_PREVIEWS) {
    expect(parseNotificationPreview(value)).toBe(value);
    expect(NOTIFICATION_PREVIEW_NAMES[value]).not.toBe("");
  }
  expect(parseNotificationPreview("everything")).toBe("name_and_text");
  expect(parseNotificationPreview(undefined)).toBe("name_and_text");
  expect(parseNotificationPreview(3)).toBe("name_and_text");
});

test("pauses last an hour, eight hours, or until 8:00 tomorrow", () => {
  const now = new Date(2026, 10, 3, 22, 30).getTime();
  const [hour, eight, morning] = pauseChoices(now);
  expect(hour!.until - now).toBe(3_600_000);
  expect(eight!.until - now).toBe(8 * 3_600_000);
  expect(new Date(morning!.until)).toEqual(new Date(2026, 10, 4, 8, 0));
});
