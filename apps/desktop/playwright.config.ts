// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

// End-to-end tests of the desktop app (e2e/): Playwright drives the real
// Electron app, which runs the real core. Build it first: npm run build.

import { defineConfig } from "@playwright/test";

export default defineConfig({
  testDir: "e2e",
  timeout: 120_000,
  expect: { timeout: 20_000 },
  // One app at a time: each test starts its own, with its own data directory.
  workers: 1,
  // A flaky test must be fixed, not retried.
  retries: 0,
  forbidOnly: !!process.env["CI"],
  reporter: [["list"]],
  use: { trace: "retain-on-failure" },
});
