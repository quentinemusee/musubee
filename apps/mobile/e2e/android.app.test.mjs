// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

// Musubee's Android app end to end: installs the APK, adds an echo account
// through the interface (ui/, copied into the app), opens a conversation of
// the unified inbox, sends a message, waits for its echo, then goes back to
// the inbox with Android's back action. This crosses every layer: the
// interface in the web view, the Capacitor plugin, the foreground service,
// the JNI binding and the Go core.
//
// The interface needs a recent system web view (docs/ADR/0021): on an older
// one, the app shows a page asking to update it, and that page is all this
// test checks (the API 30 emulator images ship WebView 83).
//
// Requires a running Android emulator or device, chosen by
// MUSUBEE_ANDROID_UDID (see android-device.mjs), and the debug APK
// (apps/mobile/README.md); MUSUBEE_ANDROID_APK overrides its path.
// Run: npm run test:android:app
//
// The page is read through UiAutomator2, which sees the accessibility tree
// of the web view: no chromedriver is needed. An element's accessible name
// is its text, and its accessible description (aria-describedby) its hint.

import { after, before, test } from "node:test";
import { existsSync, readFileSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { remote } from "webdriverio";
import { androidTimeouts, deviceWebViewMajor, dismissAnrDialog, targetDevice } from "./android-device.mjs";
import { remoteOptions, startAppium } from "./appium-server.mjs";

const APP = "app.musubee";
const HERE = dirname(fileURLToPath(import.meta.url));
const APK = process.env.MUSUBEE_ANDROID_APK || resolve(HERE, "../android/app/build/outputs/apk/debug/app-debug.apk");
const MIN_WEBVIEW = JSON.parse(readFileSync(resolve(HERE, "../capacitor.config.json"), "utf8")).android.minWebViewVersion;

const udid = targetDevice();
const webView = deviceWebViewMajor(udid);
if (webView === undefined) {
  throw new Error(`cannot read the web view version of ${udid} (adb shell dumpsys webviewupdate)`);
}
const tooOld = webView < MIN_WEBVIEW;

let appium;
let driver;

before(async () => {
  if (!existsSync(APK)) {
    throw new Error(`APK not found: ${APK} (build it first, see apps/mobile/README.md)`);
  }
  appium = await startAppium();
  driver = await remote({
    ...remoteOptions(appium.url),
    logLevel: "warn",
    connectionRetryTimeout: 300_000,
    capabilities: {
      platformName: "Android",
      "appium:automationName": "UiAutomator2",
      "appium:udid": udid,
      "appium:app": APK,
      "appium:appPackage": APP,
      "appium:appActivity": ".MainActivity",
      // A fresh install each run: no data left by a previous run.
      "appium:fullReset": true,
      "appium:autoGrantPermissions": true,
      ...androidTimeouts,
    },
  });
});

after(async () => {
  await driver?.removeApp(APP).catch(() => {});
  await driver?.deleteSession().catch(() => {});
  await appium?.stop();
});

/** An element of the page whose text, description or hint matches the XPath predicate. */
function byLabel(predicate, kind = "*") {
  return driver.$(`//${kind}[${predicate("@text")} or ${predicate("@content-desc")} or ${predicate("@hint")}]`);
}

const exactly = (label) => (attr) => `${attr}="${label}"`;
const containing = (label) => (attr) => `contains(${attr}, "${label}")`;
const BUTTON = "android.widget.Button";

// Waits for an element, closing an "isn't responding" dialog of another app
// that would cover the page (dismissAnrDialog).
// On a timeout, prints what the screen shows, to tell why from CI's log.
async function waitFor(predicate, what, { kind, timeout = 60_000 } = {}) {
  const element = byLabel(predicate, kind);
  try {
    await driver.waitUntil(
      async () => {
        await dismissAnrDialog(driver, "Musubee");
        return element.isExisting();
      },
      { timeout, interval: 500, timeoutMsg: `${what} not shown within ${timeout / 1000} s` },
    );
  } catch (error) {
    await describeScreen();
    throw error;
  }
  return element;
}

async function describeScreen() {
  const keyboard = await driver.isKeyboardShown().catch(() => "unknown");
  const source = await driver.getPageSource().catch(() => "");
  const attribute = (node, name) => new RegExp(` ${name}="([^"]*)"`).exec(node)?.[1] ?? "";
  const nodes = [...source.matchAll(/<[\w.]+ [^>]*>/g)]
    .map(([node]) => ({ node, text: attribute(node, "text"), hint: attribute(node, "hint") }))
    .filter(({ text, hint }) => text !== "" || (hint !== "" && hint !== "false"))
    .map(({ node, text, hint }) => `  ${attribute(node, "class").split(".").pop()} text=${JSON.stringify(text)} hint=${JSON.stringify(hint)} ${attribute(node, "bounds")} displayed=${attribute(node, "displayed")}`);
  const size = await driver.getWindowSize().catch(() => ({}));
  console.log(`Keyboard shown: ${keyboard}; window ${JSON.stringify(size)}\n${nodes.join("\n")}`);
}

async function typeInto(element, text) {
  await element.waitForEnabled({ timeout: 10_000 });
  await element.click();
  await element.setValue(text);
}

test(
  `an outdated web view gets a page asking to update it (WebView ${webView}, needs ${MIN_WEBVIEW})`,
  { skip: !tooOld && `WebView ${webView} is recent enough` },
  async () => {
    // The first start loads the web view and the core: allow for slow CI.
    await waitFor(exactly("Update Android System WebView"), "the update page", { timeout: 120_000 });
  },
);

test(
  "the app adds an account, sends from the inbox and gets the echo",
  { skip: tooOld && `WebView ${webView} is older than ${MIN_WEBVIEW}` },
  async () => {
    await (await waitFor(exactly("Echo: Username"), "the echo login", { kind: BUTTON, timeout: 120_000 })).click();
    await typeInto(await driver.$("//android.widget.EditText"), "alice");
    await (await waitFor(exactly("Continue"), "the Continue button", { kind: BUTTON })).click();

    // The inbox: the echo network's conversations, then one of them.
    await waitFor(exactly("Echo Group"), "the group in the inbox", { kind: BUTTON });
    await (await waitFor(exactly("Instant Echo"), "Instant Echo in the inbox", { kind: BUTTON })).click();

    const text = "hello from Appium";
    await typeInto(await waitFor(containing("Write a message"), "the composer", { kind: "android.widget.EditText" }), text);
    await (await waitFor(exactly("Send"), "the Send button", { kind: BUTTON })).click();
    await waitFor(exactly(`Instant Echo: ${text}`), "the echo");

    // Back returns to the inbox (a phone shows one pane at a time), where
    // the conversation's row now previews the echo.
    await driver.back();
    const row = await waitFor(exactly("Instant Echo"), "the inbox", { kind: BUTTON });
    await driver.waitUntil(async () => (await row.getAttribute("hint"))?.includes(text), {
      timeout: 30_000,
      timeoutMsg: "the inbox does not preview the echo",
    });
    if (await byLabel(exactly("Back to conversations"), BUTTON).isExisting()) {
      throw new Error("the thread is still shown after Back");
    }
  },
);
