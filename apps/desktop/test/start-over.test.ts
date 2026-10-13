// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

// Setting the core's unreadable data aside (start-over.ts).

import { existsSync, mkdirSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { afterEach, expect, test } from "vitest";
import { setAside } from "../src/start-over.ts";

const dirs: string[] = [];

afterEach(() => {
  for (const dir of dirs.splice(0)) {
    rmSync(dir, { recursive: true, force: true, maxRetries: 5 });
  }
});

test("renames the data directory aside with the time, and deletes nothing", () => {
  const parent = mkdtempSync(join(tmpdir(), "musubee-start-over-"));
  dirs.push(parent);
  const dataDir = join(parent, "core");
  const now = new Date("2026-10-13T21:05:09.123Z");
  for (const expected of ["core-unreadable-20261013T210509Z", "core-unreadable-20261013T210509Z-2"]) {
    mkdirSync(dataDir);
    writeFileSync(join(dataDir, "core.key"), expected);
    expect(setAside(dataDir, now)).toBe(join(parent, expected));
    expect(existsSync(dataDir)).toBe(false);
    expect(readFileSync(join(parent, expected, "core.key"), "utf8")).toBe(expected);
  }
});

test("does nothing without a data directory", () => {
  const parent = mkdtempSync(join(tmpdir(), "musubee-start-over-"));
  dirs.push(parent);
  expect(setAside(join(parent, "core"))).toBeNull();
});
