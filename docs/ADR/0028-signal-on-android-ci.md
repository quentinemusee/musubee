# ADR 0028 — Signal on Android in CI: libsignal built per ABI with the app's NDK, the Signal tests run on an emulator

- **Status**: proposed
- **Date**: 2026-10-11
- **Task**: T2.11
- **Deciders**: Claude Code (design assistant), under the maintainer's standing autonomy instruction; to be reviewed by the maintainer

## Context

ADR 0022 built libsignal's C library (`libsignal-ffi`) for the four Android ABIs in a one-off spike workflow, and linked libsignalgo's Go tests for them, without running them: the spike had no device. ADR 0025 put mautrix-signal's connector in the core behind the `musubee_signal` build tag, and its CI job builds libsignal and runs the Signal tests on Linux, Windows and macOS only. ADR 0026 (T2.9) then sealed signalmeow's tables. Both ADRs left "libsignal for Android on CI" before the connector can go into the Android app, and ADR 0025 left the size Signal adds to the Android core unknown.

What was missing:

- a repeatable build of libsignal-ffi for Android, outside the spike's workflow, that CI and the app's build can share;
- the Signal code (libsignalgo, signalmeow's sealed store, the core's connector and its sealer) tested on Android, where cgo, the NDK's C library and the SQLite driver (`mattn/go-sqlite3`, ADR 0011) differ from the desktop;
- a check that every binary links its C++ runtime statically: BoringSSL, inside libsignal, needs one, and an Android device has no `libc++_shared.so` unless the app ships it.

## Options considered

### Option A — Run the Go test binaries on an emulator with adb
`go test -c` with `GOOS=android` and the NDK's clang builds one executable per package; `adb push` copies it to `/data/local/tmp` and `adb shell` runs it as the shell user, whose exit status adb returns (Android 7 and later).
- Pros: the tests run unchanged, in the same process model as on the desktop; no Java code; the same script works on a phone connected to a developer's machine.
- Cons: the shell user is not the app's user (its SELinux domain and its data directory differ); x86_64 only on CI.

### Option B — Run them inside the app, as instrumented tests
Go tests cannot run from JUnit as they are; it would mean exporting test entry points through JNI, or rewriting the tests in Kotlin against the core API.
- Pros: the app's own process and sandbox.
- Cons: much more code for the same coverage; the core's existing instrumented tests (`CoreTest`, `CoreKeyTest`) already run the core inside the app, and the connector will join them when it goes into the app.

### Option C — Link only, as in the spike
- Pros: no emulator, short job.
- Cons: says nothing about whether the code works on Android, which is what the task needs before the app ships Signal.

## Decision

Option A. `scripts/build-libsignal.sh` takes an optional Android ABI and builds libsignal-ffi with the environment that ADR 0022 verified for the four ABIs (the NDK's clang for the app's `minSdkVersion` 24, the size-optimised release profile, BoringSSL's C++ runtime linked statically, the `aes_armv8` and `curve25519_dalek_bits="64"` cfgs). For Android it also writes a `libc++.a` linker script, `INPUT(-lc++_static -lc++abi)`, next to the library: libsignalgo links with `-lstdc++`, which the NDK's clang turns into `-lc++`, which the NDK resolves to `libc++_shared.so`, and `-static-libstdc++` does not change that in a C link (the first CI run of this task found every Signal binary needing `libc++_shared.so`). Found first through `-L`, the script links the C++ runtime statically instead; the core is the app's only native library using C++, the case where the NDK allows a static runtime. `scripts/run-android-go-tests.sh` pushes test binaries to a device and runs them.

A `signal-android` job in `checks.yml`, for x86_64 and arm64-v8a, builds and caches libsignal per ABI and per version of the build script, builds the tests of the mautrix-signal copy and of the core's Signal code (`core/connector/signal`, `core/embedded` with the tag) for Android, builds the core's shared library as the app does with and without Signal, fails if a binary needs `libc++_shared.so`, and runs the tests on an x86_64 emulator (API 30). arm64-v8a, the phones' ABI, is built and linked only: GitHub's Linux runners cannot emulate ARM at a usable speed.

Android builds of libsignal need a Linux or macOS host: BoringSSL's CMake build with the NDK's Windows toolchain is untested, and the maintainer's machine builds Android without Signal until a task needs otherwise.

## Measurements

To fill from the first green run of the `signal-android` job.

| | x86_64 | arm64-v8a |
|---|---|---|
| libsignal-ffi build (cold cache) | … | … |
| Core `libmusubee.so`, stripped, without Signal | … | … |
| Core `libmusubee.so`, stripped, with Signal | … | … |
| Tests run on the emulator | … | linked only |

## State of knowledge

| Claim | Status | Source |
|---|---|---|
| libsignal-ffi builds for x86_64 and arm64-v8a with `build-libsignal.sh` | **unknown** until CI runs | run … |
| The copy's tests and the core's Signal tests pass on an Android 11 x86_64 emulator | **unknown** until CI runs | run … |
| No binary needs `libc++_shared.so` | **unknown** until CI runs | run … |
| The same tests pass on an arm64 phone | **unknown**: the script runs there, not tried (the maintainer's Pixel 8 Pro, with their consent) | — |
| armeabi-v7a and x86 build with the script | **assumed**: ADR 0022 built them with the same settings; CI does not build them | ADR 0022 |
| The tests behave the same as the app's user | **assumed**: they run as the shell user; the connector's instrumented test comes with the connector in the app | — |

## Consequences

- The Android app's build can take libsignal from this script when the connector goes into the app: Gradle will need the library per ABI and `-tags=goolm,musubee_signal`; on Windows hosts, from a Linux or macOS build (CI artifact or WSL), or after the Windows host is made to work.
- Changing the build script rebuilds libsignal on every target, since the cache key is the script's hash: about 5 minutes per Android ABI and 12 on Windows.
- Revisit if GitHub offers ARM Linux runners with nested virtualization (then run the arm64 tests), or if the app's `minSdkVersion` changes.

## Sources

- ADR 0011 (the core on Android), ADR 0022 (spike, the Android build environment), ADR 0025, ADR 0026.
- `adb shell` returning the remote exit status: checked at each run by `run-android-go-tests.sh` (`adb shell false` must fail) rather than taken from documentation.
- `go help test`, Go 1.27.1: with `-o` naming a directory, each test binary is written to `pkg.test` there.
