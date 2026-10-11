# apps/mobile — Capacitor shell (Android and iOS)

Mobile application: the UI from [`ui/`](../../ui/) inside Capacitor, plus a native plugin embedding the Go core. The native shells stay thin: Kotlin for Android, Swift for iOS.

**Status: T1.3 done for Android.** The app embeds the Go core (the shared library of `core/ffi`, called through JNI) in a foreground service and gives the web view access to it through a Capacitor plugin ([ADR 0011](../../docs/ADR/0011-core-on-android.md)). Since T1.6 the core can run Telegram on the device when the app is built with Telegram's application credentials ([ADR 0014](../../docs/ADR/0014-telegram-on-device.md)). Since T2.5 the web view shows the real interface of `ui/`, with a one-pane layout on phones ([ADR 0021](../../docs/ADR/0021-unified-inbox.md)). iOS: T1.7 (extension memory budget) then E3.3. See [`docs/TASKS.md`](../../docs/TASKS.md).

## Layout

```
apps/mobile/
  capacitor.config.json  Capacitor configuration (bridge logging off: it would log message content)
  copy-ui.mjs            puts the build of ui/ and the pages of web/ into www/ (npm run web)
  web/       pages only the app needs: webview-too-old.html (Capacitor's errorPath)
  www/       generated (git-ignored): what the web view loads
  android/   Gradle project (Capacitor 8 template), builds the Go core with the NDK
    app/src/main/java/app/musubee/
      MainActivity.kt             registers the plugin; Back goes back in the interface first
      core/CoreLibrary.kt         Kotlin side of the JNI binding (core/ffi/jni_android.go)
      core/CoreService.kt         foreground service (type specialUse) that owns the core
      core/CoreKey.kt             the core's master key, wrapped by the Android Keystore (ADR 0019)
      core/MusubeeCorePlugin.kt   Capacitor plugin "MusubeeCore": call({request}), "event" listener
    app/src/androidTest/          instrumented tests (core, service, measurements, soak)
    measure-core.sh               measurement runs of ADR 0011
  e2e/       Appium + WebdriverIO end-to-end tests (ADR 0007)
  ios/       planned: Xcode project, Capacitor plugin + Notification Service Extension (NSE) in Swift
```

## Android

### Requirements

- Go 1.27.1 or later in `PATH` (or `-Pmusubee.go=<path to go>`).
- Node 24 and npm 11.
- JDK 21 (`JAVA_HOME`).
- Android SDK (`ANDROID_HOME`, or `sdk.dir` in `android/local.properties`, which Git ignores) with platform 36 and **NDK 28.2.13676358**: `sdkmanager "ndk;28.2.13676358"`.

Gradle builds the core itself: one `go build -buildmode=c-shared ./core/ffi` per ABI with the NDK's clang (tasks `buildCore-<abi>`). ABIs: `arm64-v8a`, `x86_64`, `x86`; `-Pmusubee.abis=x86_64` builds fewer. On Android the core uses the C build of SQLite (`mattn/go-sqlite3`), compiled by the same clang: the pure-Go one is killed by Android's seccomp filter on x86_64 (ADR 0011 point 7).

### Build and test

From `apps/mobile`:

```
(cd ../../ui && npm ci && npm run build)
npm ci
npm run sync
```

`npm run sync` copies the interface's build (`ui/dist`) and `web/` into `www/` (`npm run web`), then runs `cap sync android`, which copies `www/` and the configuration into the Android project and generates the Cordova plugin project; run it again after changing the interface or the configuration. Then, from `apps/mobile/android` (Windows: `gradlew.bat`):

```
./gradlew :app:assembleDebug
ANDROID_SERIAL=emulator-5554 ./gradlew :app:connectedDebugAndroidTest
```

`ANDROID_SERIAL` makes Gradle use that device only: never run the tests on a personal phone. The app end-to-end test then installs the debug APK and drives the interface: it adds an echo account, opens a conversation of the inbox, sends a message, waits for its echo and goes back to the inbox (from `apps/mobile/e2e`):

```
MUSUBEE_ANDROID_UDID=emulator-5554 npm run test:android:app
```

### System web view

The interface is built for Chrome 111 and later (ADR 0020), and the web view is the device's Android System WebView, which updates from the Play Store. `android.minWebViewVersion` in `capacitor.config.json` is therefore 111: on an older web view, Capacitor loads `web/webview-too-old.html`, which asks to update it, instead of a blank page. The API 30 emulator images ship WebView 83: there, the app end-to-end test only checks that page; the API 35 images run the whole journey.

### Telegram

The app offers Telegram only when it is built with Telegram's application credentials (https://my.telegram.org), which never go into the repository: either the environment variables `MUSUBEE_TG_API_ID` and `MUSUBEE_TG_API_HASH`, or the Gradle properties `musubee.telegramApiId` and `musubee.telegramApiHash` (for example in `~/.gradle/gradle.properties`). Gradle checks them and puts them in `BuildConfig`; the core service passes them to the core. The interface then offers Telegram in "Add an account": a phone number (then the code Telegram sends, and the password if the account has one), or a bot token. The QR code flow is listed too, but the interface shows its data as text and does not draw the code yet.

The instrumented end-to-end test logs in as the bridge test bot of ADR 0006 and exchanges a message with the peer bot (verified in T1.6 on CI's API 35 emulator):

```
ANDROID_SERIAL=emulator-5554 ./gradlew :app:connectedDebugAndroidTest   -Pandroid.testInstrumentationRunnerArguments.class=app.musubee.core.TelegramTest   -Pandroid.testInstrumentationRunnerArguments.musubeeTgBridgeBotToken=...   -Pandroid.testInstrumentationRunnerArguments.musubeeTgPeerBotToken=...   -Pandroid.testInstrumentationRunnerArguments.musubeeTgChatId=...
```

Without them it is skipped (`musubeeTgRequire=1` makes it fail instead, as in CI). It logs timings and memory under the tag `MusubeeTelegramTest`, never the tokens nor the messages.

### Signal

The app offers Signal only when it is built with it (ADR 0029), and should not reach users with it yet: ADR 0025 leaves an open point before any app ships Signal. Prepare libsignal-ffi once per ABI with the build script of ADR 0028 (Rust, cmake, protoc and the NDK; about 5 minutes per ABI, more on Windows), into one directory per ABI:

```
ANDROID_NDK_HOME=<sdk>/ndk/28.2.13676358 scripts/build-libsignal.sh <dir>/x86_64 x86_64
```

On Windows, run it in Git Bash with MSYS2 UCRT64 first in `PATH`, as for the desktop build (`CLAUDE.md` section 8). Then pass the service and the directory to every Gradle command, for example:

```
./gradlew -Pmusubee.abis=x86_64 -Pmusubee.signal=staging -Pmusubee.libsignal=<dir> :app:assembleDebug
```

`musubee.signal` is `staging` (Signal's test service, which CI uses) or `production`. Each selected ABI needs its `libsignal_ffi.a`; the build stops with the command to run when one is missing. `BuildConfig.SIGNAL_ENVIRONMENT` holds the service, and the core service adds `"signal"` to the core's configuration. The interface then offers Signal's QR code login; it shows the code as text, and linking from the phone that runs Signal is an open question (ADR 0029).

The instrumented test `SignalTest` checks that the core offers Signal and that the QR login returns a linking code from Signal's service, then cancels it; it needs network access, no phone. It is skipped in builds without Signal (`musubeeSignalRequire=1` makes it fail instead, as in CI):

```
ANDROID_SERIAL=emulator-5554 ./gradlew -Pmusubee.abis=x86_64 -Pmusubee.signal=staging -Pmusubee.libsignal=<dir> :app:connectedDebugAndroidTest -Pandroid.testInstrumentationRunnerArguments.class=app.musubee.core.SignalTest
```

### Measurements

- `ANDROID_SERIAL=emulator-5554 ./measure-core.sh 5` runs `CoreLibraryTest#measurements` 5 times in fresh processes and prints one JSON line per run: call cost, round trip, memory.
- The soak test runs the service for N minutes, one message every 30 seconds, and logs one sample of memory and CPU time per minute under the tag `MusubeeSoak`:

  ```
  ./gradlew :app:installDebug :app:installDebugAndroidTest
  adb shell am instrument -w -e musubee.soak.minutes 60 -e class app.musubee.core.SoakTest app.musubee.test/androidx.test.runner.AndroidJUnitRunner
  adb logcat -s MusubeeSoak:I
  ```

  Without `musubee.soak.minutes` the test is skipped.

### Rules for this app

- Never log message content: no `console` output in the interface (Capacitor forwards it to logcat), no message text in `Log` calls.
- The service starts from the activity: Android only allows starting a foreground service from the foreground.
- App data is excluded from backups (`allowBackup=false`, `data_extraction_rules.xml`).
- The core's master key (ADR 0019) lives in `noBackupFilesDir/core-key`, wrapped by the Android Keystore key `app.musubee.core.key-wrap`; `CoreService` gives it to the core as `database_key`. Never log the core's configuration: it holds the key. Uninstalling the app removes both the key and the database; losing only the Keystore key makes the core refuse to open ("written with another key").

## Points of attention

- **iOS starts in "hosted bridge" mode.** The Notification Service Extension (NSE) is a separate process with very limited memory; a full Go runtime there is uncertain (measured in T1.7).
- **AGPL / App Store compatibility is unresolved**: to be validated by a lawyer before any iOS release.
- iOS builds require a Mac; push and the NSE on a real device require a paid Apple Developer account.

## Skills

`capacitor-*`, `debugging-capacitor`, `ios-android-logs`, `safe-area-handling`; iOS: `swift-concurrency`, `swift-testing`, `background-processing`, `push-notifications`, `debugging-instruments`, `ios-memgraph-analysis`, `app-store-review`; Android: `claude-android-ninja` (services, notifications, Gradle only).

## Headers

<!-- REUSE-IgnoreStart -->

Kotlin and Swift: `// SPDX-FileCopyrightText: …` and `// SPDX-License-Identifier: AGPL-3.0-or-later` at the top of every file; XML: in a `<!-- … -->` comment after the XML declaration.

<!-- REUSE-IgnoreEnd -->
