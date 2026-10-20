// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

// Musubee's Android app end to end: installs the APK, adds an echo account
// through the interface (ui/, copied into the app), opens a conversation of
// the unified inbox, sends a message, waits for its echo, then goes back to
// the inbox with Android's back action; then a message that arrives in the
// background shows a notification, which opens its conversation. This
// crosses every layer: the interface in the web view, the Capacitor plugin,
// the foreground service, the JNI binding and the Go core.
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
// of the web view: no chromedriver is needed. A button's accessible name
// is its text, and its accessible description (aria-describedby) its hint;
// a text field's label is its hint.

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
  // A field that never shows up or stays disabled: print what the screen
  // holds instead, since the error alone does not tell the two apart.
  await element.waitForEnabled({ timeout: 10_000 }).catch(async (error) => {
    console.log(`Field exists: ${await element.isExisting().catch(() => "unknown")}`);
    await describeScreen();
    throw error;
  });
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
    // The thread's only text field. Not by its label: WebView 83 adds the
    // placeholder to the hint, 124 does not, and 153 on Android 17 exposes
    // neither the label nor the placeholder (docs/ADR/0029).
    const composer = driver.$("//android.widget.EditText");
    await composer.waitForExist({ timeout: 60_000 }).catch(async (error) => {
      await describeScreen();
      throw error;
    });
    await typeInto(composer, text);
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

// Notifications (docs/ADR/0033): a message that arrives while the app is in
// the background shows one, and tapping it opens its conversation. Delayed
// Echo answers 2 s after a message: the test sends, returns to the inbox and
// leaves the app within that time, and tries again when a slow device let
// the echo arrive on screen (no notification then, by design).
test(
  "a message that arrives in the background is notified, and the notification opens its conversation",
  { skip: tooOld && `WebView ${webView} is older than ${MIN_WEBVIEW}` },
  async () => {
    const ATTEMPTS = 5;
    for (let attempt = 1; ; attempt++) {
      await (await waitFor(exactly("Delayed Echo"), "Delayed Echo in the inbox", { kind: BUTTON })).click();
      const composer = driver.$("//android.widget.EditText");
      await composer.waitForExist({ timeout: 30_000 });
      const text = `notify me ${attempt}`;
      await typeInto(composer, text);
      await (await waitFor(exactly("Send"), "the Send button", { kind: BUTTON })).click();
      await driver.back();
      await driver.execute("mobile: pressKey", { keycode: 3 }); // Home.

      await driver.openNotifications();
      const notification = driver.$(`//*[contains(@text, "${text}")]`);
      const shown = await notification.waitForExist({ timeout: 15_000 }).then(
        () => true,
        () => false,
      );
      if (shown) {
        await notification.click();
        break;
      }
      if (attempt === ATTEMPTS) {
        await describeScreen();
        throw new Error(`no notification in ${ATTEMPTS} attempts`);
      }
      console.log(`attempt ${attempt}: no notification, the echo probably arrived on screen`);
      await driver.back(); // Closes the shade.
      await driver.activateApp(APP);
    }

    // The app opens on the conversation, from the inbox where it was left:
    // the thread, under its title. Not by the echo's text: it came in the
    // background, so the thread's status did not announce it, and WebView
    // 124 does not expose the text of the message rows.
    await waitFor(exactly("Back to conversations"), "the thread", { kind: BUTTON });
    await waitFor(exactly("Delayed Echo"), "the thread of Delayed Echo", { kind: "android.widget.TextView" });
  },
);
