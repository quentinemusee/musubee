// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

// "Hello test" for Android (T0.5): proves that the Android toolchain works
// end to end (emulator or device, Appium, UiAutomator2 driver) by opening
// the system Settings app, independently of Musubee's own app
// (android.app.test.mjs).
//
// Requires a running Android emulator or connected device, chosen by
// MUSUBEE_ANDROID_UDID (see android-device.mjs).
// Run: npm run test:android

import { after, before, test } from "node:test";
import assert from "node:assert/strict";
import { remote } from "webdriverio";
import { androidTimeouts, targetDevice } from "./android-device.mjs";
import { remoteOptions, startAppium } from "./appium-server.mjs";

let appium;
let driver;

before(async () => {
  appium = await startAppium();
  driver = await remote({
    ...remoteOptions(appium.url),
    logLevel: "warn",
    connectionRetryTimeout: 300_000,
    capabilities: {
      platformName: "Android",
      "appium:automationName": "UiAutomator2",
      "appium:udid": targetDevice(),
      "appium:appPackage": "com.android.settings",
      "appium:appActivity": ".Settings",
      "appium:noReset": true,
      ...androidTimeouts,
    },
  });
});

after(async () => {
  await driver?.deleteSession().catch(() => {});
  await appium?.stop();
});

const SETTINGS = "com.android.settings";

test("the Settings app comes to the foreground", async () => {
  // Appium starts the app before its UiAutomator2 server has settled, and the
  // launcher can come back on top meanwhile: bring the app forward and wait.
  await driver.activateApp(SETTINGS);
  await driver.waitUntil(async () => (await driver.getCurrentPackage()) === SETTINGS, {
    timeout: 60_000,
    interval: 1_000,
    timeoutMsg: "the Settings app did not reach the foreground within 60 s",
  });
});

test("its screen can be read through UiAutomator2", async () => {
  // On a slow emulator the app can be in front before its first screen is
  // drawn, or a system dialog can cover it for a moment: wait for it.
  const found = await driver
    .waitUntil(async () => (await driver.$$(`android=new UiSelector().packageName("${SETTINGS}")`)).length > 0, {
      timeout: 30_000,
      interval: 1_000,
    })
    .catch(() => false);
  const source = await driver.getPageSource();
  assert.ok(found, `no element of the Settings app found on screen within 30 s; screen:
${source.slice(0, 4000)}`);
  assert.ok(source.includes(`package="${SETTINGS}"`), "the page source does not contain the Settings app");
});
