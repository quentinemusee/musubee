# ADR 0015 — iOS notification extension memory: the core built for iOS, first measurements on the simulator

- **Status**: proposed (part 1 of T1.7; the decision between full Go, lighter crypto and hosted mode waits for the measurement on a real iPhone, part 2)
- **Date**: 2026-10-10
- **Task**: T1.7
- **Deciders**: Quentin Raimbaud (maintainer), with the design assistant

## Context

On iOS, a push notification reaches the app's **Notification Service Extension** (NSE): a separate process that iOS starts for the push and that must hand over the notification's final content within about 30 seconds. For Musubee, the extension is where an encrypted message becomes readable text: it must fetch or receive the event, decrypt it (Megolm, through Olm room keys), and write the notification. iOS kills the extension (jetsam) as soon as its **physical footprint** goes over a limit that Apple does not document. `CLAUDE.md` §3 records what Beeper reported (15 MB, then 50 MB after an exemption obtained through the EU DMA); developers report 24 to 25 MB (see Sources). ADR 0001 rated "a Go runtime fits in the extension" as **unknown, high risk** and gave it to T1.7.

T1.7 asks for: the core (or lighter crypto) built for iOS arm64, integrated into a test NSE; real memory measured on an iPhone; a "feature → peak memory" table; a written decision (full Go / lighter crypto / hosted mode only); the measured threshold as a regression test.

What can be done without a Mac and without an Apple Developer account, on GitHub's macOS runners, is this ADR (part 1). The limit itself can only be read on a device (part 2, a manual step of the maintainer and the iOS test user).

Constraints:

- **License**: everything we ship is AGPL-3.0-or-later; dependencies must be compatible (ADR 0008). No new dependency here: goolm is part of mautrix-go. XcodeGen (MIT) is a build tool of the test app, downloaded in CI, not shipped.
- **One C surface** for every platform (ADR 0010, 0011): iOS uses `core/ffi`'s header approach.
- **No message content in logs** (`CLAUDE.md` §6.8): the probe only handles generated text and logs figures.

## What was built

- `core/memprobe` (Go): runs the steps an extension would run and measures after each one. Steps: `go_runtime` (nothing done yet), `core_open`, `echo_login`, `echo_messages_N` (round trips with the echo network), `core_close`, `olm_account` (a new Olm account with 50 one-time keys), `olm_room_key` (another device sends a Megolm room key over a new Olm session, as an `m.room_key` to-device event), `megolm_decrypt_N` (N messages of 1 KiB), `gc_free` (`debug.FreeOSMemory`). Figures: from the kernel through `task_info(TASK_VM_INFO)`, the process's `phys_footprint` (what jetsam compares with the limit), its peak (`ledger_phys_footprint_peak`) and `limit_bytes_remaining`; from the Go runtime (`runtime/metrics`), mapped memory, heap objects, stacks, goroutines. The `memprobe_nocore` build tag leaves the core out (goolm only).
- `core/cmd/memprobe`: the probe as a program (JSON report on standard output), or as a C static library exporting `musubee_memprobe_run`.
- `apps/mobile/ios-probe`:
  - `build-go-xcframework.sh`, which builds any Go package of the core as an iOS XCFramework (device and simulator slices) and will serve the real app;
  - a test app and its NSE (XcodeGen project), linked with the probe;
  - `control/footprint.c`, an empty C program that prints its own footprint;
  - `run-simulator.sh` and `reports.py`, which run everything on a simulator and build the tables.
- CI job `ios-memory` (`.github/workflows/checks.yml`): runs it all on macOS for both SQLite drivers and publishes the tables in the run summary.

## Options considered

### How to build Go for iOS

#### A. `go build -buildmode=c-archive`, packaged as an XCFramework (chosen)

The core's C exports, built as a static library per SDK (`iphoneos`, `iphonesimulator`) with the SDK's clang, then `xcodebuild -create-xcframework`. Swift imports the header through a Clang module map. This is what `gomobile` does underneath, without its Java/Objective-C binding generator, which we do not need: our boundary is the JSON API of ADR 0012 over the C functions of `musubee.h`.

#### B. `gomobile bind` (T1.7's wording suggested evaluating it)

It generates Objective-C bindings for Go types and an XCFramework. Our API already crosses the boundary as JSON through four C functions (ADR 0010, 0012): generated bindings would add a second, unused surface and a tool to pin. Not chosen; nothing in it is needed that A lacks.

#### C. `-buildmode=c-shared` (a dynamic framework)

Go does not support `c-shared` for `ios/arm64`; c-archive is the only library mode. Not possible.

### Measuring an extension on the simulator

#### A. Push to the extension with `xcrun simctl push` (tried, does not work)

The app asks for provisional authorization (no prompt; granted, `granted=true` in the log), then `simctl push` sends a payload with an alert and `"mutable-content": 1`. **Verified** on the runner's simulator: SpringBoard adds the notification to the list, and no extension process starts (no line from the extension, no crash report). The test app keeps one attempt per run and records the outcome, so that a future Xcode that runs extensions for simulated pushes would show it.

#### B. Real APNs pushes to the simulator

Simulators on Apple silicon Macs can receive real remote notifications, which need an APNs key, so a paid Apple Developer account. Deferred to part 2, with the device.

#### C. Bare processes next to an empty one (chosen for the simulator)

The probe built as an iOS simulator executable and started with `xcrun simctl spawn`, a new process per configuration and repetition (three), next to the empty C program. Without UIKit, it is the closest the simulator can run to an extension, and the difference with the empty program is the cost of Go and of what is linked in. The app runs the probe too, for an order of magnitude with UIKit.

### SQLite on iOS

ADR 0009 chose the pure-Go `modernc.org/sqlite` so that the core builds without cgo; Android moved to `mattn/go-sqlite3` (cgo) because of its seccomp filter (ADR 0011). On iOS, cgo is required anyway (c-archive), so purity buys nothing there. The CI measures both.

## Measurements (simulator)

CI run [38034274220](https://github.com/quentinemusee/musubee/actions/runs/38034274220), 2026-10-10, GitHub's `macos-latest` runner (Apple silicon), iPhone simulator, Go 1.27.1, `GOMAXPROCS` 3. Peak footprint (`ledger_phys_footprint_peak`) of each process, median of three processes, MiB. The previous run ([38032894450](https://github.com/quentinemusee/musubee/actions/runs/38032894450)) gave the same figures within 0.4 MiB.

| Process | Binary | modernc | mattn |
|---|---:|---:|---:|
| Empty C program (control) | 34 KB | 10.0 | 10.0 |
| Probe **without the core** (`memprobe_nocore`), Go runtime started, nothing done | 3.3 MB | 14.1 | 13.9 |
| Probe without the core, goolm: Olm account, room key, 20 Megolm decryptions | 3.3 MB | 14.4 | 14.4 |
| Probe with the core linked in, Go runtime started, nothing done | 50.9 / 48.6 MB | 24.9 | 21.1 |
| + goolm | | 24.9 | 21.6 |
| + core: open, echo login, 20 round trips, close | | 28.6 | 24.5 |
| + core and goolm | | 28.9 | 24.4 |
| + core and goolm, `GOMEMLIMIT` 8 MiB | | 26.6 | 22.5 |
| The app (UIKit), core and goolm, one run | 52–55 MB | 35.6 | 31.1 |

Per step, in the bare process with the core and mattn (run 38032894450, one of the three processes, MiB):

| Step | Footprint | Go mapped | Go heap objects | Goroutines | ms |
|---|---:|---:|---:|---:|---:|
| go_runtime | 20.3 | 6.5 | 1.5 | 1 | 0 |
| core_open | 21.1 | 7.0 | 1.6 | 6 | 5 |
| echo_login | 23.1 | 9.1 | 1.4 | 10 | 24 |
| echo_messages_20 | 24.4 | 9.9 | 1.6 | 10 | 62 |
| core_close | 24.0 | 9.9 | 1.6 | 1 | 3 |
| olm_account | 24.0 | 9.9 | 1.6 | 1 | 6 |
| olm_room_key | 24.0 | 10.2 | 1.7 | 1 | 1 |
| megolm_decrypt_20 | 24.1 | 10.2 | 1.9 | 1 | 10 |
| gc_free | 23.3 | 9.0 | 1.3 | 1 | 1 |

With `GOMEMLIMIT` 8 MiB, the peak drops by about 2 MiB, but the 20 echo round trips take 700 ms instead of 60 (the garbage collector runs almost constantly).

Sizes: the probe executable is 48.6 MB with mattn (50.9 MB with modernc), stripped; the app and its extension each carry their own copy of the static library (52 MB each).

What the figures say:

1. **What is linked in dominates, not the Go runtime.** The Go runtime alone, with goolm, costs about 4 MiB over the empty program (13.9 against 10.0; Go mapped 4.6 MiB, heap 0.2 MiB). Linking the core (483 packages against 164, a 48.6 MB binary against 3.3 MB) adds another 7 MiB before any work with mattn, 11 MiB with modernc: the package initializers (bridgev2, the Telegram connector, their tables and registries) and the pages of the binary that the loader writes to (its data, rebased at load time).
2. **The work is cheap.** The core's whole journey adds about 3.5 MiB; goolm adds half a MiB. Decrypting messages costs almost nothing.
3. **mattn is lighter than modernc** by 3.8 MiB from the start (smaller binary, less to initialize) and stays so.
4. **The simulator's baseline is not a device's.** An empty process weighs 10 MiB on the simulator, much more than an empty process on a device; only the differences carry over, and even they are approximate (the simulator runs on macOS, with its own libraries).

So a Go extension that links only what it needs (receive an encrypted event, decrypt it with goolm, write the notification) costs about 4.5 MiB over an empty process on the simulator, against 14.5 MiB for one that links the whole core.

## Decision (part 1)

1. **iOS builds use `-buildmode=c-archive` packaged as an XCFramework** (`build-go-xcframework.sh`), with the same C header approach as the other platforms. Verified: the probe builds for devices and the simulator, links into an app and an extension, and runs.
2. **iOS uses `mattn/go-sqlite3`**, as Android does: cgo is required on iOS anyway, and it saves 3.4 MiB of the extension's budget. The build tag of `core/storage/sqlite` will include `ios` when the core itself is built for the iOS app (E3.3); until then, the probe selects it with `musubee_cgo_sqlite`.
3. **The extension will not link the whole core.** The extension's Go code will be a dedicated package that links only what decrypting a notification needs (goolm, the crypto store, an HTTP client for the event); the main app keeps the whole core. The figures make a Go extension plausible (about 4.5 MiB over an empty process for goolm and the runtime); whether it fits is decided in part 2, against the device's real limit, with a Swift extension and lighter crypto as the fallback.
4. **The measured threshold becomes a regression test now, on the simulator.** The `ios-memory` job runs on every pull request and fails when the median peak of a bare process running the core and goolm goes over a budget: 30 MiB with mattn, 35 MiB with modernc (the figures above plus about 20 %). The device measurement (part 2) will add the real threshold; the simulator's absolute figures do not carry over to a device.

## Known gaps

- **No measurement on a device yet.** The limit (`limit_remaining_bytes` + `footprint_bytes` in the extension), the device's baseline and the real figures need an iPhone, a Mac and a paid Apple Developer account (`CLAUDE.md` §8). The test app, its NSE and the device instructions are ready (`apps/mobile/ios-probe/README.md`).
- **The extension's own overhead is unknown**: the test NSE never ran on the simulator, so the footprint of an empty extension (Foundation, UserNotifications) is not measured.
- **The steps model the work, not the network.** The probe does not fetch an event from a homeserver (HTTP, TLS) nor read a real crypto store; both add memory. A real extension would also load an existing Olm account rather than create one.
- **Telegram is in the binary** but not exercised: the probe does not connect to a network.

## State of knowledge

**Verified** (2026-10-10, CI on GitHub's macOS runners):

- The Go core and goolm build for iOS (`ios/arm64`, device and simulator) as a static library, and link into an app and a Notification Service Extension.
- The probe runs on the simulator in an app and in bare processes, with both SQLite drivers; the figures above.
- `xcrun simctl push` with `"mutable-content": 1` delivers the notification without running the extension, on this runner's Xcode and simulator.
- Provisional notification authorization is granted without a prompt.
- Most of the fixed cost of the probe comes from linking the core, not from the Go runtime: the same probe without the core (`memprobe_nocore`) peaks 10.5 MiB lower (mattn) and 3.9 MiB over the empty program.
- The CI's budget check fails when the median peak is over the budget (`reports.py check`, tried on the run's reports with a budget below the figures).

**Assumed**:

- The differences between processes carry over to a device roughly; the absolute figures do not.
- The extension's real code (crypto store in SQLite, an HTTP and TLS client to fetch the event) adds a few MiB to the 4.5 MiB measured for goolm and the runtime; not measured.

**Unknown**:

- The extension's real memory limit on the test user's iPhone, and whether Apple's documented or undocumented limits differ by device or iOS version.
- Whether an extension written in Swift with a light crypto library (for example vodozemac through its C or Swift bindings) would be necessary: it would avoid the Go runtime's fixed cost entirely.
- AGPL and the App Store (`CLAUDE.md` §3): still open, unchanged by this ADR.

## Consequences

- The repository has its first iOS code: a test app, not the product. The real app (Capacitor's iOS project) comes with E3.3 and will reuse `build-go-xcframework.sh`.
- Every pull request runs two macOS jobs of about ten minutes for the iOS measurements, and fails if the simulator's figures grow by more than about 20 %.
- The core's packages will have to keep the extension's dependencies small: what an extension package imports is what it pays for, at load time.
- Part 2 of T1.7 needs the maintainer: a Mac (or the macOS CI with signing secrets), an Apple Developer account, and the test user's iPhone. Its result decides between a Go extension (minimal binary), a Swift extension with lighter crypto, or hosted mode only on iOS.
- **Revisit if:** the device measurement contradicts the simulator's differences; a future Xcode runs extensions for simulated pushes (the CI records it); Go reduces its runtime's fixed footprint.

## Sources

Accessed 2026-10-10.

- Apple, `task_info` and `task_vm_info` (fields `phys_footprint`, `ledger_phys_footprint_peak`, `limit_bytes_remaining`): the iOS SDK's `mach/task_info.h`, read through the compiler on the CI runner.
- Apple Developer Forums, NSE memory limit reported by developers (24–25 MB, undocumented by Apple; memory not released between pushes, since the extension's process is reused): https://developer.apple.com/forums/thread/64634, https://developer.apple.com/forums/thread/722879
- Go, build modes (`c-archive` is the only library mode for `ios/arm64`): `go help buildmode` and https://pkg.go.dev/cmd/go#hdr-Build_modes
- mautrix-go v0.31.0, `crypto/goolm` (`account`, `session`): https://github.com/mautrix/go
- XcodeGen 2.46.0 (MIT): https://github.com/yonaskolb/XcodeGen
- Notificare, "Testing Push Notifications in iOS Simulator in 2020" (simulated pushes did not run the extension then): https://notificare.com/blog/2020/04/24/Testing-Push-Notifications-In-iOS-Simulator-In-2020/
- ADR 0001 (stack), 0009 (bridgev2 in-process, SQLite), 0010 (shared library), 0011 (Android, mattn), 0012 (API contract)
