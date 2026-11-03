// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

import { mkdtempSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { expect, test } from "vitest";
import { loadSettings, paused, saveSettings } from "../src/notification-settings.ts";

test("the settings are kept in the data directory, each with its default", () => {
  const dir = mkdtempSync(join(tmpdir(), "musubee-notifications-"));
  expect(loadSettings(dir)).toEqual({ preview: "name_and_text", pausedUntil: 0 });
  saveSettings(dir, { preview: "nothing", pausedUntil: 1_800_000_000_000 });
  expect(loadSettings(dir)).toEqual({ preview: "nothing", pausedUntil: 1_800_000_000_000 });
  writeFileSync(join(dir, "notifications.json"), '{"preview":"everything","paused_until":"soon"}');
  expect(loadSettings(dir)).toEqual({ preview: "name_and_text", pausedUntil: 0 });
  writeFileSync(join(dir, "notifications.json"), "not json");
  expect(loadSettings(dir)).toEqual({ preview: "name_and_text", pausedUntil: 0 });
});

test("a pause ends by itself (ADR 0067)", () => {
  expect(paused({ preview: "name_and_text", pausedUntil: 2000 }, 1000)).toBe(true);
  expect(paused({ preview: "name_and_text", pausedUntil: 2000 }, 2000)).toBe(false);
  expect(paused({ preview: "name_and_text", pausedUntil: 0 }, 1000)).toBe(false);
});
