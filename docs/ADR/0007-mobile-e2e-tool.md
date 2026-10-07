# ADR 0007 — Mobile end-to-end test tool: Appium with WebdriverIO

- **Status**: accepted
- **Date**: 2026-10-07
- **Task**: T0.5
- **Deciders**: Quentin Raimbaud (maintainer), with the design assistant

## Context

T0.5 asks to choose the mobile end-to-end test tool between Maestro and Appium, and to run a "hello test" on every target in CI. `docs/TESTING.md` requires Android tests on an emulator and on a **real device** before each release, and iOS tests on a simulator and on a **real iPhone** (the simulator does not reproduce background behavior or the notification extension). Musubee's mobile UI is web code inside Capacitor's WebView, with a thin native shell.

## Options considered

### Maestro
- Pros: simple YAML flows, fast to write, good stability handling; WebViews handled through the accessibility tree; Apache-2.0.
- Cons: **iOS support is limited to simulators**; real iPhones need third-party bridges or a paid device cloud. YAML flows sit outside the TypeScript code base.

### Appium (UiAutomator2 and XCUITest drivers) with WebdriverIO
- Pros: **real devices and emulators/simulators on both platforms**; can switch into the WebView context and use web selectors on our UI; JavaScript/TypeScript, like the UI; recommended by the `capacitor-testing` skill; Apache-2.0 (Appium, drivers), MIT (WebdriverIO).
- Cons: heavier setup (Appium server, drivers, WebDriverAgent built with Xcode on iOS); slower sessions; more flakiness to manage by hand (explicit waits).

## Decision

1. **Appium 3 with WebdriverIO**, in `apps/mobile/e2e/` (Node 24, `node:test` runner, versions pinned in `package.json` and `package-lock.json`). Drivers are project dependencies (`appium-uiautomator2-driver`, `appium-xcuitest-driver`); Appium finds them without a global install.
2. Until Musubee has an app (T1.3, E3.3), the **hello tests** open the system Settings app, which proves the whole toolchain: emulator or simulator, Appium server, driver, WebDriverAgent on iOS.
3. The Android test **never guesses the device**: it uses `MUSUBEE_ANDROID_UDID`, or the only attached device, and refuses when several are attached, so that it cannot touch a personal phone by accident.
4. CI: Android on `ubuntu-latest` with KVM and `reactivecircus/android-emulator-runner` (API 30, x86_64, Google APIs); iOS on `macos-latest` with the first available iPhone simulator. Desktop jobs (repository checks, Go) now also run on macOS.

## State of knowledge

| Claim | Status | Source |
|---|---|---|
| Maestro supports Android emulators and devices, and iOS **simulators only** | **verified** (official docs, supported platforms) | <https://docs.maestro.dev/platform-support/supported-platforms>, 2026-10-07 |
| Licenses: Maestro Apache-2.0; Appium, UiAutomator2 and XCUITest drivers Apache-2.0; WebdriverIO MIT | **verified** (GitHub license API, npm metadata) | 2026-10-07 |
| The 436 npm packages installed are under permissive or compatible licenses (MIT, ISC, Apache-2.0, BSD, BlueOak, CC0, Unlicense, WTFPL, PSF-2.0, CC-BY-3.0 data, LGPL-3.0 for libvips in `sharp`); the "unknown" ones are platform builds of `sharp` that are not installed; `css-value` is MIT (README) | **verified** (lockfile and installed `package.json` files) | local scan, 2026-10-07; automated audit in T0.6 |
| Appium finds drivers declared as project dependencies | **verified** (`appium driver list --installed`) | local run |
| The Android hello test passes on an API 30 emulator | **verified** (3 runs out of 3 locally, 10 to 21 s) | local runs, 2026-10-07 |
| Appium starts the app before its UiAutomator2 server has settled, and the launcher can come back on top | **verified** (first runs failed with the launcher in front); the test now activates the app and waits for it | local runs |
| The test refuses to run with two devices attached and no `MUSUBEE_ANDROID_UDID` | **verified** (with a phone and the emulator attached; nothing was installed on the phone) | local run |
| The Android and iOS jobs pass on GitHub's runners | **verified**: Android 2/2 on the API 30 emulator (job 2 min 49 s); iOS 2/2 on an iPhone 17 Pro simulator with Xcode 26.6 (job 4 min 50 s, of which about 168 s to build WebDriverAgent) | CI, PR #8, 2026-10-07 |
| Appium works on real iPhones for our needs (signing WebDriverAgent with the paid Apple account) | **assumed** (documented by the XCUITest driver); to verify with the device in E3.3 | — |

## Consequences

- One tool for emulators, simulators and real devices on both platforms, scripted in the same language as the UI.
- iOS sessions are slow the first time (WebDriverAgent build); CI allows 45 minutes.
- Local Android runs need an emulator; on the maintainer's machine the virtual device lives on `D:\Android\avd` because `C:` is nearly full (see `apps/mobile/e2e/README.md`).
- Desktop E2E (Electron) stays with Playwright (T1.5).

## Sources

- Maestro documentation, supported platforms: <https://docs.maestro.dev/platform-support/supported-platforms>.
- Appium: <https://appium.io>; drivers on npm (`appium-uiautomator2-driver` 8.7.0, `appium-xcuitest-driver` 12.15.2).
- WebdriverIO 10: <https://webdriver.io>.
- `capacitor-testing` skill (Appium section).
