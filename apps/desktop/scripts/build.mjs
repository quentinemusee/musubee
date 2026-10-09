// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

// Builds the desktop app in place (docs/ADR/0013-desktop-shell.md):
//
//   resources/core/musubee-core[.exe]   the core (core/cmd/musubee-core)
//   resources/ui/                        the interface (ui/, built by Vite)
//   resources/core-api.schema.json       the schema the main process validates against
//   dist/                                the main process and the preload (tsc)
//
// Needs Go and the interface's dependencies (cd ui && npm ci). The core is
// built without cgo: the desktop needs no C toolchain (its SQLite is pure Go,
// docs/ADR/0009).
//
//   npm run build

import { execFileSync } from "node:child_process";
import { cpSync, mkdirSync, rmSync } from "node:fs";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const desktop = resolve(dirname(fileURLToPath(import.meta.url)), "..");
const root = resolve(desktop, "../..");
const resources = join(desktop, "resources");
const exe = process.platform === "win32" ? ".exe" : "";
const npm = process.platform === "win32" ? "npm.cmd" : "npm";

function run(command, args, cwd, env = {}) {
  console.log(`> ${[command, ...args].join(" ")}`);
  execFileSync(command, args, { cwd, stdio: "inherit", env: { ...process.env, ...env }, shell: process.platform === "win32" && command === npm });
}

rmSync(resources, { recursive: true, force: true });
rmSync(join(desktop, "dist"), { recursive: true, force: true });
mkdirSync(join(resources, "core"), { recursive: true });

run("go", ["build", "-trimpath", "-o", join(resources, "core", `musubee-core${exe}`), "./core/cmd/musubee-core"], root, { CGO_ENABLED: "0" });
run(npm, ["run", "build"], join(root, "ui"));
cpSync(join(root, "ui", "dist"), join(resources, "ui"), { recursive: true });
cpSync(join(root, "core", "api", "schema", "core-api.schema.json"), join(resources, "core-api.schema.json"));
run(process.execPath, [join(desktop, "node_modules", "typescript", "bin", "tsc"), "-p", "tsconfig.json"], desktop);
