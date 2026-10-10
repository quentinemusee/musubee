// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

// Builds core/cmd/musubee-core into a temporary directory: the supervisor
// tests run the real core, not a stand-in.

import { execFileSync } from "node:child_process";
import { mkdtempSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import type { TestProject } from "vitest/node";

declare module "vitest" {
  export interface ProvidedContext {
    coreExecutable: string;
  }
}

export default function setup(project: TestProject): () => void {
  const dir = mkdtempSync(join(tmpdir(), "musubee-desktop-test-"));
  const executable = join(dir, process.platform === "win32" ? "musubee-core.exe" : "musubee-core");
  execFileSync("go", ["build", "-tags=goolm", "-o", executable, "./core/cmd/musubee-core"], {
    cwd: resolve(fileURLToPath(new URL("../../..", import.meta.url))),
    env: { ...process.env, CGO_ENABLED: "0" },
    stdio: "inherit",
  });
  project.provide("coreExecutable", executable);
  return () => rmSync(dir, { recursive: true, force: true, maxRetries: 5 });
}
