# ADR 0029 — Signal in the Android app: an opt-in Gradle property, libsignal prepared per ABI

- **Status**: accepted
- **Date**: 2026-10-11
- **Task**: T2.12
- **Deciders**: Claude Code (design assistant), under the maintainer's standing autonomy instruction; to be reviewed by the maintainer

## Context

ADR 0025 put mautrix-signal's connector in the core behind the `musubee_signal` build tag; ADR 0026 sealed signalmeow's tables; ADR 0028 built libsignal-ffi for Android with `scripts/build-libsignal.sh DIR ABI` and ran the Signal tests on an emulator, outside the app. The Android app still built its core with `-tags=goolm` only (`apps/mobile/android/app/build.gradle`), so it never offered Signal.

Two constraints shape how Signal goes in:

- **Signal must not reach users yet.** ADR 0025 left an open point about sender certificates, reported privately upstream; ADR 0026 and the task list keep it as a condition before any app ships Signal. Signal also adds about 20 MB to the core (ADR 0028).
- **libsignal is slow to build and needs a Rust toolchain**: 4 to 6 minutes per ABI on CI, 5 to 13 on the maintainer's machine. Every other part of the core's Android build needs only Go and the NDK, and Gradle builds it with nothing prepared beforehand (ADR 0011).

## Options considered

### Option A — A Gradle property, off by default, and libsignal prepared beforehand
`-Pmusubee.signal=staging|production` turns Signal on; `-Pmusubee.libsignal=DIR` points to libsignal built by `scripts/build-libsignal.sh DIR/<abi> <abi>`.
- Pros: builds without the property are unchanged; the expensive build stays a separate, cached step (on CI the cache of ADR 0028's job); one place sets the Signal service.
- Cons: a developer prepares libsignal once per ABI; nothing stops a release build with the property (E3.x will gate release builds).

### Option B — Gradle builds libsignal itself
A task per ABI runs the build script.
- Pros: "gradlew assemble" stays self-contained.
- Cons: the Android build would need bash, Rust, cmake and protoc everywhere, and a clean build would take up to 40 minutes for three ABIs; Gradle's up-to-date checks over a Cargo build are hard to get right.

### Option C — A product flavor ("signal")
- Pros: both variants side by side in one build.
- Cons: doubles every task and test command for a switch that has one boolean effect on the native build and the configuration; a property is enough until release builds exist.

### Option D — Signal always on
- Rejected: it would ship Signal before the condition above is met, and add 20 MB to every build.

## Decision

**Option A.**

- **Gradle** (`apps/mobile/android/app/build.gradle`): `musubee.signal` accepts `staging` or `production` and requires `musubee.libsignal`. With it, each `buildCore-<abi>` task builds with `-tags=goolm,musubee_signal` and `CGO_LDFLAGS=-L<libsignal>/<abi>`, declares that directory as an input, and fails early, with the command to run, when `libsignal_ffi.a` is missing there. The `libc++.a` linker script of ADR 0028 in the same directory makes the C++ runtime static. `BuildConfig.SIGNAL_ENVIRONMENT` carries the service, empty without Signal.
- **The core's configuration** (`CoreService.coreConfig`): `"signal": {"environment": ...}` when `SIGNAL_ENVIRONMENT` is set. The core then offers Signal's QR code login (ADR 0025).
- **Windows hosts**: Gradle gives Go `"<ndk>/clang.exe" --target=<triple><minSdk>` as its C compiler, for every build, instead of the NDK's `.cmd` wrapper, whose command line overflows cmd.exe's limit when the core links libsignal (ADR 0028). Go splits `CC` on spaces and honours quotes. Linux and macOS keep the target's clang script.
- **Test**: the instrumented test `SignalTest` starts the core service, checks that Signal is offered with its one flow, starts the QR code login, which opens a provisioning connection to Signal's service and generates its keys with libsignal, checks that a `sgnl://linkdevice` code comes back, cancels, and checks that no Signal account was left. It needs no phone. It is skipped in builds without Signal, unless `musubeeSignalRequire=1`. It never logs the code.
- **CI**: the `android` job (API 30 and 35, x86_64) restores the x86_64 libsignal of the `signal-android` job (same cache key; it builds it when the cache misses), builds the app with `-Pmusubee.signal=staging`, and runs every instrumented test with `musubeeSignalRequire=1`, then the Appium journey, on that build. Staging, because CI has no Signal account and must never touch production accounts.

## Measurements (Windows host, NDK 28.2.13676358, Go 1.27.1)

| | Without Signal | With Signal |
|---|---|---|
| Core, x86 (stripped) | 54.6 MB | 74.6 MB |
| Core, x86_64 (stripped) | | 79.2 MB (ADR 0028 measured 79.3 MB on Linux) |
| Debug APK, x86_64 | | 83.5 MB |
| Shared libraries the x86_64 core needs | | `liblog`, `libdl`, `libm`, `libz`, `libc`: no `libc++_shared.so` |
| `login.start` to the linking code, emulator API 30 (x86) against staging | | 339 and 368 ms (two runs) |

The x86 libsignal build exposed a bug of the build script on Windows hosts: `cygpath -m` appends `.exe` to a program that exists, which made the archiver `llvm-ar.exe.exe`. It was fixed in T2.11's branch before this task.

## State of knowledge

| Claim | Status | Source |
|---|---|---|
| A build without the property is unchanged, and `SignalTest` is skipped there | **verified** (Windows host, emulator API 30 x86: 13 tests, 0 failures, 3 skipped by assumption) | `connectedDebugAndroidTest` |
| A build with `-Pmusubee.signal=staging` offers Signal, and its QR login reaches staging and returns a linking code | **verified** (same emulator: 13 tests, 0 failures, 2 skipped) | `SignalTest` |
| The same on API 30 and 35 x86_64 on CI, with the Appium journey on the Signal build | **verified** (run 38180342503: 13 tests each with `musubeeSignalRequire=1`, libsignal from the cache of the `signal-android` job, no rebuild between assembling and testing) | `.github/workflows/checks.yml`, job `android` |
| arm64-v8a: the app with Signal on a phone | **unknown** (needs the maintainer's consent to use the Pixel 8 Pro) | |
| Linking an account from the app | **unknown**, and probably not possible as is: see below | |
| Memory and battery with a Signal account connected | **unknown** | |

**Linking from a phone is an open problem.** Signal links a device when the primary phone's Signal app scans the code that the new device shows. The interface shows the code as text (`AddAccount.tsx`), not as a QR image; and even drawn, a code on the screen of the phone that runs Signal cannot be scanned by that same phone. Whether Signal's app can link from a code given another way (an image, a link) is unknown. The answer belongs to the onboarding work (E2.2): drawing the code is needed in any case, for the desktop and for a second phone.

## Consequences

- Developers who want Signal in the Android app prepare libsignal once per ABI and pass two properties; the README explains it. Everyone else is unaffected.
- CI's Android app job now also depends on Signal's staging service being reachable: an outage there fails the job. The Telegram journeys already depend on Telegram the same way.
- Every Windows build of the Android core uses `clang.exe --target`, with or without Signal.
- Follow-ups: the sender certificate outcome before any build with Signal reaches users; release builds that refuse `musubee.signal` until then (E3.x); drawing QR codes and the same-phone linking question (E2.2); arm64 on a phone; armeabi-v7a, which the app does not build yet.

## Sources

- ADR 0011 (core on Android), ADR 0025 (Signal on the device), ADR 0026 (Signal's secrets at rest), ADR 0028 (Signal on Android in CI).
- Go 1.27.1, `src/cmd/go/internal/work/exec.go`, `envList`: the `CC` variable is split into a command and its arguments with `quoted.Split`, which honours quotes (read 2026-10-11).
- Signal's linking flow as implemented by signalmeow's provisioning code in `core/replace/mautrix-signal/pkg/signalmeow` (the `sgnl://linkdevice` URI carries the provisioning address and a public key).
