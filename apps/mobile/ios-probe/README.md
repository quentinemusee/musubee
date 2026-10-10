# apps/mobile/ios-probe — memory probe for the iOS notification extension (T1.7)

A test app, not part of Musubee. It answers one question of T1.7 ([ADR 0015](../../../docs/ADR/0015-ios-nse-memory.md)): how much memory the Go core and Matrix encryption (goolm) cost inside an iOS **Notification Service Extension** (NSE), a separate process that iOS kills when it goes over a small memory limit.

The probe itself is Go: package [`core/memprobe`](../../../core/memprobe) runs the steps an extension would run and measures after each one. [`core/cmd/memprobe`](../../../core/cmd/memprobe) exports it to C (`musubee_memprobe_run`, [`memprobe.h`](../../../core/cmd/memprobe/memprobe.h)).

| File | Role |
|---|---|
| `build-go-xcframework.sh` | Builds a Go package of the core as an iOS static library (`-buildmode=c-archive`, `GOOS=ios`) for devices and the simulator, packaged as an XCFramework. Meant to serve the real app later too. |
| `project.yml` | [XcodeGen](https://github.com/yonaskolb/XcodeGen) (MIT) specification: the app `MemoryProbe` and its extension `MemoryProbeNSE`. The Xcode project is generated, not versioned. |
| `App/` | The app: asks for provisional notification authorization (no prompt); with the launch argument `--probe`, runs the probe in its own process. |
| `NSE/` | The extension: runs the probe for every push with `"mutable-content": 1`, logs the report, writes the peak in the notification. |
| `control/footprint.c` | The control: an empty C program that prints its own footprint. |
| `run-simulator.sh` | Runs the probe on a booted simulator: as an executable in bare processes (`simctl spawn`), next to the control; in the app; and tries the extension with one `simctl push`. |
| `reports.py` | Puts the reports back together from the unified log and writes a Markdown table. |

## Requirements

macOS with Xcode, Go (version of `core/go.mod`), XcodeGen 2.46.0 and python3. An Apple silicon Mac: the simulator slice is arm64 only.

## On the simulator

```
apps/mobile/ios-probe/build-go-xcframework.sh ./cmd/memprobe core/cmd/memprobe/memprobe.h MusubeeMemProbe apps/mobile/ios-probe/build
apps/mobile/ios-probe/run-simulator.sh <simulator udid> /tmp/ios-memory
```

A fifth argument to `build-go-xcframework.sh`, and a third to `run-simulator.sh`, set Go build tags: `musubee_cgo_sqlite` selects the cgo SQLite driver instead of the pure-Go one. The CI job `ios-memory` (`.github/workflows/checks.yml`) runs both and publishes the tables in the run summary. `MUSUBEE_PROBE_REPETITIONS` sets how many times each bare-process configuration runs (3).

The job then runs `python3 reports.py check OUTPUT_DIR "core + crypto" BUDGET_MIB`, which fails when the median peak of the bare processes running the core and goolm is over the budget set in the workflow's matrix: the regression test of T1.7 on the simulator (ADR 0015).

The simulator enforces **no** memory limit: its figures say what the code costs, not whether it fits. `limit_remaining_bytes` reads 0 there.

`xcrun simctl push` delivers the notification **without running the extension** on the simulators tried (Xcode on GitHub's macOS runners, October 2026): the system log shows SpringBoard adding the notification and no extension process. That is why the bare processes stand in for it. The script still tries once and records the outcome in `report.md`.

## On an iPhone (needs a Mac and an Apple Developer account)

1. Build the XCFramework as above.
2. `cd apps/mobile/ios-probe && xcodegen generate && open MemoryProbe.xcodeproj`.
3. Choose a development team for both targets (or pass `DEVELOPMENT_TEAM=...` to `xcodebuild`), select the iPhone, run the app once.
4. Send the app a push with an alert and `"mutable-content": 1` in `aps` (`xcrun simctl push` only reaches simulators): for example from Apple's Push Notifications Console (developer account), to the device token that the app logs (`APNs device token: ...`). The extension's report is in Console.app (subsystem `app.musubee.memoryprobe`); on a device, `limit_remaining_bytes` plus `footprint_bytes` gives the extension's real limit.
