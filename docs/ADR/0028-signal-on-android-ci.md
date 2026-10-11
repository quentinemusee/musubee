# ADR 0028 — Signal on Android in CI: libsignal built per ABI with the app's NDK, the Signal tests run on an emulator

- **Status**: accepted
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

The script also builds for Android on a Windows host (MSYS2 UCRT64, Rust's GNU host toolchain, as for the desktop build): Rust and CMake get Windows paths and the NDK's `.cmd` wrapper of clang, and the Windows-only steps of the desktop build (the system libraries in `cgo-ldflags`, the `libwindows` import library) are skipped. Go, though, must be given `clang.exe --target=<triple><API>` rather than the `.cmd` wrapper: a link with libsignal passes a command line longer than cmd.exe accepts ("The command line is too long").

## Measurements

GitHub's `ubuntu-latest` runners, run 38169205986 (2026-10-11): libsignal-ffi v0.102.2 (commit `8fc2113`), Rust 1.98.1, Go 1.27.1, NDK 28.2.13676358, API 24. Core sizes: `go build -buildmode=c-shared -trimpath -ldflags="-s -w" ./core/ffi`, as the app's Gradle build does; the build without Signal already holds Telegram.

| | x86_64 | arm64-v8a |
|---|---|---|
| libsignal-ffi build, cold cache | 249 s | 286 s |
| Core `libmusubee.so`, stripped, without Signal | 58.4 MB | 55.2 MB |
| Core `libmusubee.so`, stripped, with Signal | 79.3 MB (+20.9 MB) | 74.5 MB (+19.3 MB) |
| Libraries the binaries need | `libc`, `libdl`, `liblog`, `libm`, `libz` | the same |
| Tests | 7 binaries, 107 tests and subtests run on the emulator (API 30): all pass but one that upstream skips ("This test is broken") | built and linked only |

On the maintainer's Windows machine (MSYS2 UCRT64, the same versions): the x86_64 build took 635 s, and the test binaries of `pkg/libsignalgo`, `pkg/signalmeow/store` and `core/connector/signal` linked with `clang.exe --target=x86_64-linux-android24`, needing the same five libraries. They were not run there: the local emulator image is 32-bit x86.

## State of knowledge

| Claim | Status | Source |
|---|---|---|
| libsignal-ffi builds for x86_64 and arm64-v8a with `build-libsignal.sh` on Linux | **verified** (CI) | run 38169205986 |
| It builds for x86_64 on a Windows host, and Signal's test binaries link with it | **verified** (run on the maintainer's machine, 2026-10-11) | the commands of `CLAUDE.md` §9 |
| The copy's tests (`libsignalgo`, `msgconv`, `signalmeow`, its sealed store) and the core's Signal tests (`core/connector/signal`, `core/embedded` with the tag) pass on an Android 11 x86_64 emulator | **verified** (CI) | run 38169205986 |
| No binary needs `libc++_shared.so` once the `libc++.a` script is in place; without it, every binary linking libsignal did | **verified** (CI, before and after; and locally with the NDK's clang) | runs 38168466838 and 38169205986 |
| `run-android-go-tests.sh` reports a failing binary as a failure | **verified** (on the local emulator: binaries of the wrong ABI failed the run; and `adb shell false` is checked at each run) | 2026-10-11 |
| Signal adds about 20 MB to the Android core (stripped) | **verified** (CI) | run 38169205986 |
| The same tests pass on an arm64 phone | **unknown**: the script runs there, not tried (the maintainer's Pixel 8 Pro, with their consent) | — |
| armeabi-v7a and x86 build with the script | **assumed**: ADR 0022 built them with the same settings; CI does not build them | ADR 0022 |
| The tests behave the same as the app's user | **assumed**: they run as the shell user; the connector's instrumented test comes with the connector in the app | — |

## Consequences

- The Android app's build can take libsignal from this script when the connector goes into the app: Gradle will need the library per ABI and `-tags=goolm,musubee_signal`; on a Windows host, Gradle must pass Go `clang.exe --target=...` instead of the `.cmd` wrapper it uses today, or the link fails on cmd.exe's command line limit.
- Changing the build script rebuilds libsignal on every target, since the cache key is the script's hash: about 5 minutes per Android ABI and 12 on Windows.
- Revisit if GitHub offers ARM Linux runners with nested virtualization (then run the arm64 tests), or if the app's `minSdkVersion` changes.

## Sources

- ADR 0011 (the core on Android), ADR 0022 (spike, the Android build environment), ADR 0025, ADR 0026.
- `adb shell` returning the remote exit status: checked at each run by `run-android-go-tests.sh` (`adb shell false` must fail) rather than taken from documentation.
- `go help test`, Go 1.27.1: with `-o` naming a directory, each test binary is written to `pkg.test` there.
