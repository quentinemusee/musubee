# Musubee — project context for Claude Code

> Working name: **Musubee**. A Beeper-style universal messenger built on Matrix that **hides all of Matrix's complexity** from the user. 100% open source.

Read this file in full at the start of every session. It is the source of truth for decisions. If you contradict it, update it in the same commit and explain why.

## 0. Language

**Everything in the repository is written in English**: Markdown files, ADRs, code, identifiers (variables, types, functions), file and directory names, comments, script output, UI strings in source code, commit messages and pull requests. No French anywhere in the project. `python scripts/language_check.py` enforces this in CI.

The **conversation** with the maintainer in Claude Code is in **French**.

## 1. Product vision

Users sign in, add their accounts (Telegram, Signal, WhatsApp…), then write and send voice messages, photos, GIFs and files from a single place, end-to-end encrypted. They can **merge** conversations with the same person coming from several networks into a single thread. Maximum customization (themes, appearance, layout). Self-hosting possible in the long run.

Targets: **Android, iOS, Windows, macOS, Linux** (native apps). **No web app for now** (later phase, "hosted core" mode only).

## 2. Architecture decisions (locked unless an ADR says otherwise)

| Topic | Decision |
|---|---|
| Core | **Go**: `mautrix-go` (MPL-2.0), `bridgev2` framework, pure-Go Olm encryption (`goolm` build tag), SQLite |
| Interface | **TypeScript + React + Vite**, a single codebase for every platform |
| Desktop | **Electron** (core embedded as a sidecar/library, or a remote core) |
| Mobile | **Capacitor** + a native plugin embedding the Go core; thin Kotlin (Android) and Swift (iOS) shells |
| iOS notifications | Notification Service Extension (NSE) in Swift, a separate process with very limited memory |
| License | **AGPL-3.0-or-later** for all our code. DCO required from contributors |
| Server tests | Synapse homeserver in Docker (reference), never mocks for integration tests |

Why not Flutter / Wails / Tauri: see `docs/ADR/0001-stack.md`. Summary: Flutter can call a Go core through a C FFI (gomuks' "Nexus" frontend does), but it does not let us reuse the UI for the future web app, nor follow the proven "Go backend + web frontend + Electron" model; Wails mobile is experimental; system webviews are a problem on Linux (the gomuks project left Wails for Electron because of WebKitGTK). *(Corrected in T0.1: the initial version said Flutter had "no natural path" to a Go core, which was too strong.)*

License clarified in T0.1: **AGPL-3.0-or-later**, copyright "Quentin Raimbaud", repository compliant with the REUSE 3.3 specification. See `docs/ADR/0002-license-and-reuse.md`.

### Two connection modes per account (central concept)
1. **On-device**: the bridge runs inside the app and talks directly to the network (Telegram, Signal…). Real end-to-end encryption: no server sees the content.
2. **Hosted bridge** (ours or the user's): fallback when mode 1 is not possible. "End-to-bridge" encryption: the Matrix server sees nothing, but **the bridge host sees the plaintext**. The UI must say so honestly.

The domain abstraction layer (`Conversation`, `Message`, `Person`, `Account`) **must not expose any Matrix type to the UI**. This is what makes switching between modes possible.

### Conversation merging
A local link table `Person ↔ [Conversation]` (stored on the device, syncable in encrypted form). A feature of the domain layer, not of the bridges.

## 3. Known limits — do not promise the impossible

- **No audio/video calls across networks.** Beeper does not support them either; relaying them would require reverse-engineered VoIP stacks. What we do: a call notification + opening the official app; native calls (MatrixRTC) only between Musubee users, later.
- **iOS is the riskiest platform.** The NSE is a separate process with little memory (Beeper had 15 MB, then 50 MB after an exemption obtained through the EU DMA; more on some devices). A full Go runtime is tight there. A small push relay is required. **iOS starts in hosted-bridge mode.**
- **Legal risk of bridges**: the terms of service of WhatsApp, iMessage, etc. are hostile to unofficial clients. Increasing risk order: native Matrix → Telegram → Signal → Discord/Slack → WhatsApp → iMessage. We start with the least risky.
- **AGPL and Apple's App Store**: compatibility must be validated before publishing on iOS. It is not resolved. Deferred by the maintainer on 2026-10-07, when the repository was private; it became public on 2026-10-08 and still has no external contributors.
- I (the design assistant) am not a lawyer: licensing points must be reviewed by a competent human before the public launch.

## 4. Open questions settled by the spikes (phase 1 closed, ADR 0016)

1. Can a `bridgev2` connector run **without a homeserver**, in the same process as the client? **Yes** (ADR 0009): `core/localmatrix` implements the Matrix side; mautrix-telegram's connector runs unchanged (ADR 0014).
2. Does the Go core build cleanly as a shared library for Windows, Android and iOS (size, memory, cgo)? **Yes**: shared library on Windows and Linux (ADR 0010), with JNI on Android (ADR 0011), XCFramework of static libraries on iOS (ADR 0015).
3. What is the real memory budget in the iOS NSE? Go (`goolm`) or lighter crypto? **Open**: on the simulator, Go with goolm costs about 4 MiB over an empty process, and the extension will link only what it needs (ADR 0015); the device's limit is measured in E3.3 (paid Apple account).
4. How does the UI talk to the core? **One JSON API contract** (ADR 0012): in-process on mobile, a supervised child process over stdio on the desktop (ADR 0013).
5. Conversation merging: data model and sync across devices. **Settled in T2.1 (ADR 0017)**: links keyed by each conversation's network identity, in the core's database; the encrypted sync is designed, not built.
6. UI: state management and virtualized message list (chosen by ADR, with performance measurements on low-end Android). **Phase 2** (T2.4).

Not spiked in phase 1 and moved to phase 2: native Matrix accounts (the core as a Matrix client, T2.2) and Signal on the device (libsignal is Rust, T2.6).

## 5. Repository layout (target)

```
/core          Go: domain, connectors, storage, local API
/ui            React + TypeScript
/apps/desktop  Electron
/apps/mobile   Capacitor + native shells (android/, ios/ + NSE)
/infra         test docker compose (Synapse, Postgres, test bridges…)
/docs          TASKS.md, TESTING.md, SKILLS.md, ADR/
/scripts       repository tooling (license and language checks, skill installation) and its tests
/LICENSES      texts of the licenses cited by SPDX headers (REUSE)
/.github       GitHub Actions workflows, PR template
```

## 6. Working rules (mandatory)

1. **Nothing is "done" without passing tests.** Write the test first when possible. Run them and show the output. See `docs/TESTING.md`.
2. **Test environment as close to reality as possible**, even if the setup is long: real Synapse, real bridges, real devices for iOS. No mocks for integrations. Mocks are only tolerated in pure unit tests.
3. **Never a real user account nor a production secret in CI.** Test credentials are allowed, stored as encrypted CI secrets (GitHub Actions secrets), never in the repository or in logs: the app's `api_id` / `api_hash`, and tokens of bots dedicated to tests, even on production Telegram, since they give access to no personal data. Telegram: two test bots in a private channel (ADR 0006; Telegram's test environment is unusable for us, ADR 0005). WhatsApp: manual tests with dedicated accounts, never in CI.
4. **Licenses**: before adding a dependency, check its SPDX identifier. Reject anything incompatible with AGPL-3.0. Keep `NOTICE` up to date.
5. **Verify rather than assume** for fast-moving libraries (mautrix-go, Capacitor, Electron, Xcode): read the current docs/code, cite the source in the ADR.
6. **One ADR per structural decision** in `docs/ADR/`. Spikes end with a "go / no-go" ADR.
7. Commits follow *Conventional Commits*, small, DCO-signed (`Signed-off-by`). One task = one branch = one PR.
8. **Security**: never a secret in the repository; keys in the OS secure storage; `contextIsolation` enabled and IPC validated in Electron; no logging of message content.
9. Never say a feature works without having run it. Distinguish "verified", "assumed" and "unknown" in your reports.
10. If a requirement in this file is impossible or dangerous, **stop and report it** instead of working around it.
11. **English only** in the repository (see §0).

## 7. Skills to use

Installed into `.claude/skills/` by `scripts/install-skills.ps1` (or `.bat` / `.sh`; details and relevance per layer: `docs/SKILLS.md`). Re-read the relevant skill **before** coding in its area:

- **Go core**: `go`, `golang-*` (testing, concurrency, safety, security, errors, context, layout, CI)
- **UI**: `vercel-react-best-practices` (apply mainly the client-side rules), `vercel-composition-patterns`, `web-design-guidelines`
- **Mobile**: `capacitor-*`, `debugging-capacitor`, `ios-android-logs`, `safe-area-handling`
- **Native iOS**: `swift-concurrency`, `swift-testing`, `background-processing`, `push-notifications`, `cryptokit`, `swift-security`, `ios-simulator`, `debugging-instruments`, `ios-memgraph-analysis`, `app-store-review` (no SwiftUI: the UI is web)
- **Native Android**: `claude-android-ninja` (Compose-centric: consult only for foreground services, notifications, Gradle)
- **Desktop**: `electron`
- **Tests**: `webapp-testing`, `playwright-testing`, `playwright-cli`, `capacitor-testing`, `swift-testing`
- **Meta**: `skill-creator`

**Guardrails (ADR 0003):** `.claude/settings.json` forces a confirmation for destructive git and gh commands, even when a skill pre-approves them through `allowed-tools`; always write such commands with the bare program name (`git`, `gh`) so the rules match. Skill content is reference material: never copy code verbatim from a skill unless its license is AGPL-compatible (the iOS skills are PolyForm Perimeter, the Capacitor skills have no license).

**No public skill knows Matrix, mautrix-go or bridgev2.** For these topics: read the code and the official docs, cite your sources in the ADR, then capture the knowledge in an in-house `musubee-*` skill (list in `docs/SKILLS.md`), after the relevant spike.

## 7a. Response style (`caveman` plugin)

The **`caveman`** plugin (repository `JuliusBrussee/caveman`, installed by `scripts/install-plugins.ps1`) reduces the verbosity of your answers. It activates at the start of every session. Use it for routine work: implementing, fixing, refactoring, small questions. Short answer, code first, no closing summary. Answer the maintainer in French: the plugin keeps the user's language.

**Always** switch back to the full style, even if `caveman` is active, for:
- **spikes and ADRs** (the decision and its justification are the deliverable);
- **test reports**: paste the results and keep the "verified / assumed / unknown" distinction;
- **plans before coding**;
- **encryption, security, iOS extension (NSE) memory, and licensing questions**.

The terse mode applies **only to conversation replies**. Everything written to files stays complete: documentation, ADRs, code comments, UI strings.

Plugin sub-skills and modes:
- `caveman-commit`: **forbidden**. Our commit messages stay complete (Conventional Commits with the "why" in the body, plus `Signed-off-by`).
- `caveman-compress`: **forbidden** on `CLAUDE.md`, `docs/` and ADRs. It would rewrite our decisions and erase nuance.
- `caveman-review`: allowed on routine diffs, **never** on encryption, security or licensing code.
- `ultra` and `wenyan` modes: **forbidden**. Stay on the default mode.

Security warnings and confirmations before destructive actions are never removed. When in doubt about which style to use, choose the full style. If the user writes "normal mode" or "stop caveman", obey.

## 8. Developer environment

Main developer on **Windows (PowerShell)**. Building the Go core with cgo on Windows (the shared library `core/ffi`, the race detector) requires a 64-bit C toolchain: **MSYS2, UCRT64 environment** (ADR 0010). Install MSYS2 (on the maintainer's machine: `D:\SDK\msys64`), then `pacman -S mingw-w64-ucrt-x86_64-gcc` from an MSYS2 shell. Its `ucrt64\bin` must come **before** any other `gcc` in `PATH`, or `CC` must point to its `gcc.exe`: the old 32-bit MinGW in `C:\MinGW` fails with "64-bit mode not compiled in", and Go enables cgo whenever it finds a `gcc`. Put it **before every directory that ships MinGW runtime DLLs** (`libgcc_s_seh-1.dll`, `libstdc++-6.dll`; for example an ESP-IDF or Rust `esp` toolchain): GCC's internal programs (`cc1`, `as`) find their DLLs through `PATH`, load the wrong ones and die silently, and Go only reports `runtime/cgo: ...cgo.exe: exit status 2` (seen on the maintainer's machine in T1.2). Without a working toolchain, use `CGO_ENABLED=0` (everything but `core/ffi` and `-race` still builds and tests). The Android app (T1.3) also needs **JDK 21** (on the maintainer's machine: `D:\SDK\jdk-21`; the system `java` may be older) and the Android SDK with **NDK 28.2.13676358**; Gradle builds the core for Android itself (`apps/mobile/README.md`). **iOS builds require a Mac** (or a macOS CI runner). Push notifications and the NSE on a real device require a **paid Apple Developer account** (someone close to the maintainer will use the app on an iPhone: they are the first iOS test user).

## 9. Commands (update this block in every task that adds some)

Verified in T0.1 (Windows, Python 3.14, REUSE 6.2.0):

```
tools:      python -m pip install -r scripts/requirements-dev.txt
licenses:   python scripts/license_check.py
language:   python scripts/language_check.py
tests:      python -m unittest discover -s scripts/tests -v
skills:     scripts/install-skills.ps1 | .bat | .sh      (--dry-run prints the commands only)
plugins:    scripts/install-plugins.ps1 | .bat | .sh
```

Verified in T0.3 (Windows, Go 1.27, Docker Desktop; see `infra/README.md`):

```
infra unit:         go test ./infra/...
infra integration:  go test -tags=integration ./infra/...       (needs Docker; about 20 s)
test homeserver:    go run ./infra/cmd/testenv up | status | user NAME PASS | down
vet:                go vet -tags=integration ./infra/...    go vet -tags=telegram ./infra/...
```

Verified in T0.4 (see `infra/README.md` for the test bots):

```
telegram e2e:       go test -tags=telegram -v ./infra/telegramtest/   (needs Docker and network; message test needs the bot tokens)
```

`go test -race` needs a 64-bit C toolchain (cgo): see §8 (MSYS2 UCRT64 on Windows since T1.2).

Verified in T0.5 (see `apps/mobile/e2e/README.md`):

```
mobile tools:       cd apps/mobile/e2e && npm ci
android hello:      MUSUBEE_ANDROID_UDID=emulator-5554 npm run test:android   (needs a running emulator)
ios hello:          MUSUBEE_IOS_UDID=<udid> npm run test:ios                  (macOS, booted simulator)
```

Verified in T0.6:

```
dependency licenses: go run ./scripts/licenseaudit        (-v lists every dependency; run npm ci first)
```

Verified in T1.1 (Windows, Go 1.27.1; see `core/README.md`). **Go 1.27.1 or later is required**: 1.27.0 has a `database/sql` deadlock (ADR 0009).

```
core tests:         go test -count=1 ./core/...
core vet / format:  go vet ./core/...        gofmt -l core
core benchmark:     go test -run '^$' -bench RoundTrip -benchtime 2000x ./core/bridgehost/
core race (Docker): docker run --rm -v <repo>:/src -w /src golang:1.27.1 go test -race ./core/...
```

Verified in T1.2 (Windows with MSYS2 UCRT64 GCC 16.2.0, and the `golang:1.27.1` container; see ADR 0010). In Git Bash, put the toolchain first for the command: `PATH="/d/SDK/msys64/ucrt64/bin:$PATH"`.

```
core race:          go test -race -count=1 ./core/...
shared library:     go build -buildmode=c-shared -o musubee.dll ./core/ffi     (libmusubee.so on Linux)
FFI round trip:     go test -count=1 -v -run TestSharedLibrary ./core/ffi/    (MUSUBEE_FFI_PINGS, MUSUBEE_FFI_ROUNDTRIPS: longer loops)
no toolchain:       CGO_ENABLED=0 go test ./core/...                            (skips core/ffi)
```

Verified in T1.3 (Windows, Go 1.27.1, JDK 21, NDK 28.2.13676358, emulator API 30; see `apps/mobile/README.md` and ADR 0011):

```
mobile app tools:   cd apps/mobile && npm ci && npx cap sync android
android build:      cd apps/mobile/android && ./gradlew :app:assembleDebug      (-Pmusubee.abis=x86_64: fewer ABIs)
android tests:      ANDROID_SERIAL=emulator-5554 ./gradlew :app:connectedDebugAndroidTest
android app e2e:    cd apps/mobile/e2e && MUSUBEE_ANDROID_UDID=emulator-5554 npm run test:android:app
android measures:   ANDROID_SERIAL=emulator-5554 ./measure-core.sh 5          (soak test: see apps/mobile/README.md)
android SQLite:     go test -tags=musubee_cgo_sqlite -count=1 ./core/...     (the cgo driver of Android builds, on the host; ADR 0011)
```

Verified in T1.4 (Windows, Go 1.27.1, Node 24.20, npm 11; see ADR 0012, `ui/README.md`):

```
api types:          go generate ./core/api            (Go and TypeScript types from core/api/schema/core-api.schema.json)
ui tools:           cd ui && npm ci
ui tests:           npm test                          npm run typecheck
api benchmark:      go test -run '^$' -bench RoundTrip -benchtime 2000x ./core/embedded/
transports:         cd scripts/transportbench && npm ci && node bench.mjs   (needs the C toolchain; MUSUBEE_BENCH_PINGS, _ROUNDTRIPS, _ONLY)
```

Verified in T1.5 (Windows, Go 1.27.1, Node 24.20, golangci-lint 2.14.0; CI on Linux, Windows and macOS; see ADR 0013, `apps/desktop/README.md`):

```
go lint:            golangci-lint run ./core/... ./infra/... ./scripts/licenseaudit/...   (.golangci.yml)
ui lint / build:    cd ui && npm run lint && npm run build
desktop tools:      cd apps/desktop && npm ci                       (after cd ui && npm ci)
desktop checks:     npm test        npm run typecheck        npm run lint
desktop build:      npm run build                                   (core, interface, main process)
desktop app:        npm start
desktop e2e:        npm run test:e2e                                (opens real windows; Linux: xvfb-run, see the README)
core program:       go build -o musubee-core.exe ./core/cmd/musubee-core
```

golangci-lint is installed with `go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.14.0` (on the maintainer's machine: `GOBIN=D:\SDK\golangci-lint`).

Verified in T1.6 (Windows, Go 1.27.1, JDK 21; the Telegram journeys on CI with the test bots' secrets; see ADR 0014). Telegram's application credentials come from `MUSUBEE_TG_API_ID` and `MUSUBEE_TG_API_HASH`, never from the repository:

```
telegram core e2e:    go test -tags=telegram -v ./infra/telegramtest/ondevice/       (test bots, see infra/README.md)
telegram desktop e2e: cd apps/desktop && npm run build && npx playwright test telegram.spec.ts
telegram android:     MUSUBEE_TG_API_ID=... MUSUBEE_TG_API_HASH=... ./gradlew :app:assembleDebug   (the app offers Telegram)
telegram android e2e: ANDROID_SERIAL=emulator-5554 ./gradlew :app:connectedDebugAndroidTest -Pandroid.testInstrumentationRunnerArguments.class=app.musubee.core.TelegramTest (+ bot arguments, see apps/mobile/README.md)
stripped core:        go build -trimpath -ldflags="-s -w" -o musubee-core.exe ./core/cmd/musubee-core
```

A bot has one Telegram session at a time: never run two Telegram tests at once.

Verified in T1.7 part 1 (macOS CI runners, Xcode, Go 1.27.1, XcodeGen 2.46.0; see ADR 0015, `apps/mobile/ios-probe/README.md`). iOS builds need a Mac:

```
memory probe:       go run ./core/cmd/memprobe -core -crypto -data-dir <dir>     (cgo; JSON report)
probe tests:        go test -count=1 ./core/memprobe/        (-tags=memprobe_nocore: without the core)
ios xcframework:    apps/mobile/ios-probe/build-go-xcframework.sh ./cmd/memprobe core/cmd/memprobe/memprobe.h MusubeeMemProbe apps/mobile/ios-probe/build musubee_cgo_sqlite
ios simulator:      apps/mobile/ios-probe/run-simulator.sh <udid> <output-dir> musubee_cgo_sqlite
```

Verified in T2.1 (Windows, Go 1.27.1, Node 24.20; see ADR 0017):

```
persons store:      go test -count=1 ./core/persons/                 (-tags=musubee_cgo_sqlite: the Android driver)
persons journeys:   go test -count=1 -run Persons -v ./core/embedded/
```
