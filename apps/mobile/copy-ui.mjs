// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

// Puts the interface into www/, Capacitor's webDir, before `cap sync`: the
// build of ui/ (cd ../../ui && npm run build) and the pages of web/, which
// only the app needs. www/ is generated: Git ignores it.
//
//   npm run web

import { cpSync, existsSync, rmSync } from "node:fs";
import { fileURLToPath } from "node:url";

const here = (path) => fileURLToPath(new URL(path, import.meta.url));
const dist = here("../../ui/dist/");
const www = here("www/");

if (!existsSync(dist)) {
  console.error(`${dist} not found: build the interface first (cd ui && npm ci && npm run build).`);
  process.exit(1);
}
rmSync(www, { recursive: true, force: true });
cpSync(dist, www, { recursive: true });
cpSync(here("web/"), www, { recursive: true });
console.log(`Copied the interface into ${www}`);
