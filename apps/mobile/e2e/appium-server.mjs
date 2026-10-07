// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

// Starts the Appium server installed in this project (with the drivers listed
// in package.json), waits until it answers, and stops it afterwards. Tests
// can use an already running server instead by setting MUSUBEE_APPIUM_URL.

import { spawn } from "node:child_process";
import { readFileSync } from "node:fs";
import { createRequire } from "node:module";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

const require = createRequire(import.meta.url);

/** Resolves the path of the appium executable script of this project. */
function appiumMain() {
  const pkgPath = require.resolve("appium/package.json");
  const pkg = JSON.parse(readFileSync(pkgPath, "utf8"));
  const bin = typeof pkg.bin === "string" ? pkg.bin : pkg.bin.appium;
  return join(dirname(pkgPath), bin);
}

async function waitForStatus(url, timeoutMs) {
  const deadline = Date.now() + timeoutMs;
  let lastError;
  while (Date.now() < deadline) {
    try {
      const res = await fetch(`${url}/status`);
      if (res.ok) {
        return;
      }
      lastError = new Error(`HTTP ${res.status}`);
    } catch (err) {
      lastError = err;
    }
    await new Promise((resolve) => setTimeout(resolve, 500));
  }
  throw new Error(`Appium did not answer at ${url} within ${timeoutMs} ms: ${lastError}`);
}

/**
 * Returns { url, stop }. Uses MUSUBEE_APPIUM_URL when set; otherwise starts a
 * local server on a free-ish port and returns a function that stops it.
 */
export async function startAppium() {
  if (process.env.MUSUBEE_APPIUM_URL) {
    return { url: process.env.MUSUBEE_APPIUM_URL, stop: async () => {} };
  }
  const port = 4723 + Math.floor(Math.random() * 1000);
  const url = `http://127.0.0.1:${port}`;
  const logs = [];
  const child = spawn(process.execPath, [appiumMain(), "--address", "127.0.0.1", "--port", String(port), "--log-level", "warn"], {
    cwd: dirname(fileURLToPath(import.meta.url)),
    stdio: ["ignore", "pipe", "pipe"],
  });
  for (const stream of [child.stdout, child.stderr]) {
    stream.on("data", (chunk) => logs.push(chunk.toString()));
  }
  try {
    await waitForStatus(url, 60_000);
  } catch (err) {
    child.kill();
    throw new Error(`${err.message}\n--- Appium output ---\n${logs.join("")}`);
  }
  return {
    url,
    logs: () => logs.join(""),
    stop: async () => {
      child.kill();
      await new Promise((resolve) => child.once("exit", resolve));
    },
  };
}

/** Splits an http URL into the options webdriverio's remote() expects. */
export function remoteOptions(url) {
  const u = new URL(url);
  return {
    protocol: u.protocol.replace(":", ""),
    hostname: u.hostname,
    port: Number(u.port),
    path: u.pathname === "" ? "/" : u.pathname,
  };
}
