// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

// The desktop app end to end (T1.5): the Electron app, its interface and
// the real core in its child process, driven by Playwright. Each test starts
// the app with a data directory of its own.
//
//   npm run build && npm run test:e2e

import { mkdtempSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join, resolve as resolvePath } from "node:path";
import { fileURLToPath } from "node:url";
import { _electron as electron, expect, test, type ElectronApplication, type Page } from "@playwright/test";
// Also brings window.musubee, as the interface sees it.
import type { CoreStatus } from "../../../ui/src/shell.ts";

// resolvePath() drops the trailing separator: on Windows, "dir\" on a command
// line escapes the closing quote, and Electron gets a path ending with '"'.
const appDir = resolvePath(fileURLToPath(new URL("..", import.meta.url)));

interface Running {
  app: ElectronApplication;
  page: Page;
}

let dataDirs: string[] = [];

test.afterEach(() => {
  for (const dir of dataDirs) {
    rmSync(dir, { recursive: true, force: true, maxRetries: 5 });
  }
  dataDirs = [];
});

function newDataDir(): string {
  const dir = mkdtempSync(join(tmpdir(), "musubee-desktop-e2e-"));
  dataDirs.push(dir);
  return dir;
}

async function launch(dataDir: string): Promise<Running> {
  const app = await electron.launch({
    args: [appDir],
    env: { ...process.env, MUSUBEE_USER_DATA_DIR: dataDir, MUSUBEE_E2E: "1" },
  });
  const page = await app.firstWindow();
  await expect(page.getByRole("status", { name: "App status" })).toHaveText(/Ready|Loading/);
  return { app, page };
}

function corePid(app: ElectronApplication): Promise<number | undefined> {
  return app.evaluate(() => (globalThis as unknown as { musubeeE2E: { corePid(): number | undefined } }).musubeeE2E.corePid());
}

function isRunning(pid: number): boolean {
  try {
    process.kill(pid, 0);
    return true;
  } catch {
    return false;
  }
}

/** Adds an account on the echo network and opens its Instant Echo conversation. */
async function addEchoAccount(page: Page, username: string): Promise<void> {
  await expect(page.getByRole("heading", { name: "Add an account" })).toBeVisible();
  await page.getByRole("button", { name: "Echo: Username" }).click();
  await page.getByLabel("Username").fill(username);
  await page.getByRole("button", { name: "Continue" }).click();
  await page.getByRole("button", { name: "Instant Echo" }).click();
  await expect(page.getByRole("heading", { name: "Instant Echo" })).toBeVisible();
}

async function sendAndGetEcho(page: Page, text: string): Promise<void> {
  await page.getByLabel("Message").fill(text);
  await page.getByRole("button", { name: "Send" }).click();
  const log = page.getByRole("log");
  await expect(log.locator('[data-from-me="true"]', { hasText: text })).toContainText("Sent");
  await expect(log.locator('[data-from-me="false"]', { hasText: text })).toContainText("Instant Echo");
}

test("open the app, send a message and get its echo, then find it again after a restart", async () => {
  const dataDir = newDataDir();
  let { app, page } = await launch(dataDir);
  await expect(page).toHaveTitle("Musubee");
  await addEchoAccount(page, "desktop");
  await expect(page.getByText("Connected")).toBeVisible();
  await sendAndGetEcho(page, "hello from Playwright");

  // Closing the app closes the core: no process is left behind.
  const pid = await corePid(app);
  expect(pid).toBeDefined();
  await app.close();
  expect(isRunning(pid!)).toBe(false);

  ({ app, page } = await launch(dataDir));
  await page.getByRole("button", { name: "Instant Echo" }).click();
  await expect(page.getByRole("log").locator(".message", { hasText: "hello from Playwright" })).toHaveCount(2);
  await app.close();
});

test("log out of an account: it goes away with its conversations", async () => {
  const { app, page } = await launch(newDataDir());
  await addEchoAccount(page, "leaving");
  await page.getByRole("button", { name: "Log out of Echo: leaving" }).click();
  // Nothing happens before the confirmation.
  await page.getByRole("button", { name: "Keep" }).click();
  await expect(page.getByRole("heading", { name: "Instant Echo" })).toBeVisible();
  await page.getByRole("button", { name: "Log out of Echo: leaving" }).click();
  await page.getByRole("button", { name: "Log out", exact: true }).click();
  await expect(page.getByRole("heading", { name: "Add an account" })).toBeVisible();
  expect(await page.evaluate(`window.musubee.core.call(JSON.stringify({ id: 1, command: "conversations.list" }))`)).toContain('"conversations":[]');
  await app.close();
});

test("the interface is isolated, and the main process checks what it sends", async () => {
  const { app, page } = await launch(newDataDir());

  // The renderer runs in Chromium's sandbox. Electron reports it on Windows
  // and macOS only.
  const sandboxed = await app.evaluate(({ app: electronApp }) => electronApp.getAppMetrics().filter((m) => m.type === "Tab").map((m) => m.sandboxed));
  expect(sandboxed.length).toBeGreaterThan(0);
  if (process.platform !== "linux") {
    expect(sandboxed.every((s) => s === true)).toBe(true);
  }
  expect(page.url()).toBe("app://musubee/index.html");

  // No Node.js in the page. window.musubee exists only with context
  // isolation: contextBridge refuses to work without it.
  const globals = await page.evaluate(() => {
    const w = window as unknown as Record<string, unknown>;
    return {
      require: typeof w["require"],
      process: typeof w["process"],
      module: typeof w["module"],
      shell: Object.keys(window.musubee ?? {}),
      core: Object.keys(window.musubee?.core ?? {}).toSorted(),
    };
  });
  expect(globals).toEqual({ require: "undefined", process: "undefined", module: "undefined", shell: ["core"], core: ["call", "onEvent", "onStatus"] });

  // Requests the schema refuses get the core API's own errors.
  const codes = await page.evaluate(async () => {
    // Functions in page.evaluate run in the page: they cannot be moved out.
    // oxlint-disable-next-line unicorn/consistent-function-scoping
    const call = async (request: string) => (JSON.parse(await window.musubee!.core.call(request)) as { error?: { code: string } }).error?.code;
    return {
      notJson: await call("not json"),
      extraField: await call(JSON.stringify({ id: 1, command: "core.hello", extra: true })),
      zeroId: await call(JSON.stringify({ id: 0, command: "core.hello" })),
      unknownCommand: await call(JSON.stringify({ id: 2, command: "core.shutdown" })),
      badParams: await call(JSON.stringify({ id: 3, command: "messages.send", params: { conversation_id: 5, text: "x" } })),
      missingParams: await call(JSON.stringify({ id: 4, command: "messages.send" })),
      valid: await call(JSON.stringify({ id: 5, command: "core.hello" })),
    };
  });
  expect(codes).toEqual({
    notJson: "invalid_request",
    extraField: "invalid_request",
    zeroId: "invalid_request",
    unknownCommand: "unknown_command",
    badParams: "invalid_params",
    missingParams: "invalid_params",
    valid: undefined,
  });

  // Payloads that no interface sends are refused outright.
  const refused = await page.evaluate(async () => {
    // oxlint-disable-next-line unicorn/consistent-function-scoping
    const outcome = (p: Promise<unknown>) => p.then(() => "accepted", (e: unknown) => String(e));
    const call = window.musubee!.core.call as (request: unknown) => Promise<string>;
    return {
      number: await outcome(call(42)),
      huge: await outcome(call(JSON.stringify({ id: 1, command: "debug.ping", params: { payload: "x".repeat(2 << 20) } }))),
    };
  });
  expect(refused.number).toContain("must be a string");
  expect(refused.huge).toContain("at most");

  // No navigation, no new window, no inline script.
  const blocked = await page.evaluate(async () => {
    const opened = window.open("https://example.com/");
    const script = document.createElement("script");
    script.textContent = "window.inlineRan = true";
    document.body.append(script);
    location.href = "https://example.com/";
    await new Promise((resolve) => setTimeout(resolve, 500));
    return { opened: opened === null, inlineRan: (window as unknown as { inlineRan?: boolean }).inlineRan === true };
  });
  expect(blocked).toEqual({ opened: true, inlineRan: false });
  expect(page.url()).toBe("app://musubee/index.html");
  await app.close();
});

test("the app starts the core again when it stops unexpectedly", async () => {
  const { app, page } = await launch(newDataDir());
  await addEchoAccount(page, "crash");
  await page.evaluate(() => {
    const seen: CoreStatus["state"][] = [];
    (window as unknown as { statuses: string[] }).statuses = seen;
    window.musubee!.core.onStatus((status) => seen.push(status.state));
  });

  const pid = await corePid(app);
  process.kill(pid!);
  await expect.poll(async () => (await corePid(app)) ?? pid).not.toBe(pid);
  await expect(page.getByRole("status", { name: "App status" })).toHaveText("Ready");
  expect(await page.evaluate(() => (window as unknown as { statuses: string[] }).statuses)).toEqual(["ready", "restarting", "ready"]);

  // The interface read its state again, and works with the new core.
  await page.getByRole("button", { name: "Instant Echo" }).click();
  await sendAndGetEcho(page, "after the restart");
  await app.close();
});

interface Measures {
  pingUs: number;
  roundTripMeanMs: number;
  roundTripP50Ms: number;
  roundTripMaxMs: number;
}

type Call = (request: { id: number; command: string; params?: object }) => Promise<string>;
type OnEvent = (listener: (event: string) => void) => () => void;

/**
 * Measures the core through call and onEvent: 2,000 pings with a 1 KiB
 * payload, then 50 round trips to the Instant Echo conversation (a message
 * sent, until its echo arrives). Playwright serialises the function to run it
 * in the page or in the main process: it must not use anything from this
 * module.
 */
async function measure(call: Call, onEvent: OnEvent, conversationId: string): Promise<Measures> {
  let id = 0;
  const payload = { payload: "x".repeat(1024) };
  for (let i = 0; i < 200; i++) {
    await call({ id: ++id, command: "debug.ping", params: payload });
  }
  const pings = 2000;
  let start = performance.now();
  for (let i = 0; i < pings; i++) {
    await call({ id: ++id, command: "debug.ping", params: payload });
  }
  const pingUs = ((performance.now() - start) / pings) * 1000;

  const times: number[] = [];
  for (let i = 0; i < 60; i++) {
    const text = `round trip ${i} ${Math.random()}`;
    const received = new Promise<void>((done) => {
      const stop = onEvent((json) => {
        const event = JSON.parse(json) as { type: string; data: { message?: { from_me: boolean; text: string } } };
        if (event.type === "message.added" && event.data.message?.from_me === false && event.data.message.text === text) {
          stop();
          done();
        }
      });
    });
    start = performance.now();
    await call({ id: ++id, command: "messages.send", params: { conversation_id: conversationId, text } });
    await received;
    if (i >= 10) {
      times.push(performance.now() - start);
    }
  }
  const sorted = times.toSorted((a, b) => a - b);
  return {
    pingUs,
    roundTripMeanMs: sorted.reduce((a, b) => a + b, 0) / sorted.length,
    roundTripP50Ms: sorted[Math.floor(sorted.length / 2)]!,
    roundTripMaxMs: sorted.at(-1)!,
  };
}

// Playwright needs the object pattern to pass testInfo second.
// oxlint-disable-next-line eslint/no-empty-pattern
test("measure the start-up of the core and the cost of a call through the app", async ({}, testInfo) => {
  const { app, page } = await launch(newDataDir());
  await addEchoAccount(page, "bench");
  const conversationId = await page.evaluate(async () => {
    const response = JSON.parse(await window.musubee!.core.call(JSON.stringify({ id: 1, command: "conversations.list" }))) as {
      result: { conversations: { conversation_id: string; name: string }[] };
    };
    return response.result.conversations.find((c) => c.name === "Instant Echo")!.conversation_id;
  });

  // From the renderer: the interface's path (preload, IPC, validation, main
  // process, core), while the interface shows the conversation.
  const source = measure.toString();
  // An expression rather than new Function: the page's policy forbids eval,
  // and expressions Playwright evaluates are not subject to it.
  const fromRenderer = `(${source})((request) => window.musubee.core.call(JSON.stringify(request)), (listener) => window.musubee.core.onEvent(listener), ${JSON.stringify(conversationId)})`;
  const renderer = (await page.evaluate(fromRenderer)) as Measures;
  // The same path with the conversation off screen: the store still takes
  // every event, but React no longer renders the thread.
  await page.getByRole("button", { name: "Add an account" }).click();
  await expect(page.getByRole("heading", { name: "Instant Echo" })).toBeHidden();
  const rendererThreadHidden = (await page.evaluate(fromRenderer)) as Measures;
  // From the main process: the supervisor and the core only.
  const main = await app.evaluate(
    async (_, [fn, id]) => {
      const hook = (globalThis as unknown as { musubeeE2E: { call: Call; onEvent: OnEvent } }).musubeeE2E;
      const run = new Function(`return (${fn})`)() as typeof measure;
      return run(hook.call, hook.onEvent, id);
    },
    [source, conversationId] as const,
  );
  await app.close();

  // Three more cold starts of the app on the same data directory.
  const startups: number[] = [];
  const dataDir = newDataDir();
  for (let i = 0; i < 4; i++) {
    const run = await launch(dataDir);
    await expect(run.page.getByRole("status", { name: "App status" })).toHaveText(/Ready/);
    startups.push(...(await run.app.evaluate(() => (globalThis as unknown as { musubeeE2E: { startupTimesMs(): number[] } }).musubeeE2E.startupTimesMs())));
    await run.app.close();
  }
  const result = { platform: process.platform, arch: process.arch, renderer, rendererThreadHidden, main, coreStartupMs: startups };
  console.log(JSON.stringify(result));
  writeFileSync(testInfo.outputPath("measurements.json"), JSON.stringify(result, null, 2));
  await testInfo.attach("measurements", { body: JSON.stringify(result, null, 2), contentType: "application/json" });
  // Loose bounds: they catch a broken transport, not a slow machine.
  expect(renderer.pingUs).toBeLessThan(10_000);
  expect(renderer.roundTripP50Ms).toBeLessThan(1_000);
});
