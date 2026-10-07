# apps/mobile/e2e — mobile end-to-end tests

Appium 3 with WebdriverIO, run by Node's built-in test runner. Why Appium and not Maestro: [ADR 0007](../../../docs/ADR/0007-mobile-e2e-tool.md).

Until Musubee has a mobile app, the **hello tests** open the system Settings app to prove that the toolchain works: emulator or simulator, Appium, the platform driver.

## Setup

Node 24, then from this directory:

```
npm ci
```

Appium and its drivers (UiAutomator2 for Android, XCUITest for iOS) are project dependencies; nothing is installed globally.

## Android

Requires the Android SDK (`ANDROID_HOME`) and a running emulator.

```
MUSUBEE_ANDROID_UDID=emulator-5554 npm run test:android
```

The test refuses to run when several devices are attached and `MUSUBEE_ANDROID_UDID` is not set, so that it never touches a personal phone by accident (`adb devices` lists them).

Creating an emulator once (Windows example; the virtual device is placed on `D:` because it needs several gigabytes):

```
set ANDROID_AVD_HOME=D:\Android\avd
avdmanager create avd -n musubee_api30 -k "system-images;android-30;google_apis;x86" -d pixel
emulator -avd musubee_api30 -no-window -no-audio -no-boot-anim -no-snapshot -gpu swiftshader_indirect -partition-size 2047
```

## iOS

Requires macOS with Xcode and a booted simulator (`xcrun simctl boot <udid>`).

```
MUSUBEE_IOS_UDID=<udid> npm run test:ios
```

The first session builds WebDriverAgent with Xcode, which takes several minutes.

## CI

`.github/workflows/checks.yml`: Android on Linux with KVM and `reactivecircus/android-emulator-runner`; iOS on macOS with the first available iPhone simulator.
