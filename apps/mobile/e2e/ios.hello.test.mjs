// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

// "Hello test" for iOS (T0.5): proves that the iOS toolchain works end to
// end (Xcode, simulator, Appium, XCUITest driver with WebDriverAgent) by
// opening the system Settings app. Musubee's own app replaces it later (E3.3).
//
// Requires macOS with Xcode and a booted simulator. MUSUBEE_IOS_UDID selects
// it (xcrun simctl list devices booted); otherwise the first booted one is used.
// Run: npm run test:ios

import { after, before, test } from "node:test";
import assert from "node:assert/strict";
import { execFileSync } from "node:child_process";
import { remote } from "webdriverio";
import { remoteOptions, startAppium } from "./appium-server.mjs";

const SETTINGS = "com.apple.Preferences";
// XCUIApplicationState: 4 = running in the foreground.
const RUNNING_FOREGROUND = 4;

function bootedSimulator() {
  if (process.env.MUSUBEE_IOS_UDID) {
    return process.env.MUSUBEE_IOS_UDID;
  }
  const out = execFileSync("xcrun", ["simctl", "list", "devices", "booted", "--json"], { encoding: "utf8" });
  for (const devices of Object.values(JSON.parse(out).devices)) {
    const booted = devices.find((d) => d.state === "Booted");
    if (booted) {
      return booted.udid;
    }
  }
  throw new Error("no booted iOS simulator (boot one with xcrun simctl boot <udid>)");
}

let appium;
let driver;

before(async () => {
  appium = await startAppium();
  driver = await remote({
    ...remoteOptions(appium.url),
    logLevel: "warn",
    // The first session builds WebDriverAgent with Xcode, which takes minutes.
    connectionRetryTimeout: 900_000,
    capabilities: {
      platformName: "iOS",
      "appium:automationName": "XCUITest",
      "appium:udid": bootedSimulator(),
      "appium:bundleId": SETTINGS,
      "appium:noReset": true,
      "appium:newCommandTimeout": 300,
      "appium:wdaLaunchTimeout": 900_000,
      "appium:wdaConnectionTimeout": 900_000,
    },
  });
});

after(async () => {
  await driver?.deleteSession().catch(() => {});
  await appium?.stop();
});

test("the Settings app is in the foreground", async () => {
  const state = await driver.execute("mobile: queryAppState", { bundleId: SETTINGS });
  assert.equal(state, RUNNING_FOREGROUND);
});

test("its screen can be read through XCUITest", async () => {
  const source = await driver.getPageSource();
  assert.match(source, /XCUIElementTypeApplication/);
  assert.match(source, /Settings/);
});
