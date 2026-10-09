// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

// Musubee's Android app end to end (T1.3): installs the APK, logs in to the
// echo network through the web page (apps/mobile/www), sends a message and
// waits for its echo. This crosses every layer: the web view, the Capacitor
// plugin, the foreground service, the JNI binding and the Go core.
//
// Requires a running Android emulator or device, chosen by
// MUSUBEE_ANDROID_UDID (see android-device.mjs), and the debug APK
// (apps/mobile/README.md); MUSUBEE_ANDROID_APK overrides its path.
// Run: npm run test:android:app
//
// The page is read through UiAutomator2, which sees the accessibility tree
// of the web view: no chromedriver is needed.

import { after, before, test } from "node:test";
import { existsSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { remote } from "webdriverio";
import { androidTimeouts, targetDevice } from "./android-device.mjs";
import { remoteOptions, startAppium } from "./appium-server.mjs";

const APP = "app.musubee";
const APK =
  process.env.MUSUBEE_ANDROID_APK ||
  resolve(dirname(fileURLToPath(import.meta.url)), "../android/app/build/outputs/apk/debug/app-debug.apk");

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
      "appium:udid": targetDevice(),
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

/** An element of the page whose text or description matches the XPath predicate. */
function byLabel(predicate) {
  return driver.$(`//*[${predicate("@text")} or ${predicate("@content-desc")}]`);
}

const exactly = (label) => (attr) => `${attr}="${label}"`;
const containing = (label) => (attr) => `contains(${attr}, "${label}")`;

async function waitFor(predicate, what, timeout = 60_000) {
  const element = byLabel(predicate);
  await element.waitForExist({ timeout, interval: 500, timeoutMsg: `${what} not shown within ${timeout / 1000} s` });
  return element;
}

test("the app logs in and gets the echo of a message", async () => {
  // The first start loads the web view and the core: allow for slow CI.
  await (await waitFor(exactly("Log in"), "the Log in button", 120_000)).click();
  await waitFor(exactly("Logged in, conversation: Instant Echo"), "the logged-in status");

  const text = "hello from Appium";
  const input = await driver.$("//android.widget.EditText");
  await input.waitForEnabled({ timeout: 10_000 });
  await input.setValue(text);
  await (await byLabel(exactly("Send"))).click();

  await waitFor(containing(`Sent: ${text}`), "the sent message");
  await waitFor((attr) => `starts-with(${attr}, "Received: ") and contains(${attr}, "${text}")`, "the echo");
});
