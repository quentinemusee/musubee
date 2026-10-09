// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

// The Telegram journey on the desktop (T1.6): through the interface, the
// user adds a Telegram account, receives a message, answers it and logs
// out; the core in the app's child process talks to production Telegram
// itself (ADR 0014). It uses the test bots and the private channel of
// ADR 0006 (see infra/README.md):
//
//   MUSUBEE_TG_BRIDGE_BOT_TOKEN  bot the app logs in as
//   MUSUBEE_TG_PEER_BOT_TOKEN    bot playing the remote party
//   MUSUBEE_TG_CHAT_ID           the channel
//   MUSUBEE_TG_API_ID, MUSUBEE_TG_API_HASH  the application, passed to the core
//
// Without them it is skipped, unless MUSUBEE_REQUIRE_TELEGRAM_E2E=1. A bot has
// one session at a time: never run it alongside another Telegram test.
//
//   npm run build && npx playwright test telegram.spec.ts

import { randomBytes } from "node:crypto";
import { mkdtempSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join, resolve as resolvePath } from "node:path";
import { fileURLToPath } from "node:url";
import { _electron as electron, expect, test } from "@playwright/test";

const appDir = resolvePath(fileURLToPath(new URL("..", import.meta.url)));

const env = {
  bridgeToken: process.env["MUSUBEE_TG_BRIDGE_BOT_TOKEN"] ?? "",
  peerToken: process.env["MUSUBEE_TG_PEER_BOT_TOKEN"] ?? "",
  chatId: process.env["MUSUBEE_TG_CHAT_ID"] ?? "",
  apiId: process.env["MUSUBEE_TG_API_ID"] ?? "",
  apiHash: process.env["MUSUBEE_TG_API_HASH"] ?? "",
};
const configured = Object.values(env).every((value) => value !== "");
const required = process.env["MUSUBEE_REQUIRE_TELEGRAM_E2E"] === "1";

// A trace records what is typed, the bot token included: none for this file.
test.use({ trace: "off" });

/** The few Bot API methods the peer bot needs. Errors never quote the token. */
async function botApi<T>(method: string, params: Record<string, unknown>): Promise<T> {
  const response = await fetch(`https://api.telegram.org/bot${env.peerToken}/${method}`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(params),
    signal: AbortSignal.timeout(30_000),
  });
  const body = (await response.json()) as { ok: boolean; result?: T; description?: string };
  if (!body.ok || body.result === undefined) {
    throw new Error(`Bot API ${method}: ${body.description ?? response.status}`);
  }
  return body.result;
}

interface Update {
  message?: { chat: { id: number }; text?: string };
  channel_post?: { chat: { id: number }; text?: string };
}

/** Polls the peer bot's updates until a post with this text appears in the channel. */
async function peerSees(text: string, withinMs: number): Promise<boolean> {
  const deadline = Date.now() + withinMs;
  while (Date.now() < deadline) {
    const updates = await botApi<Update[]>("getUpdates", { timeout: 10, allowed_updates: ["message", "channel_post"] });
    if (updates.some((u) => (u.channel_post ?? u.message)?.text === text && String((u.channel_post ?? u.message)?.chat.id) === env.chatId)) {
      return true;
    }
    await new Promise((resolve) => setTimeout(resolve, 2_000));
  }
  return false;
}

test("add a Telegram account, receive a message, answer it, log out", async () => {
  test.skip(!configured && !required, "set the Telegram test variables to run it (see infra/README.md)");
  expect(configured, "the Telegram test variables are required").toBe(true);
  test.setTimeout(8 * 60_000);

  const dataDir = mkdtempSync(join(tmpdir(), "musubee-desktop-telegram-"));
  const app = await electron.launch({
    args: [appDir],
    // The app passes its environment, and so the application credentials,
    // to the core.
    env: { ...process.env, MUSUBEE_USER_DATA_DIR: dataDir, MUSUBEE_E2E: "1" },
  });
  let loggedIn = false;
  try {
    const page = await app.firstWindow();

    // 1. Add the account with the bot flow.
    await expect(page.getByRole("heading", { name: "Add an account" })).toBeVisible();
    await page.getByRole("button", { name: "Telegram: Bot token" }).click();
    const tokenField = page.getByLabel("Bot token");
    await expect(tokenField).toHaveAttribute("type", "password");
    await tokenField.fill(env.bridgeToken);
    await page.getByRole("button", { name: "Continue" }).click();
    await expect(page.getByRole("heading", { name: "Accounts" })).toBeVisible({ timeout: 60_000 });
    loggedIn = true;
    await expect(page.locator(".account-state", { hasText: "Connected" })).toBeVisible({ timeout: 120_000 });

    // 2. The peer bot posts in the channel; the post shows in its
    // conversation. A missed post is retried with a new message.
    let inbound = "";
    for (let attempt = 1; attempt <= 3 && inbound === ""; attempt++) {
      const text = `musubee desktop e2e: from Telegram ${randomBytes(4).toString("hex")} (attempt ${attempt})`;
      await botApi("sendMessage", { chat_id: Number(env.chatId), text });
      try {
        await expect(page.getByRole("button", { name: /Musubee CI/ })).toBeVisible({ timeout: 60_000 });
        await page.getByRole("button", { name: /Musubee CI/ }).click();
        await expect(page.getByRole("log").locator('[data-from-me="false"]', { hasText: text })).toBeVisible({ timeout: 60_000 });
        inbound = text;
      } catch {
        console.log(`attempt ${attempt}: the post did not show within a minute`);
      }
    }
    expect(inbound, "a post of the peer bot reached the app").not.toBe("");

    // 3. Answer it; the peer bot sees the answer.
    const outbound = `musubee desktop e2e: from the app ${randomBytes(4).toString("hex")}`;
    await page.getByLabel("Message").fill(outbound);
    await page.getByRole("button", { name: "Send" }).click();
    await expect(page.getByRole("log").locator('[data-from-me="true"]', { hasText: outbound })).toContainText("Sent", { timeout: 60_000 });
    expect(await peerSees(outbound, 2 * 60_000), "the peer bot saw the answer").toBe(true);

    // 4. Log out: the account and its conversations go away.
    await page.getByRole("button", { name: /^Log out of Telegram/ }).click();
    await page.getByRole("button", { name: "Log out", exact: true }).click();
    await expect(page.getByRole("heading", { name: "Add an account" })).toBeVisible({ timeout: 60_000 });
    loggedIn = false;
  } finally {
    if (loggedIn) {
      // Never leave a session of the bot behind: the next run could not
      // receive its updates.
      const page = await app.firstWindow();
      await page.evaluate(
        `(async () => { const { accounts } = JSON.parse(await window.musubee.core.call(JSON.stringify({ id: 1, command: "accounts.list" }))).result;
           for (const a of accounts) await window.musubee.core.call(JSON.stringify({ id: 2, command: "accounts.logout", params: { account_id: a.account_id } })); })()`,
      );
    }
    await app.close();
    rmSync(dataDir, { recursive: true, force: true, maxRetries: 5 });
  }
});
