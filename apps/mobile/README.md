# apps/mobile — Capacitor shell (Android and iOS)

Mobile application: the UI from [`ui/`](../../ui/) inside Capacitor, plus a native plugin embedding the Go core. The native shells stay thin: Kotlin for Android, Swift for iOS.

**Status: T1.3 done for Android.** The app embeds the Go core (the shared library of `core/ffi`, called through JNI) in a foreground service and gives a test web page access to it through a Capacitor plugin ([ADR 0011](../../docs/ADR/0011-core-on-android.md)). No real UI yet. iOS: T1.7 (extension memory budget) then E3.3. See [`docs/TASKS.md`](../../docs/TASKS.md).

## Layout

```
apps/mobile/
  capacitor.config.json  Capacitor configuration (bridge logging off: it would log message content)
  www/       test page of the core plugin (replaced by the UI from ui/ later)
  android/   Gradle project (Capacitor 8 template), builds the Go core with the NDK
    app/src/main/java/app/musubee/
      MainActivity.kt             registers the plugin
      core/CoreLibrary.kt         Kotlin side of the JNI binding (core/ffi/jni_android.go)
      core/CoreService.kt         foreground service (type specialUse) that owns the core
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

Gradle builds the core itself: one `go build -buildmode=c-shared ./core/ffi` per ABI with the NDK's clang (tasks `buildCore-<abi>`). ABIs: `arm64-v8a`, `x86_64`, `x86`; `-Pmusubee.abis=x86_64` builds fewer.

### Build and test

From `apps/mobile`:

```
npm ci
npx cap sync android
```

`cap sync` copies `www/` and the configuration into the Android project, and generates the Cordova plugin project; run it again after changing them. Then, from `apps/mobile/android` (Windows: `gradlew.bat`):

```
./gradlew :app:assembleDebug
ANDROID_SERIAL=emulator-5554 ./gradlew :app:connectedDebugAndroidTest
```

`ANDROID_SERIAL` makes Gradle use that device only: never run the tests on a personal phone. The app end-to-end test then installs the debug APK and drives the test page (from `apps/mobile/e2e`):

```
MUSUBEE_ANDROID_UDID=emulator-5554 npm run test:android:app
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

- Never log message content: no `console` output in the web page (Capacitor forwards it to logcat), no message text in `Log` calls.
- The service starts from the activity: Android only allows starting a foreground service from the foreground.
- App data is excluded from backups (`allowBackup=false`, `data_extraction_rules.xml`).

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
