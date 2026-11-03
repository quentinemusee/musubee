// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

import { expect, test } from "vitest";
import { NOTIFICATION_PREVIEW_NAMES, NOTIFICATION_PREVIEWS, parseNotificationPreview } from "./notification-preview";

test("a stored preview is one of the choices, or the default", () => {
  for (const value of NOTIFICATION_PREVIEWS) {
    expect(parseNotificationPreview(value)).toBe(value);
    expect(NOTIFICATION_PREVIEW_NAMES[value]).not.toBe("");
  }
  expect(parseNotificationPreview("everything")).toBe("name_and_text");
  expect(parseNotificationPreview(undefined)).toBe("name_and_text");
  expect(parseNotificationPreview(3)).toBe("name_and_text");
});
