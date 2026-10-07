// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

// "Hello test" for Android (T0.5): proves that the Android toolchain works
// end to end (emulator or device, Appium, UiAutomator2 driver) by opening
// the system Settings app. Musubee's own app replaces it from T1.3 on.
//
// Requires a running Android emulator or connected device. Set
// MUSUBEE_ANDROID_UDID (see "adb devices") to choose it; without it the test
// uses the only device attached and refuses to guess when there are several,
// so that it never touches a personal phone by accident.
// Run: npm run test:android

import { after, before, test } from "node:test";
import assert from "node:assert/strict";
import { execFileSync } from "node:child_process";
import { join } from "node:path";
import { remote } from "webdriverio";
import { remoteOptions, startAppium } from "./appium-server.mjs";

function adbPath() {
  const sdk = process.env.ANDROID_HOME || process.env.ANDROID_SDK_ROOT;
  return sdk ? join(sdk, "platform-tools", "adb") : "adb";
}

/** Parses "adb devices" output into the serials of ready devices. */
export function readyDevices(output) {
  return output
    .split(/\r?\n/)
    .slice(1)
    .map((line) => line.trim().split(/\s+/))
    .filter(([serial, state]) => serial && state === "device")
    .map(([serial]) => serial);
}

function targetDevice() {
  if (process.env.MUSUBEE_ANDROID_UDID) {
    return process.env.MUSUBEE_ANDROID_UDID;
  }
  const devices = readyDevices(execFileSync(adbPath(), ["devices"], { encoding: "utf8" }));
  if (devices.length === 1) {
    return devices[0];
  }
  throw new Error(
    devices.length === 0
      ? "no Android device or emulator is ready (adb devices)"
      : `several Android devices are attached (${devices.join(", ")}): set MUSUBEE_ANDROID_UDID to the emulator to use`,
  );
}

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
      "appium:newCommandTimeout": 300,
      "appium:adbExecTimeout": 120_000,
      "appium:uiautomator2ServerInstallTimeout": 180_000,
      "appium:uiautomator2ServerLaunchTimeout": 180_000,
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
  const elements = await driver.$$(`android=new UiSelector().packageName("${SETTINGS}")`);
  assert.ok(elements.length > 0, "no element of the Settings app found on screen");
  const source = await driver.getPageSource();
  assert.ok(source.includes(`package="${SETTINGS}"`), "the page source does not contain the Settings app");
});
