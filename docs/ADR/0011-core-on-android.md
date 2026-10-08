# ADR 0011 — The core on Android: JNI in the shared library, in a foreground service

- **Status**: accepted
- **Date**: 2026-10-08
- **Task**: T1.3
- **Deciders**: Quentin Raimbaud (maintainer), with the design assistant

## Context

On Android the Go core runs inside the app, as a library loaded by a Capacitor plugin (`CLAUDE.md` §2). T1.2 built it as a C shared library for the desktop (ADR 0010). T1.3 decides how the Android app loads it and calls it, and how the core keeps running while no activity is visible.

The task asks to compare two ways of building the core for Android:

- the shared library of ADR 0010, called through JNI;
- `gomobile bind`, the Go team's generator of Java bindings.

How the UI talks to the core (direct FFI or a local socket) is T1.4. T1.3 only needs a binding to measure it and an end-to-end path from the web view to the core.

Constraints:

- **Licenses.** Capacitor is MIT and `golang.org/x/mobile` is BSD-3-Clause, both compatible with AGPL-3.0-or-later.
- **Platform levels.** Capacitor 8 requires `minSdk` 24 and `targetSdk` 36.
- **Background limits.** A messenger must stay connected to its networks in the background. Android stops background work unless a foreground service with a visible notification runs it. From Android 14, every foreground service declares a type and the matching permission.

## Options considered

### Binding

#### A. Shared library with JNI entry points (chosen)

`core/ffi/jni_android.go`, built only for Android, adds four JNI functions to the library of ADR 0010: `Java_app_musubee_core_NativeCore_open`, `call`, `nextEvent` and `close`. The library still exports the five C functions of `musubee.h`, so there is one `libmusubee.so` per ABI.

- The JNI functions and the C exports share the same handle map (`core/ffi/ffi.go`).
- They follow the same safety rules:
  - each export recovers Go panics;
  - a stale handle raises an `IllegalStateException` instead of crashing;
  - inputs are copied, so no pointer to Java memory is kept.
- The Gradle build runs one `go build -buildmode=c-shared` per ABI with the NDK's clang (`apps/mobile/android/app/build.gradle`).
- On the Kotlin side, `app.musubee.core.CoreLibrary` is 46 lines.

Pros:

- One C surface and one set of safety rules for every platform. The desktop, Android and later iOS load the same package (`core/ffi`).
- No code generator and no extra tool in the build: Go and the NDK only.
- Each ABI is a separate Gradle task, so Gradle can skip it when nothing changed or build only the ABI it needs (`-Pmusubee.abis`).

Cons:

- 130 lines of hand-written JNI glue (C helpers and Go exports), covered by the instrumented tests below.
- JNI exceptions and local references are ours to handle.

#### B. `gomobile bind`

`core/mobile` exposes the same four operations as a Go type, `Core`, with methods `Call`, `NextEvent` and `Close`. `gomobile bind -javapkg=app.musubee.gomobile` generates the Java classes (`Mobile`, `Core`) and an AAR with one `libgojni.so` per ABI.

The tool versions were pinned through the `tool` directives of `core/go.mod`. They added `golang.org/x/mobile` and, indirectly, `x/mod`, `x/sync` and `x/tools` to the module graph.

Pros:

- No hand-written JNI.
- The same command can produce an XCFramework for iOS.

Cons:

- `golang.org/x/mobile` has **no tagged release**: we would pin a pseudo-version (the latest is `v0.0.0-20260908204917-8b95e45f8d3e`, checked on 2026-10-08 with `go list -m golang.org/x/mobile@latest`).
- A second build tool and a second interface next to `musubee.h`, with their own rules (allowed types, generated names).
- All ABIs are rebuilt together, and the build installs and runs `gomobile` then `gobind`: slower (Measurements).

Both options were implemented side by side, as two Gradle product flavors (`jni` and `gomobile`) running the same instrumented tests. The losing flavor was removed afterwards. The comparison code, including `core/mobile` and the measurement script `measure-bindings.sh`, stays in the history: commit "feat(mobile): Android app with the core in a foreground service".

### Keeping the core alive: foreground service type

The core runs in `CoreService`, a foreground service that the Capacitor plugin starts and binds. Its notification is low-importance and says "Connected to your accounts". Since Android 14, the service needs a type:

- **`dataSync`**: for apps targeting Android 15 or later, the system limits `dataSync` (and `mediaProcessing`) services to 6 hours in 24 while the app is in the background, then calls `Service.onTimeout`. A messenger connected all day does not fit.
- **`remoteMessaging`**: the documentation describes it as transferring text messages from one device to another, for continuity when the user switches devices. That is not our use.
- **`specialUse` (chosen)**: "any valid foreground service use cases that aren't covered by the other foreground service types". It requires the `FOREGROUND_SERVICE_SPECIAL_USE` permission and a free-form `PROPERTY_SPECIAL_USE_FGS_SUBTYPE` property explaining the use. Google reviews that explanation, plus a declaration in the Play Console (description, user impact, video), when the app is submitted.
- **No foreground service** (push only): this is the long-term goal for hosted bridges, where a server can wake the app. It is not possible in "on-device" mode, where the device itself holds the connection to the network.

## Decision

1. **Android loads the core as `libmusubee.so`, the shared library of ADR 0010 with JNI entry points** (option A). `gomobile bind` is not used. The two are equivalent at run time (Measurements); A keeps one interface and one build tool for every platform.
2. **ABIs: `arm64-v8a`, `x86_64` and `x86`.**
   - `armeabi-v7a` (32-bit ARM) is left out for now. Each ABI adds about 18 MB to a universal APK, and new devices are 64-bit.
   - Revisit when we publish (Play App Bundles split per ABI), or if a test user has a 32-bit device.
   - `x86` is only for emulators; it can be dropped from release builds.
3. **The core lives in a foreground service of type `specialUse`.** The subtype property explains that the service keeps the user's messaging accounts connected, so that messages arrive while the app is in the background.
   - The service is `START_STICKY` and not exported.
   - The plugin starts it from the activity, since Android only allows starting a foreground service from the foreground.
   - **Play risk:** the reviewer may reject `specialUse`. The fallbacks are push for hosted bridges, or `dataSync` with its 6-hour limit. To revisit before the first Play submission (E3.3).
4. **The web view reaches the core through the `MusubeeCore` Capacitor plugin**:
   - `call({request})` resolves with the JSON response;
   - an `event` listener receives every event;
   - commands run on a thread pool, never on the plugin's thread.
   - The plugin is local to the app (registered in `MainActivity`), not an npm package. T1.4 defines the real contract.
5. **No message content in logs.**
   - Capacitor's bridge logging is turned off (`"loggingBehavior": "none"` in `capacitor.config.json`): by default it would write every plugin result, messages included, to logcat in debug builds.
   - The test page writes nothing to the console.
6. **No backup of the app data** (`allowBackup=false` and data-extraction rules excluding every domain): the core's database holds message history and, later, keys. To revisit together with key storage (OS keystore) and encrypted backups.

## Measurements

**Hardware and versions:**
- Emulator `musubee_api30`: Android 11 (API 30), Google APIs **x86 (32-bit)** image, revision 10, emulator 30.8.4.
- Host: Windows 11, Intel Core i7-9750H, Windows Hypervisor Platform acceleration.
- Real device: Google Pixel 8 Pro (Tensor G3, arm64), Android 17 (API 37): the maintainer's own phone, used with their permission.
- Go 1.27.1, NDK 28.2.13676358, Capacitor 8.5.3, AGP 8.13.0. Date: 2026-10-08.

The binding comparison ran on the emulator; the Pixel then measured the chosen binding.

**Runs:** `apps/mobile/android/measure-bindings.sh 5` (comparison commit) runs the instrumented test `CoreLibraryTest#measurements` 5 times per binding, each run in a fresh process. Its successor for the remaining binding is `measure-core.sh`.

**Test settings:**
- Ping: 1 KiB payload, JSON both ways; 3 loops of 20,000 calls after 1,000 warm-up calls.
- Round trip: send to the "Instant Echo" contact, then read its echo from the event stream; 100 runs after 10 warm-up runs.
- PSS: proportional set size of the whole app process (`Debug.getPss`), so it includes the Java runtime and the web view's libraries.

| Measurement | JNI shared library | gomobile bind |
|---|---|---|
| Ping, mean of the 15 loops (min to max) | 44.3 µs (38.2 to 51.7) | 41.9 µs (37.9 to 46.9) |
| Round trip, mean of the 5 runs (min to max) | 3.89 ms (3.53 to 4.34) | 3.77 ms (3.47 to 4.15) |
| Round trip, slowest of 100, per run | 21 to 83 ms | 23 to 44 ms |
| PSS after `open` | 51.6 to 52.2 MiB | 52.4 to 53.1 MiB |
| PSS at the end (median, max) | 61.9 MiB, 85.9 MiB | 61.9 MiB, 83.6 MiB |
| Go heap in use at the end / reserved | 0.65 to 0.79 MiB / 7.2 MiB | 0.67 to 0.80 MiB / 7.1 MiB |
| Goroutines at the end | 7 | 10 (gomobile's own) |
| Library per ABI, stripped (`-trimpath -ldflags=-s -w`) | arm64-v8a 18.3 MB, x86_64 19.5 MB, x86 17.8 MB | same within 10 KB |
| Debug APK, 3 ABIs | 59.8 MB | 59.8 MB |
| Build of the core for 3 ABIs, empty Go cache | 184 s | 248 s |
| Same, warm Go cache (Gradle `--rerun-tasks`) | 55 s | 95 s |

**Memory reading.** In every run of both bindings, PSS climbs by about 23 MiB during the first ping loop and comes back to within about 2 MiB of its starting point by the third loop. For example, one JNI run: 56.9, 80.6, 66.6 then 58.9 MiB. The Go heap stays under 1 MiB throughout. The swings are the Java heap collecting 20,000 response arrays per loop, not a leak in the binding. One run in five ends higher (76 to 86 MiB) on both bindings, depending on when the Java collector ran.

**On the Pixel 8 Pro** (JNI binding, arm64, `measure-core.sh 5`, phone plugged in, screen on):

| Measurement | Pixel 8 Pro |
|---|---|
| Ping, mean of the 15 loops (min to max) | 45.3 µs (37.6 to 63.0) |
| Round trip, mean of the 5 runs (min to max) | **17.4 ms** (15.0 to 20.3) |
| Round trip, slowest of 100, per run | 51 to 71 ms |
| PSS after `open` | 57.5 to 60.7 MiB |
| PSS at the end | 84.9 to 85.5 MiB |
| Go heap in use at the end / reserved | 1.25 to 1.33 MiB / 10.5 to 10.8 MiB |
| Goroutines at the end | 7 |

Calls cost the same as on the emulator. Round trips, however, are about 4.5 times slower than on the emulator, and the cause is **unknown**:

- fsync is unlikely: the database uses WAL with `synchronous=NORMAL`, so a commit does not sync.
- Candidates are SQLite's busy waits, which back off by 1, 2, 5 then 10 ms (ADR 0010 already saw one writer waiting during each round trip), and the CPU frequency governor on an idle phone.
- 17 ms is not noticeable for a message, but it is a point to investigate with the storage design.

The phone's PSS also ends higher than the emulator's: its Java collector runs less eagerly on a phone with more memory, while the Go heap stays near 1.3 MiB.

**Soak, emulator** (`SoakTest`, one message and its echo every 30 seconds, one sample per minute): 35 minutes and 70 messages. The run stopped there because the session's background task holding the emulator reached its time limit.

| Sample | Start | Minute 10 | Minute 35 |
|---|---|---|---|
| PSS | 53.3 MiB | 54.2 MiB | 55.5 MiB |
| Go heap in use / reserved | 0.65 / 3.2 MiB | 0.63 / 3.2 MiB | 0.63 / 3.2 MiB |
| Goroutines | 9 | 9 | 9 |
| Process CPU time since start | — | — | 0.65 s, i.e. **18.5 ms per minute** |

The Go side does not grow. PSS gains 1.3 MiB between minutes 10 and 35, which is within the noise of the Java heap; a longer run would tell.

**Soak and battery, Pixel 8 Pro** (`SoakTest`, 60 minutes, 2026-10-08 21:16 to 22:16):

- **Setup.** The instrumentation ran without `-w`, so it did not depend on the cable. The phone was on battery for 59 minutes, 34 of them with the screen off.
- **The phone was not idle.** Its owner was on a voice call in another app (Messenger) for about 45 minutes of the hour, and used the phone now and then (screen on for 25 minutes).
- **Two phases.** The test holds no wake lock and sends one message per 30 seconds of *awake* time, so its pace follows the phone's sleep:
  - first 19 minutes: the phone suspended between messages, as it would with the real app. It was awake 331 s, and 12 messages were sent;
  - last 41 minutes: the call kept the phone awake without a break, and 80 messages were sent.

| Sample | Start | 12 min | 30 min | 60 min |
|---|---|---|---|---|
| Messages sent | 0 | 9 | 34 | 92 |
| PSS | 63.7 MiB | 64.3 MiB | 60.5 MiB | 60.1 MiB |
| Go heap in use / reserved | 1.15 / 10.9 MiB | 1.20 / 10.8 MiB | 1.23 / 10.6 MiB | 1.23 / 10.6 MiB |
| Goroutines, native heap | 9, 6.9 MiB | 9, 6.9 MiB | 9, 6.9 MiB | 9, 6.9 MiB |

- **Memory.** It does not grow: PSS stays between 60.1 and 64.6 MiB, and the Go heap between 1.15 and 1.25 MiB.
- **CPU.** The app process, including the test code that drives it, used 8.2 s of CPU over the hour, 89 ms per message.
  - The cost per message is about the same in both phases: 103 ms before the call, 87 ms during it. The call changed how many messages were sent, not what each one cost.
  - That is far more than on the emulator (18.5 ms per minute, about 9 ms per message). How it splits between the core, SQLite and the test driver is **unknown**.
- **Battery.** `dumpsys batterystats` (after `--reset`) estimates **0.109 mAh** for the app over the hour, all CPU, 0.106 mAh of it while in the foreground service.
  - This is a model, not a measurement on the battery rail: Android multiplies each app's own CPU time by the power profile of the cores it ran on.
  - The estimate is per app (per Linux UID), so it does **not** include the call: Messenger's consumption is counted under Messenger's UID.
  - It comes to about 1.2 µAh per message.
- **What the call biases.**
  - The whole phone drained 180 mAh (4,510 mAh battery, 48 % to 43 %), mostly for the call and the screen. Comparing the app with that total is meaningless, so this ADR gives no share of the phone's drain.
  - The call kept the phone awake, so the test sent more messages than an idle phone would have. A rate per hour taken from this run overstates the cost of an idle hour.
  - The cost of the service when nothing happens — a phone asleep, with no messages — was not isolated, and is **unknown**. A run on an idle phone (screen off, no call) would measure it.
  - The test network is local. Real networks add radio wake-ups, which only T1.6 (Telegram on the device) can measure.

**Build times** were measured on the host above with `./gradlew --no-daemon buildCore-arm64-v8a buildCore-x86_64 buildCore-x86 --rerun-tasks` and `bindGomobile --rerun-tasks`, including Gradle's startup. The cold runs used a fresh `GOCACHE`.


## State of knowledge

| Claim | Status | Source |
|---|---|---|
| Both bindings send and receive through the core on the emulator (6 instrumented tests each, including through the foreground service) | **verified** | `connectedJniDebugAndroidTest`, `connectedGomobileDebugAndroidTest` in the comparison commit, 2026-10-08 |
| The two bindings have the same speed and memory within noise | **verified** on the emulator above | Measurements |
| `libmusubee.so` exports the 4 JNI and the 5 C functions | **verified** | `llvm-nm -D` on the android/amd64 build |
| The web page → plugin → service → core path works end to end | **verified** on the emulator | `apps/mobile/e2e/android.app.test.mjs` |
| `dataSync` is limited to 6 h per 24 h when targeting Android 15+ | **verified** (docs) | "Foreground service timeouts", accessed 2026-10-08 |
| `specialUse` needs a subtype property, reviewed with the Play Console declaration | **verified** (docs) | "Foreground service types", Play policy "Understanding foreground service requirements", accessed 2026-10-08 |
| Google Play will accept `specialUse` for a messenger's on-device bridges | **unknown** | only the review will tell (E3.3) |
| The tests and the app end to end pass on a real phone | **verified** on a Pixel 8 Pro, Android 17: 7 instrumented tests (1 skipped: soak) and the Appium test | 2026-10-08 |
| The service runs in the foreground with type `specialUse` on Android 14+ | **verified** on Android 17 (`dumpsys activity services`: `isForeground=true`, `types=0x40000000`) | 2026-10-08 |
| Calls cost the same on a phone; round trips are slower | **verified** (Pixel 8 Pro table) | Measurements |
| Why round trips take 17 ms on the Pixel | **unknown** | to investigate with the storage design |
| Over one hour on a phone, the core's memory does not grow | **verified** for the echo network | Measurements, soak on the Pixel 8 Pro |
| Each message costs the app about 90 ms of CPU and 1.2 µAh on the Pixel | **verified** as Android's model-based estimate, per app, not affected by the call in another app | Measurements, soak on the Pixel 8 Pro |
| The idle cost of the service over an hour on a quiet phone | **unknown**: the soak ran during a 45-minute call in another app | to measure in T1.6, on an idle phone connected to Telegram |
| The battery cost stays small with a real network connection (radio wake-ups) | **unknown** | T1.6 |
| The core survives being killed by the system and restarted by `START_STICKY` | **unknown** | resynchronisation work, see ADR 0010 point 8 |

## Consequences

- `core/ffi` is the only binding package. The `core/mobile` package and the `golang.org/x/mobile` tool dependency are removed.
- Building the Android app needs Go 1.27.1+, the Android SDK, NDK 28.2.13676358 and JDK 21. Gradle builds the core itself, so `gradlew :app:assembleDebug` needs no preparation step besides `npx cap sync android`.
- **Watch:**
  - the 6-hour rule and the type rules change with each Android release;
  - Play's verdict on `specialUse`;
  - the APK size: about 18 MB per ABI, so publish App Bundles.
- **iOS (later):** Swift imports C headers directly, so the same `musubee.h` can serve; whether `c-archive` or `c-shared` fits the app and the NSE is T1.7.
- **Revisit if:**
  - the hand-written JNI grows with the T1.4 contract: a generator may then pay off;
  - `gomobile` gains tagged releases and features we need.

## Sources

Accessed 2026-10-08.

- Android Developers, "Foreground service types": https://developer.android.com/develop/background-work/services/fgs/service-types
- Android Developers, "Foreground service timeouts": https://developer.android.com/develop/background-work/services/fgs/timeout
- Google Play Console Help, "Understanding Google Play's foreground service requirements": https://support.google.com/googleplay/android-developer/answer/13392821
- gomobile command documentation: https://pkg.go.dev/golang.org/x/mobile/cmd/gomobile (module `golang.org/x/mobile` v0.0.0-20260908204917-8b95e45f8d3e, BSD-3-Clause)
- Capacitor 8 Android requirements: `@capacitor/android` 8.5.3 template (`variables.gradle`, `minSdk` 24, `compileSdk`/`targetSdk` 36) and https://capacitorjs.com/docs/android
- Capacitor native bridge (`nativePromise`, `addListener`): `@capacitor/android` 8.5.3, `capacitor/src/main/assets/native-bridge.js`
- JNI specification, "JNI Functions": https://docs.oracle.com/en/java/javase/21/docs/specs/jni/functions.html
- ADR 0010 (C shared library) and ADR 0009 (bridgev2 in-process)
