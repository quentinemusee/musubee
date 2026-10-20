// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

// The desktop app end to end (T1.5): the Electron app, its interface and
// the real core in its child process, driven by Playwright. Each test starts
// the app with a data directory of its own.
//
//   npm run build && npm run test:e2e

import { randomBytes } from "node:crypto";
import { existsSync, mkdtempSync, readdirSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join, resolve as resolvePath } from "node:path";
import { fileURLToPath } from "node:url";
import { _electron as electron, expect, test, type ElectronApplication, type Page } from "@playwright/test";
// jsQR is CommonJS: under NodeNext its function is the export named default.
import jsqr from "jsqr";
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

async function launch(dataDir: string, env: Record<string, string> = {}, status: RegExp = /Ready|Loading/): Promise<Running> {
  const app = await electron.launch({
    args: [appDir],
    env: { ...process.env, ...env, MUSUBEE_USER_DATA_DIR: dataDir, MUSUBEE_E2E: "1" },
  });
  const page = await app.firstWindow();
  await expect(page.getByRole("status", { name: "App status" })).toHaveText(status);
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
  await loginEcho(page, username);
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

test("a network's reactions, edits, deletions and replies change the messages they are about", async () => {
  const dataDir = newDataDir();
  let { app, page } = await launch(dataDir);
  await addEchoAccount(page, "changes");
  let log = page.getByRole("log");
  const mine = (text: string) => log.locator('.message[data-from-me="true"]', { hasText: text });
  const theirs = (text: string) => log.locator('.message[data-from-me="false"]', { hasText: text });
  // The echo network's commands make Instant Echo act on messages.
  const send = async (text: string) => {
    await page.getByLabel("Message").fill(text);
    await page.getByRole("button", { name: "Send" }).click();
    await expect(mine(text)).toContainText("Sent");
  };

  await send("/react 🎉");
  await expect(mine("/react 🎉").getByRole("listitem", { name: "🎉: 1 person" })).toBeVisible();

  await send("/edit first try");
  await expect(theirs("first try (edited)")).toContainText("Edited");
  // The edit changed the echo: no second message for it.
  await expect(theirs("first try")).toHaveCount(1);

  await send("/delete a secret");
  await expect(log.locator('.message[data-deleted="true"]')).toHaveText(/Message deleted/);
  await expect(theirs("a secret")).toHaveCount(0);

  await send("/reply an answer");
  const quote = theirs("an answer").locator("blockquote");
  await expect(quote).toContainText("You");
  await expect(quote).toContainText("/reply an answer");

  // Read again from the core after a restart: the same changes.
  await app.close();
  ({ app, page } = await launch(dataDir));
  await page.getByRole("button", { name: "Instant Echo" }).click();
  log = page.getByRole("log");
  await expect(mine("/react 🎉").getByRole("listitem", { name: "🎉: 1 person" })).toBeVisible();
  await expect(theirs("first try (edited)")).toContainText("Edited");
  await expect(log.locator('.message[data-deleted="true"]')).toHaveCount(1);
  await expect(theirs("an answer").locator("blockquote")).toContainText("/reply an answer");
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

test("a QR code login shows a code that a phone can scan, then adds the account", async () => {
  const { app, page } = await launch(newDataDir());
  await expect(page.getByRole("heading", { name: "Add an account" })).toBeVisible();
  await page.getByRole("button", { name: "Echo: QR code" }).click();
  // The echo network confirms the code by itself after two seconds: read
  // the image's source and the text at once, and decode the image after.
  const qr = page.getByRole("img", { name: "QR code" });
  await expect(qr).toBeVisible();
  const [src, text] = await Promise.all([qr.getAttribute("src"), page.locator(".login-link").textContent()]);
  expect(src).toMatch(/^data:image\/svg\+xml,/);
  expect(text).toMatch(/^musubee-echo:[0-9a-f]{6}$/);

  // Draw the image as the screen would, then decode it as a phone would.
  const { pixels, width } = await page.evaluate(async (source) => {
    const image = new Image();
    image.src = source;
    await image.decode();
    const canvas = document.createElement("canvas");
    canvas.width = canvas.height = 400;
    const context = canvas.getContext("2d")!;
    context.drawImage(image, 0, 0, 400, 400);
    return { pixels: Array.from(context.getImageData(0, 0, 400, 400).data), width: 400 };
  }, src!);
  expect(jsqr.default(Uint8ClampedArray.from(pixels), width, width)?.data).toBe(text);

  await expect(page.getByText(`Echo: qr${text!.slice("musubee-echo:".length)}`)).toBeVisible();
  await expect(page.getByRole("heading", { name: "Add an account" })).toBeHidden();
  await app.close();
});

test("a first launch says what Musubee is and what each network brings, and keeps rare ways to log in apart", async () => {
  // Fake application credentials: the app offers Telegram, which nothing
  // contacts before a login starts.
  const { app, page } = await launch(newDataDir(), { MUSUBEE_TG_API_ID: "1", MUSUBEE_TG_API_HASH: "0123456789abcdef0123456789abcdef" });
  await expect(page.getByRole("heading", { name: "Welcome to Musubee" })).toBeVisible();
  await expect(page.getByText("your messages do not go through a Musubee server")).toBeVisible();
  const telegram = page.getByRole("listitem").filter({ has: page.getByRole("heading", { name: "Telegram" }) });
  await expect(telegram.getByText("Your Telegram chats, groups and channels.")).toBeVisible();
  await expect(telegram.getByRole("button", { name: "Telegram: With your phone number" })).toHaveAccessibleDescription(
    "Telegram sends you a code to type here.",
  );
  const bot = telegram.getByRole("button", { name: "Telegram: As a bot" });
  await expect(bot).toBeHidden();
  await telegram.getByText("Other ways to log in").click();
  await expect(bot).toBeVisible();

  // With an account, adding another one goes straight to the networks.
  await loginEcho(page, "first");
  await page.getByRole("button", { name: "Add an account" }).click();
  await expect(page.getByRole("heading", { name: "Add an account" })).toBeVisible();
  await expect(page.getByRole("heading", { name: "Welcome to Musubee" })).toBeHidden();
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
  expect(globals).toEqual({ require: "undefined", process: "undefined", module: "undefined", shell: ["core"], core: ["call", "onEvent", "onStatus", "startOver"] });

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

/**
 * The inbox's rows, in order: each one's button is named by the entry's name.
 * The list is virtualized: only the rows near the viewport are there, each
 * with its aria-posinset and the list's aria-setsize.
 */
function inboxRows(page: Page) {
  return page.getByRole("region", { name: "Conversations" }).getByRole("listitem");
}

/** Logs in on the echo network, without opening a conversation. */
async function loginEcho(page: Page, username: string): Promise<void> {
  await expect(page.getByRole("heading", { name: "Add an account" })).toBeVisible();
  await page.getByRole("button", { name: "Echo: Username" }).click();
  await page.getByLabel("Username", { exact: true }).fill(username);
  await page.getByRole("button", { name: "Continue" }).click();
  await expect(page.getByRole("heading", { name: "Add an account" })).toBeHidden();
}

/** Calls the core as the interface does, and returns the result. */
async function coreCall<T>(page: Page, command: string, params?: object): Promise<T> {
  const response = JSON.parse(
    (await page.evaluate(
      ([name, args]) => window.musubee!.core.call(JSON.stringify({ id: 1, command: name, ...(args === undefined ? {} : { params: args }) })),
      [command, params] as const,
    )) as string,
  ) as { result?: T; error?: { message: string } };
  if (response.result === undefined) {
    throw new Error(`${command}: ${response.error?.message}`);
  }
  return response.result;
}

test("the inbox shows the most recent conversation first, with its last message and unread count", async () => {
  const { app, page } = await launch(newDataDir());
  await addEchoAccount(page, "inbox");
  await sendAndGetEcho(page, "first in the inbox");
  const first = inboxRows(page).first().getByRole("button");
  await expect(first).toHaveAccessibleName("Instant Echo");
  await expect(first).toHaveAccessibleDescription(/Echo.*first in the inbox/);

  // In the group, Instant Echo answers: its name comes before the message.
  await page.getByRole("button", { name: "Echo Group" }).click();
  await expect(page.getByRole("heading", { name: "Echo Group" })).toBeVisible();
  await page.getByLabel("Message").fill("hello group");
  await page.getByRole("button", { name: "Send" }).click();
  await expect(page.getByRole("log").locator('[data-from-me="false"]', { hasText: "hello group" })).toBeVisible();
  await expect(first).toHaveAccessibleName("Echo Group");
  await expect(first).toHaveAccessibleDescription(/Group.*Instant Echo: hello group/);
  await expect(first).toHaveAttribute("aria-current", "true");

  // An answer in a conversation that is not open is unread until opened.
  type Listed = { conversations: { conversation_id: string; name: string; unread_count?: number }[] };
  const { conversations } = await coreCall<Listed>(page, "conversations.list");
  const instant = conversations.find((c) => c.name === "Instant Echo")!;
  expect(instant.unread_count).toBeUndefined();
  await coreCall(page, "messages.send", { conversation_id: instant.conversation_id, text: "read me later" });
  await expect(first).toHaveAccessibleName("Instant Echo");
  await expect(first).toHaveAccessibleDescription(/^1 unread message Echo.*read me later/);
  await first.click();
  await expect(page.getByRole("log").locator('[data-from-me="false"]', { hasText: "read me later" })).toBeVisible();
  await expect(first).toHaveAccessibleDescription(/^Echo.*read me later/);
  await expect
    .poll(async () => (await coreCall<Listed>(page, "conversations.list")).conversations.find((c) => c.name === "Instant Echo")?.unread_count)
    .toBeUndefined();
  await app.close();
});

test("conversations merged into a person make one entry, and one thread with a switcher", async () => {
  const { app, page } = await launch(newDataDir());
  await loginEcho(page, "alpha");
  await page.getByRole("button", { name: "Add an account" }).click();
  await loginEcho(page, "beta");
  type Listed = { conversations: { conversation_id: string; name: string }[] };
  let instant: string[] = [];
  await expect
    .poll(async () => {
      const { conversations } = await coreCall<Listed>(page, "conversations.list");
      instant = conversations.filter((c) => c.name === "Instant Echo").map((c) => c.conversation_id);
      return conversations.length;
    })
    .toBe(8);
  await expect(inboxRows(page).first()).toHaveAttribute("aria-setsize", "8");
  // instant[1] is alpha's: the core lists the most recent first.
  await coreCall(page, "persons.create", { name: "Echo Friend", conversation_ids: instant });
  // A message in alpha's conversation brings the person first.
  await coreCall(page, "messages.send", { conversation_id: instant[1], text: "to the person" });

  const first = inboxRows(page).first();
  await expect(first).toHaveAttribute("aria-setsize", "7");
  await expect(first.getByRole("button")).toHaveAccessibleName("Echo Friend");
  await expect(first.getByRole("button")).toHaveAccessibleDescription(/Echo.*to the person/);
  await first.getByRole("button").click();
  await expect(page.getByRole("heading", { name: "Echo Friend" })).toBeVisible();
  const switcher = page.getByRole("group", { name: "Conversations with Echo Friend" });
  // The most recent conversation is open; the other is one click away.
  await expect(switcher.getByRole("button")).toHaveText(["Echo: alpha", "Echo: beta"]);
  await expect(switcher.getByRole("button", { name: "Echo: alpha" })).toHaveAttribute("aria-pressed", "true");
  await switcher.getByRole("button", { name: "Echo: beta" }).click();
  await expect(switcher.getByRole("button", { name: "Echo: beta" })).toHaveAttribute("aria-pressed", "true");
  await sendAndGetEcho(page, "through beta");
  // That conversation is now the person's most recent: still one entry.
  await expect(switcher.getByRole("button")).toHaveText(["Echo: beta", "Echo: alpha"]);
  await expect(first.getByRole("button")).toHaveAccessibleName("Echo Friend");
  await expect(first.getByRole("button")).toHaveAccessibleDescription(/through beta/);
  await expect(first).toHaveAttribute("aria-setsize", "7");
  await app.close();
});

test("merge two conversations from the thread, rename the person, then separate them", async () => {
  const { app, page } = await launch(newDataDir());
  await loginEcho(page, "alpha");
  await page.getByRole("button", { name: "Add an account" }).click();
  await loginEcho(page, "beta");
  await expect(inboxRows(page).first()).toHaveAttribute("aria-setsize", "8");
  await page.getByRole("button", { name: "Instant Echo" }).first().click();
  await expect(page.getByRole("heading", { name: "Instant Echo" })).toBeVisible();

  // The other account's Instant Echo is offered first, as the same name.
  await page.getByRole("button", { name: "Same person…" }).click();
  const panel = page.getByRole("region", { name: "Same person on another network?" });
  const candidates = panel.getByRole("listitem");
  await expect(candidates.first()).toContainText(/Instant Echo.*same name/);
  await panel.getByLabel("Find their conversation on another network or account").fill("delayed");
  // Both accounts' Delayed Echo.
  await expect(candidates).toHaveCount(2);
  await expect(candidates.first()).toContainText("Delayed Echo");
  await panel.getByLabel("Find their conversation on another network or account").fill("instant");
  await candidates.first().getByRole("button", { name: /^Merge with Instant Echo/ }).click();

  // One entry, one thread with a switcher; the panel closed.
  await expect(panel).toBeHidden();
  await expect(inboxRows(page).first()).toHaveAttribute("aria-setsize", "7");
  const switcher = page.getByRole("group", { name: "Conversations with Instant Echo" });
  await expect(switcher.getByRole("button")).toHaveCount(2);

  await page.getByRole("button", { name: "Person…" }).click();
  const personPanel = page.getByRole("region", { name: "Conversations with Instant Echo" });
  await personPanel.getByLabel("Name", { exact: true }).fill("Echo Friend");
  await personPanel.getByRole("button", { name: "Rename" }).click();
  await expect(page.getByRole("heading", { name: "Echo Friend", level: 2 })).toBeVisible();
  await expect(inboxRows(page).getByRole("button", { name: "Echo Friend" })).toBeVisible();

  // Separating them all asks first; the conversations stay.
  const renamed = page.getByRole("region", { name: "Conversations with Echo Friend" });
  await renamed.getByRole("button", { name: "Separate all…" }).click();
  await renamed.getByRole("button", { name: "Separate all", exact: true }).click();
  await expect(inboxRows(page).first()).toHaveAttribute("aria-setsize", "8");
  await expect(page.getByRole("heading", { name: "Instant Echo", level: 2 })).toBeVisible();
  await expect(page.getByRole("region", { name: "Same person on another network?" })).toBeVisible();
  await expect(page.getByRole("group", { name: /^Conversations with/ })).toBeHidden();
  await app.close();
});

test("a narrow window shows the inbox or the thread, and Back returns to the inbox", async () => {
  const { app, page } = await launch(newDataDir());
  // The window's minimum width (640) is narrower than the phone layout's 720.
  await app.evaluate(({ BrowserWindow }) => BrowserWindow.getAllWindows()[0]!.setSize(660, 720));
  await loginEcho(page, "narrow");
  const inbox = page.getByRole("region", { name: "Conversations" });
  const back = page.getByRole("button", { name: "Back to conversations" });

  await page.getByRole("button", { name: "Instant Echo" }).click();
  await expect(page.getByRole("heading", { name: "Instant Echo" })).toBeVisible();
  await expect(inbox).toBeHidden();
  await sendAndGetEcho(page, "on a narrow window");

  // The history's Back (Android's back gesture) returns to the inbox.
  await page.goBack();
  await expect(inbox).toBeVisible();
  await expect(page.getByRole("heading", { name: "Instant Echo" })).toBeHidden();
  await expect(inboxRows(page).first().getByRole("button")).toHaveAccessibleDescription(/on a narrow window/);

  // So does the thread's own back button.
  await page.getByRole("button", { name: "Echo Group" }).click();
  await expect(inbox).toBeHidden();
  await back.click();
  await expect(inbox).toBeVisible();
  await expect(back).toBeHidden();

  // A wide window shows both, without the back button.
  await app.evaluate(({ BrowserWindow }) => BrowserWindow.getAllWindows()[0]!.setSize(1000, 720));
  await page.getByRole("button", { name: "Echo Group" }).click();
  await expect(page.getByRole("heading", { name: "Echo Group" })).toBeVisible();
  await expect(inbox).toBeVisible();
  await expect(back).toBeHidden();
  await app.close();
});

test("the inbox reads more conversations as the user scrolls", async () => {
  const { app, page } = await launch(newDataDir());
  await loginEcho(page, "crowd");
  // The crowd's contacts each sent a message, the first one two hours ago:
  // the first page (50 conversations) does not reach it. Once they all did:
  type Listed = { conversations: { last_message?: object }[] };
  await expect
    .poll(async () => (await coreCall<Listed>(page, "conversations.list")).conversations.filter((c) => c.last_message).length)
    .toBe(120);
  const oldest = page.getByRole("button", { name: "Crowd Contact 001" });
  const last = inboxRows(page).last();
  const list = page.locator(".inbox-list");
  await expect(async () => {
    await list.evaluate((element) => element.scrollTo(0, element.scrollHeight));
    await expect(last).toHaveAttribute("aria-posinset", "124", { timeout: 1_000 });
  }).toPass({ timeout: 60_000 });
  // The last of the 124 conversations, and few rows in the DOM.
  await expect(last).toHaveAttribute("aria-setsize", "124");
  await expect(last.getByRole("button")).toHaveAccessibleName("Crowd Contact 001");
  await expect(oldest).toHaveAccessibleDescription(/Hello from Crowd Contact 001/);
  expect(await inboxRows(page).count()).toBeLessThan(60);
  await app.close();
});

test("an account without network keeps its conversations and says so", async () => {
  const dataDir = newDataDir();
  let { app, page } = await launch(dataDir);
  await addEchoAccount(page, "offline");
  await sendAndGetEcho(page, "before the network went away");
  await app.close();

  // The echo network connects an "offline" account only right after its login.
  ({ app, page } = await launch(dataDir));
  await expect(page.getByText("No network: the echo network simulates a device without connection.")).toBeVisible();
  const first = inboxRows(page).first().getByRole("button");
  await expect(first).toHaveAccessibleName("Instant Echo");
  await expect(first).toHaveAccessibleDescription(/before the network went away/);
  await first.click();
  await expect(page.getByRole("log").locator(".message", { hasText: "before the network went away" })).toHaveCount(2);
  await page.getByLabel("Message").fill("while offline");
  await page.getByRole("button", { name: "Send" }).click();
  await expect(page.getByRole("log").locator('[data-status="failed"]', { hasText: "while offline" })).toBeVisible();
  await app.close();
});

test("an account whose session ended is logged in to again, and keeps its conversations", async () => {
  const dataDir = newDataDir();
  let { app, page } = await launch(dataDir);
  await addEchoAccount(page, "expired");
  await sendAndGetEcho(page, "before the session ended");
  await app.close();

  // The echo network refuses an "expired" account's session after a restart.
  ({ app, page } = await launch(dataDir));
  await expect(page.getByText("Session ended: the echo network simulates a session that the network ended.")).toBeVisible();
  await page.getByRole("button", { name: "Log in again to Echo: expired" }).click();
  await expect(page.getByRole("heading", { name: "Log in again to Echo: expired" })).toBeVisible();
  // Only the flows of the account's network, and the username filled in.
  await page.getByRole("button", { name: "Echo: Username" }).click();
  await expect(page.getByLabel("Username", { exact: true })).toHaveValue("expired");
  await page.getByRole("button", { name: "Continue" }).click();

  await expect(page.getByText("Connected")).toBeVisible();
  await expect(page.getByRole("button", { name: "Log in again to Echo: expired" })).toBeHidden();
  await page.getByRole("button", { name: "Instant Echo" }).click();
  await expect(page.getByRole("log").locator(".message", { hasText: "before the session ended" })).toHaveCount(2);
  await sendAndGetEcho(page, "after logging in again");
  await app.close();
});

/** The core's key file (core/secrets/keyfile.go, src/core-key.ts). */
function coreKeyFile(dataDir: string): { protection: string; key: string } {
  return JSON.parse(readFileSync(join(dataDir, "core", "core.key"), "utf8")) as { protection: string; key: string };
}

/** Whether safeStorage can keep the core's key here (src/core-key.ts). */
async function secureStorageUsable(app: ElectronApplication): Promise<{ usable: boolean; state: string }> {
  return app.evaluate(({ safeStorage }) => {
    const available = safeStorage.isEncryptionAvailable();
    const backend = process.platform === "linux" ? safeStorage.getSelectedStorageBackend() : "system";
    // The state goes into the failure message: which backend Chromium chose.
    return {
      usable: available && backend !== "basic_text" && backend !== "unknown",
      state: `safeStorage available: ${available}, backend: ${backend}`,
    };
  });
}

/** The key, decrypted by the app's safeStorage. */
function decryptKey(app: ElectronApplication, blob: string): Promise<string> {
  return app.evaluate(({ safeStorage }, encrypted) => safeStorage.decryptString(Buffer.from(encrypted, "base64")), blob);
}

// CI sets MUSUBEE_REQUIRE_SAFESTORAGE on macOS and Linux, where it provides
// a Keychain or a GNOME keyring: there, falling back to the plain key file
// is a failure, not a skip.
const requireSecureStorage = process.env["MUSUBEE_REQUIRE_SAFESTORAGE"] === "1";

test("the core's key is kept by the system's secure storage, and opens the account again", async () => {
  const dataDir = newDataDir();
  let { app, page } = await launch(dataDir);
  await addEchoAccount(page, "keyed");
  await expect(page.getByText("Connected")).toBeVisible();
  const { usable, state } = await secureStorageUsable(app);
  const file = coreKeyFile(dataDir);
  if (process.platform === "win32") {
    expect(file.protection).toBe("dpapi");
  } else {
    expect(usable || !requireSecureStorage, state).toBe(true);
    expect(file.protection).toBe(usable ? "electron-safestorage" : "none");
  }
  if (file.protection === "electron-safestorage") {
    const key = await decryptKey(app, file.key);
    expect(Buffer.from(key, "base64")).toHaveLength(32);
    const log = readFileSync(join(dataDir, "core", "core.log"), "utf8");
    expect(log).toContain('"protection":"host"');
    expect(log).not.toContain(key);
  }
  await app.close();

  // The account's sealed session opens with the same key.
  ({ app, page } = await launch(dataDir));
  await expect(page.getByText("Connected")).toBeVisible();
  expect(coreKeyFile(dataDir)).toEqual(file);
  await app.close();
});

test("a plain key file moves into the system's secure storage, and the account stays", async () => {
  // Chromium's --password-store=basic exists on Linux only: it hides the
  // keyring from the first start, so the core makes its plain key file. The
  // app applies it in end-to-end mode only (src/main.ts), since it removes
  // the one that Playwright adds to every start.
  test.skip(process.platform !== "linux", "Linux only");
  const dataDir = newDataDir();
  let { app, page } = await launch(dataDir, { MUSUBEE_E2E_PASSWORD_STORE: "basic" });
  await addEchoAccount(page, "moved");
  await expect(page.getByText("Connected")).toBeVisible();
  const plain = coreKeyFile(dataDir);
  expect(plain.protection).toBe("none");
  await app.close();

  ({ app, page } = await launch(dataDir));
  const { usable, state } = await secureStorageUsable(app);
  expect(usable || !requireSecureStorage, state).toBe(true);
  test.skip(!usable, "no keyring on this desktop");
  await expect(page.getByText("Connected")).toBeVisible();
  const moved = coreKeyFile(dataDir);
  expect(moved.protection).toBe("electron-safestorage");
  expect(await decryptKey(app, moved.key)).toBe(plain.key);
  expect(readFileSync(join(dataDir, "core", "core.key"), "utf8")).not.toContain(plain.key);
  await app.close();
});

test("a lost key offers to start over, which sets the old data aside and gives an empty app", async () => {
  const dataDir = newDataDir();
  let { app, page } = await launch(dataDir);
  await addEchoAccount(page, "lost");
  await expect(page.getByText("Connected")).toBeVisible();
  // Starting over is refused while the key opens.
  await expect(page.evaluate(() => window.musubee!.core.startOver!())).rejects.toThrow("the core's key is not lost");
  await app.close();

  // As if the system's secure storage was reset: DPAPI or safeStorage
  // cannot decrypt the key file any more. The byte flipped is in the
  // second-to-last block, so that safeStorage's CBC padding always breaks;
  // a plain key file gets another key, which does not open the database.
  const path = join(dataDir, "core", "core.key");
  const file = coreKeyFile(dataDir);
  if (file.protection === "none") {
    file.key = randomBytes(32).toString("base64");
  } else {
    const sealed = Buffer.from(file.key, "base64");
    sealed[sealed.length - 17]! ^= 0xff;
    file.key = sealed.toString("base64");
  }
  const broken = JSON.stringify(file);
  writeFileSync(path, broken);

  ({ app, page } = await launch(dataDir, {}, /cannot open your data/));
  await expect(page.getByRole("heading", { name: "Musubee cannot open your data" })).toBeVisible();
  expect(await corePid(app)).toBeUndefined();
  await page.getByRole("button", { name: "Start over…" }).click();
  await page.getByRole("button", { name: "Start over", exact: true }).click();

  // An empty app, which adds an account as on the first start.
  await expect(page.getByRole("status", { name: "App status" })).toHaveText("Ready");
  await expect(page.getByRole("heading", { name: "Add an account" })).toBeVisible();
  const aside = readdirSync(dataDir).filter((name) => name.startsWith("core-unreadable-"));
  expect(aside).toHaveLength(1);
  expect(existsSync(join(dataDir, aside[0]!, "core.db"))).toBe(true);
  expect(readFileSync(join(dataDir, aside[0]!, "core.key"), "utf8")).toBe(broken);
  await addEchoAccount(page, "again");
  await expect(page.getByText("Connected")).toBeVisible();
  await app.close();

  // The new key opens the new data.
  ({ app, page } = await launch(dataDir));
  await expect(page.getByText("Connected")).toBeVisible();
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
