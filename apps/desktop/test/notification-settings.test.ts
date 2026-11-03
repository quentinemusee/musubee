// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

import { mkdtempSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { expect, test } from "vitest";
import { loadPreview, savePreview } from "../src/notification-settings.ts";

test("the preview is kept in the data directory, and defaults to everything", () => {
  const dir = mkdtempSync(join(tmpdir(), "musubee-notifications-"));
  expect(loadPreview(dir)).toBe("name_and_text");
  savePreview(dir, "nothing");
  expect(loadPreview(dir)).toBe("nothing");
  writeFileSync(join(dir, "notifications.json"), '{"preview":"everything"}');
  expect(loadPreview(dir)).toBe("name_and_text");
  writeFileSync(join(dir, "notifications.json"), "not json");
  expect(loadPreview(dir)).toBe("name_and_text");
});
