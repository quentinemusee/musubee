// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

// Runs the thread list benchmark (docs/ADR/0020) and prints one JSON line
// per run, then the medians. Build the page first (npm run bench:build).
//
//   node bench/run.mjs [--target electron|android] [--lists a,b] [--n 1000,5000]
//                      [--throttle 1,4] [--runs 3] [--check]
//
// --check: the performance regression test of the app's list (MessageList,
// list "app"): 5000 messages, CPU throttled 4 times, 3 runs; exits with 1
// when a median passes a limit of CHECKS.
//
// electron: the Electron of apps/desktop (cd apps/desktop && npm ci).
// android:  the debug app with the page in its assets (see ui/README.md),
//           installed on the device of ANDROID_SERIAL, adb in PATH.

import { createReadStream, existsSync } from "node:fs";
import { createServer } from "node:http";
import { createRequire } from "node:module";
import { dirname, extname, join, normalize, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { parseArgs } from "node:util";
import { _android, _electron } from "playwright-core";

const here = dirname(fileURLToPath(import.meta.url));
const dist = resolve(here, "../dist-bench");
const ALL_LISTS = ["plain", "content-visibility", "react-virtuoso", "tanstack-virtual", "virtua", "app"];

/**
 * The limits of --check. The DOM size and the position are exact; the
 * times leave room for slow CI machines (about 5 times the medians of a
 * desktop computer, docs/ADR/0020): they catch a list that stops
 * virtualizing or a change that makes each frame much slower, not noise.
 */
const CHECKS = {
  nodes: { max: 500, why: "the DOM holds only the rows near the viewport" },
  mountMs: { max: 2000, why: "time to show the newest message" },
  updateMs: { max: 150, why: "a status change" },
  appendMs: { max: 300, why: "a new message followed at the bottom" },
  scrollP95: { max: 250, why: "95th percentile of frame intervals in a fast fling" },
  prependMs: { max: 400, why: "200 older messages added above" },
  prependDrift: { max: 2, why: "the message being read stays in place (pixels)" },
  olderLoaded: { min: 1, why: "nearing the top reads older messages" },
  olderDrift: { max: 2, why: "the message being read stays in place when they come (pixels)" },
};

const { values } = parseArgs({
  options: {
    target: { type: "string", default: "electron" },
    lists: { type: "string", default: ALL_LISTS.join(",") },
    n: { type: "string", default: "1000,5000,20000" },
    throttle: { type: "string", default: "1,4" },
    runs: { type: "string", default: "3" },
    check: { type: "boolean", default: false },
  },
});
if (values.check) {
  Object.assign(values, { lists: "app", n: "5000", throttle: "4", runs: "3" });
}
const lists = values.lists.split(",");
const sizes = values.n.split(",").map(Number);
const throttles = values.throttle.split(",").map(Number);
const runs = Number(values.runs);

if (!existsSync(join(dist, "index.html"))) {
  throw new Error("build the page first: npm run bench:build");
}

/** Serves dist-bench on the loopback interface, for Electron. */
function serve() {
  const types = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css" };
  const server = createServer((req, res) => {
    const path = normalize(join(dist, decodeURIComponent(new URL(req.url, "http://x").pathname)));
    if (!path.startsWith(dist) || !existsSync(path) || path === dist) {
      res.writeHead(404).end();
      return;
    }
    res.writeHead(200, { "content-type": types[extname(path)] ?? "application/octet-stream" });
    createReadStream(path).pipe(res);
  });
  return new Promise((ok) => server.listen(0, "127.0.0.1", () => ok(server)));
}

async function openTarget() {
  if (values.target === "electron") {
    const server = await serve();
    const base = `http://127.0.0.1:${server.address().port}/index.html`;
    const desktop = createRequire(resolve(here, "../../apps/desktop/package.json"));
    const app = await _electron.launch({
      executablePath: desktop("electron"),
      args: [join(here, "electron-main.cjs")],
      env: { ...process.env, MUSUBEE_BENCH_URL: `${base}?list=plain&n=10` },
    });
    const page = await app.firstWindow();
    return { page, base, close: async () => { await app.close(); server.close(); } };
  }
  if (values.target === "android") {
    const [device] = await _android.devices();
    if (device === undefined) {
      throw new Error("no Android device: start an emulator or set ANDROID_SERIAL");
    }
    await device.shell("am start -W -n app.musubee/.MainActivity");
    const webview = await device.webView({ pkg: "app.musubee" });
    const page = await webview.page();
    return { page, base: "https://localhost/bench/index.html", close: () => device.close() };
  }
  throw new Error(`unknown target ${values.target}`);
}

const median = (xs) => {
  const s = xs.filter((x) => typeof x === "number").toSorted((a, b) => a - b);
  return s.length === 0 ? null : s[Math.floor(s.length / 2)];
};

const { page, base, close } = await openTarget();
const cdp = await page.context().newCDPSession(page);
const results = [];
try {
  for (const n of sizes) {
    for (const throttle of throttles) {
      for (const list of lists) {
        for (let run = 0; run < runs; run++) {
          // The app's list starts with older messages to read, as in the app.
          await page.goto(`${base}?list=${list}&n=${n}${list === "app" ? "&history=more" : ""}`);
          await page.waitForFunction(() => window.bench !== undefined);
          await cdp.send("HeapProfiler.collectGarbage");
          await cdp.send("Emulation.setCPUThrottlingRate", { rate: throttle });
          const r = { list, n, throttle, run };
          try {
            Object.assign(r, await page.evaluate(() => window.bench.mount()));
            Object.assign(r, await page.evaluate(() => window.bench.memory()));
            const update = await page.evaluate(() => window.bench.update());
            const append = await page.evaluate(() => window.bench.append());
            const jank = await page.evaluate(() => window.bench.scroll(120, 400, false));
            const blank = await page.evaluate(() => window.bench.scroll(60, 400, true));
            const prepend = await page.evaluate(() => window.bench.prepend(200));
            const merge = await page.evaluate(() => window.bench.storeMerge(20));
            // Only the app's list reads older messages by itself.
            const older = list === "app" ? await page.evaluate(() => window.bench.older()) : undefined;
            Object.assign(r, {
              updateMs: update.frameMs,
              appendMs: append.shownMs,
              followed: append.followed,
              scrollP50: jank.p50,
              scrollP95: jank.p95,
              scrollOver33: jank.over33,
              blank: blank.blank,
              prependMs: prepend.frameMs,
              prependDrift: prepend.drift,
              mergeMs: merge.mergeMs,
              olderLoaded: older?.loaded,
              olderDrift: older?.drift,
            });
          } catch (error) {
            r.error = String(error).split("\n")[0];
          } finally {
            await cdp.send("Emulation.setCPUThrottlingRate", { rate: 1 });
          }
          console.log(JSON.stringify(r));
          results.push(r);
        }
      }
    }
  }
} finally {
  await close();
}

// Medians per list, size and throttling.
const keys = ["mountMs", "nodes", "heapMiB", "updateMs", "appendMs", "scrollP50", "scrollP95", "scrollOver33", "blank", "prependMs", "prependDrift", "mergeMs", "olderLoaded", "olderDrift"];
const groups = Map.groupBy(results, (r) => `${r.n}\t${r.throttle}\t${r.list}`);
console.log(["n", "cpu", "list", ...keys, "followed", "bottom", "askedAtMount", "errors"].join("\t"));
for (const [key, rs] of groups) {
  const cells = keys.map((k) => {
    const m = median(rs.map((r) => r[k]));
    return m === null ? "-" : Number.isInteger(m) ? String(m) : m.toFixed(k === "blank" || k === "scrollOver33" ? 3 : 1);
  });
  const followed = rs.every((r) => r.followed) ? "yes" : "no";
  const bottom = rs.every((r) => r.bottomVisible) ? "yes" : "no";
  const asked = rs.some((r) => r.askedAtMount) ? "yes" : "no";
  console.log([key, ...cells, followed, bottom, asked, rs.filter((r) => r.error).length].join("\t"));
}

if (values.check) {
  const failures = results.filter((r) => r.error).map((r) => `run ${r.run}: ${r.error}`);
  if (!results.every((r) => r.bottomVisible && r.followed)) {
    failures.push("the newest message was not shown, or a new one was not followed");
  }
  if (results.some((r) => r.askedAtMount)) {
    failures.push("the list read older messages while opening at the newest one");
  }
  for (const [key, { min, max, why }] of Object.entries(CHECKS)) {
    const m = median(results.map((r) => r[key]));
    if (m === null || m > (max ?? Infinity) || m < (min ?? -Infinity)) {
      failures.push(`${key} = ${m}, expected ${max === undefined ? `>= ${min}` : `<= ${max}`}: ${why}`);
    }
  }
  if (failures.length > 0) {
    console.error(`thread list check failed:\n  ${failures.join("\n  ")}`);
    process.exit(1);
  }
  console.log("thread list check passed");
}
