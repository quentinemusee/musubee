// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

// Chooses the Android device of the tests. MUSUBEE_ANDROID_UDID (see
// "adb devices") selects it; without it the tests use the only device
// attached and refuse to guess when there are several, so that they never
// touch a personal phone by accident.

import { execFileSync } from "node:child_process";
import { join } from "node:path";

export function adbPath() {
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

export function targetDevice() {
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

/** Appium capabilities shared by the Android tests (slow CI emulators). */
export const androidTimeouts = {
  "appium:newCommandTimeout": 300,
  "appium:adbExecTimeout": 120_000,
  "appium:uiautomator2ServerInstallTimeout": 180_000,
  "appium:uiautomator2ServerLaunchTimeout": 180_000,
};

/**
 * Taps "Wait" on an "<app> isn't responding" system dialog, which a freshly
 * booted CI emulator sometimes shows over every app for its launcher (seen
 * on API 35). Returns whether it did. An ANR of the app under test
 * (ownApp, its label) is a real failure: it throws instead of hiding it.
 */
export async function dismissAnrDialog(driver, ownApp) {
  const wait = await driver.$('android=new UiSelector().resourceId("android:id/aerr_wait")');
  if (!(await wait.isExisting())) {
    return false;
  }
  const title = await driver
    .$('android=new UiSelector().resourceId("android:id/alertTitle")')
    .getText()
    .catch(() => "");
  if (ownApp && title.includes(ownApp)) {
    throw new Error(`the app under test is not responding: ${title}`);
  }
  console.log(`dismissing a system dialog: ${title || "an app is not responding"}`);
  await wait.click();
  return true;
}
