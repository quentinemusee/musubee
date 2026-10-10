# Task breakdown — Musubee

Format: **ID — title**. Each task has a deliverable, acceptance criteria and tests. Claude Code handles them in order, one branch per task. Phases 2+ are deliberately less detailed: they will be refined after the phase 1 go/no-go.

Legend: 🧪 = mandatory test · 🍎 = requires a Mac · 💶 = requires a paid Apple account · 📱 = requires a real device

---

## Phase 0 — Foundations and test environment

**T0.1 — Repository skeleton and license**
- Deliverable: the layout of `CLAUDE.md` §5, `LICENSE` (AGPL-3.0), `NOTICE`, `CONTRIBUTING.md` with DCO, `docs/ADR/0001-stack.md`, SPDX headers.
- Acceptance: the repository clones and every directory has a README; the license-check command runs.
- 🧪 Test: a CI script that fails if a source file has no SPDX header.

**T0.2 — Install and verify the skills**
- Deliverable: skills installed according to `docs/SKILLS.md`, list of those actually loaded (`/plugin` then `/reload-plugins`, restart the session if needed).
- Deliverable: run `scripts/install-skills.ps1` and `scripts/install-plugins.ps1` (`caveman` plugin) — or their `.bat` / `.sh` equivalents — fix any skill name that has changed, commit the fixed script.
- Acceptance: `npx skills list` shows ~38 skills; for each area (Go, React, Capacitor, Swift, Android, Electron, tests), a control question makes Claude cite the right skill.

**T0.3 — Integration test environment**
- Deliverable: `infra/compose.test.yml` with Synapse, PostgreSQL, dummybridge; start and stop script; documentation.
- Acceptance: a Matrix test client creates a room, sends a message, reads it back.
- 🧪 Test: `go test -tags=integration ./infra/...` starts the environment, checks service health, shuts it down (integration tests carry the `integration` build tag, see ADR 0004).

**T0.4 — Telegram test environment**
- Deliverable: Telegram bridge connected to the local Synapse and to Telegram. Telegram's test servers turned out unusable (ADR 0005), so the test uses two dedicated test bots in a private channel (ADR 0006).
- Acceptance: a message flows between two Telegram test parties (bots) through the bridge, both ways.
- 🧪 Test: automated E2E, usable in CI. Also record in the ADR what exists (or not) for Signal.

**T0.5 — Multi-platform CI**
- Deliverable: pipeline with Linux, Windows and macOS runners; Android job on an emulator; iOS job on a simulator 🍎. Choice of the mobile E2E tool (Maestro or Appium) documented in an ADR.
- Acceptance: a "hello test" passes on every target.

**T0.6 — Automated license audit**
- Deliverable: audit tool for Go and npm dependencies, wired into CI, with a list of justified exceptions.
- 🧪 Test: deliberately add an incompatible dependency on a test branch → CI fails.

---

## Phase 1 — Feasibility spikes (go / no-go)

Goal: prove that the Go core can run **on the device**. Each spike ends with an ADR with a clear conclusion (feasible / feasible with reservations / not feasible) and measured figures.

**T1.1 — bridgev2 connector without a homeserver**
- Question: can a `bridgev2` connector run in-process, with a local implementation of the "Matrix" interface?
- Deliverable: our own minimal **echo connector** written with `bridgev2` (AGPL; replaces Beeper's dummybridge, which has no license, see ADR 0004): one login flow, fake contacts and conversations, messages echoed back, a few failure modes (delayed echo, failed send). Proof of concept running it in-process (send and receive in a Go test), ADR.
- Rule: do not read or copy dummybridge's code (no license). Learning from AGPL-compatible mautrix bridges is allowed once their license is checked.
- 🧪 Test: the message goes through the connector and reaches local storage, without Synapse.
- Outcome: feasible (go), see ADR 0009. Requires Go 1.27.1 or later.

**T1.2 — Core as a shared library (Windows and Linux)**
- Deliverable: core built as a shared library (cgo), called from a small host program; size and memory measurements. Document the C toolchain required on Windows.
- 🧪 Test: round-trip call through FFI, without memory leaks (long-running loop test).
- Outcome: go on Windows and Linux, see ADR 0010. C interface in `core/ffi/musubee.h`; Windows toolchain MSYS2 UCRT64; SQLite stays pure Go (`modernc.org/sqlite`).

**T1.3 — Core on Android**
- Deliverable: AAR or library via the NDK (evaluate `gomobile bind` and the shared library, compare in an ADR), minimal Capacitor plugin, foreground service.
- 📱 🧪 Test: send and receive on an emulator, then a real device; battery and memory measurements over 1 h.
- Outcome: go with JNI entry points in the shared library (gomobile dropped), core in a `specialUse` foreground service; see ADR 0011. Verified on an API 30 emulator and on a Pixel 8 Pro (Android 17); the x86_64 emulators of the CI (API 30 and 35) found that the pure-Go SQLite is killed by seccomp on Android x86_64, so Android builds use `mattn/go-sqlite3` (ADR 0011 point 7). Open points: Google Play's acceptance of `specialUse`, 17 ms round trips on the phone, and the idle battery cost (the one-hour soak ran during a call in another app).

**T1.4 — Core ↔ UI API contract**
- Deliverable: versioned schema (commands and event streams), TypeScript type generator, ADR comparing direct FFI and a local socket.
- 🧪 Test: contract tests on the Go side and the TypeScript side against the same schema.
- Outcome: JSON Schema 2020-12 in `core/api/schema`, version 1.0, with Go and TypeScript types generated by an in-house generator; contract tests on both sides with two independent validators, and every response and event of the core validated in its tests; see ADR 0012. Transports measured from Node: every one adds under 0.2 ms per call, so mobile stays in-process and the desktop will run the core as a child process over stdio (T1.5). Also fixed: the SQLite pool reopened connections on every message (round trip 11 ms → 4 ms on Windows).

**T1.5 — Electron shell**
- Deliverable: Electron window with a minimal UI, core as a sidecar or library, `contextIsolation` enabled, validated IPC.
- 🧪 Test: Playwright, "open the app, send a dummy message" journey.
- Outcome: the core runs as its own program (`core/cmd/musubee-core`) over stdio, supervised by Electron's main process (restart with backoff, gives up after five crashes in a minute); the interface is served on `app://musubee` with a strict content security policy, in a sandboxed and context-isolated window, and every request is checked against the core API schema before it reaches the core; see ADR 0013. Playwright journeys pass on Linux, Windows and macOS in CI: send and echo, restart with the history kept, isolation checks, recovery after the core is killed. Measured on the three systems: the main process reaches the core as fast as plain Node did in ADR 0012, the IPC adds 0.1 to 0.3 ms per call, the core starts in 60 to 420 ms, and re-rendering the open thread on every event adds 1.4 to 7 ms to a round trip, an input for the virtualized list ADR. Also: a minimal React interface, golangci-lint and oxlint in CI.

**T1.6 — Telegram on the device**
- Deliverable: Telegram connector in "on-device" mode inside the core, against the test servers.
- 🧪 Test: automated E2E on Windows and emulated Android.
- 📱 Battery: one hour on a real phone left idle (screen off, no call or other app in use), connected to Telegram, to measure the idle cost of the service that T1.3 could not isolate (ADR 0011).
- Outcome: mautrix-telegram's connector runs inside the core (`core/connector/telegram`), with a pure-Go stand-in for its cgo WebP encoder, the application credentials from the environment (desktop) or the build (Android), and a new API command `accounts.logout` (API 1.1); see ADR 0014. Automated journeys against production Telegram with the test bots pass on CI: the core alone, the desktop app through its interface (Windows) and the Android app on an API 35 emulator (bot login 6 to 11 s, a post reaches the core in 3 to 5 s, an answer is seen by the other bot in under 0.3 s, Go heap about 2 MiB). The core grows from 18 to 54 MB (desktop) and from 17 to 56 MB (Android x86_64). Battery, one hour idle on the Pixel 8 Pro with the maintainer's account (logged in by phone number): about 5 mAh for the app (8.6 % of what the phone attributed, mostly the radio), 2.45 s of CPU; but the connection dropped and came back 14 times during deep Doze, as the service holds no wake lock, so real-time delivery on an idle phone needs push (E2.6). Known gap: the Telegram session key is stored in plaintext in `core.db`, against `CLAUDE.md` §6.8; to fix before any public release (encrypted database with a key from the OS keystore).

**T1.7 — iOS NSE memory budget** 🍎 💶 📱
- Deliverable: core (or lighter crypto) built for iOS arm64 (evaluate `gomobile bind` to an xcframework), integrated into a test NSE; measurement of real memory on an iPhone (tools: `debugging-instruments`, `ios-memgraph-analysis`).
- Acceptance: a "feature → peak memory" table; written decision: full Go / lighter crypto / hosted mode only for iOS.
- 🧪 Test: the measured threshold becomes a regression test.
- Part 1 done (2026-10-10, ADR 0015, proposed): the core and goolm build for iOS as an XCFramework (`apps/mobile/ios-probe/build-go-xcframework.sh`); a test app with a notification extension; the `ios-memory` CI job measures the probe on the simulator for both SQLite drivers. `simctl push` does not run extensions, so the simulator figures come from bare processes. Outcome so far: on the simulator, the Go runtime with goolm costs about 4 MiB over an empty process, linking the whole core another 7 MiB, the work about 4 MiB; the extension will link only what it needs, and iOS will use `mattn/go-sqlite3`. The CI fails if the simulator's peak goes over a budget (30 MiB with mattn).
- Part 2 (needs the maintainer: a Mac or signing secrets for the macOS CI, a paid Apple Developer account, the test user's iPhone): run the test app on the device (`apps/mobile/ios-probe/README.md`), read the extension's real limit, decide full Go / lighter crypto / hosted mode only, and add the device's threshold to the regression test.

**T1.8 — Go / no-go decision**
- Deliverable: a summary ADR in `docs/ADR/`: what is feasible on each platform, what is postponed, fallback plan (hosted-bridge mode).
- Outcome: **go** (ADR 0016). Windows, Linux and macOS: on the device. Android: on the device, with reservations (Play and `specialUse`, push needed in deep Doze). iOS: hosted mode first; on-device plausible but its extension's memory limit is unmeasured, deferred to E3.3 with the paid Apple account. Not spiked and moved to phase 2: native Matrix accounts (the core as a Matrix client, also the base of the hosted-bridge mode) and Signal on the device (libsignal is Rust).

---

## Phase 2 — Android + Windows MVP (networks: native Matrix, Telegram, Signal)

Order and reasons: ADR 0016. Tasks first, then the epics they belong to. 📞 = needs a manual action of the maintainer (a phone number, a real device).

**T2.1 — Domain model and person links**
- Deliverable: the domain layer of the core (`Account`, `Conversation`, `Message`, `Person`) and a local link table person ↔ conversations in the core's database; API commands to list persons and to link or unlink a conversation (minor API version); an ADR on the data model and on how the links will sync across devices, encrypted (`CLAUDE.md` §4, question 5).
- Acceptance: no Matrix identifier or type reaches the API (checked by a test over the echo and Telegram journeys); the links survive a restart.
- 🧪 Test: unit tests of the domain layer, contract tests of the new commands on both sides.
- Outcome: persons in the core (`core/persons`, ADR 0017), API 1.2 (`persons.*` commands, `person.*` events, `Conversation.person_id`). A link names a conversation by its identity on its network, not by its local room: it survives restarts, stays dormant while its account is absent, and wakes up when the account comes back; these identities are the same on every device, which the encrypted sync will need (rules recorded in the ADR, not built). Only direct conversations can be linked. Every response and event of the core's tests, and of the Telegram journey, is checked for Matrix identifiers (`apitest.MatrixIDs`). The interface's store keeps the persons; showing them is T2.5. Not run: the refusal of group conversations (the echo network has none; T2.5 adds one).

**T2.2 — Native Matrix accounts**
- Deliverable: a `matrix` network in the core: sign-in to a homeserver, sync, rooms as conversations, text sent and received, end-to-end encryption with goolm and a crypto store in the core's database; ADR (it is also the client of the hosted-bridge mode).
- Acceptance: an encrypted conversation between two accounts works through the core API, and survives a restart of the core.
- 🧪 Test: integration tests against Synapse in Docker (`infra/testenv`), never a mock.
- Outcome: a `matrix` network in the core (`core/connector/matrix`, ADR 0018): a bridgev2 connector in which the core is an end-to-end encrypted Matrix client of the user's homeserver (mautrix-go's `cryptohelper` on goolm), with its crypto and state stores in the core's database; logout deletes the device on the homeserver and its keys in the database. Every Go build of the core now needs the `goolm` tag. The journey against Synapse (`infra/matrixtest`: an encrypted direct conversation both ways, a restart, logout, nothing sensitive in the log) runs on CI only: Docker Desktop does not start on the maintainer's machine. Left for later tasks: group invitations (accepted in another client for now), edits, media content, key backup and verification (E2.4, E2.8); the pickle key and the access token are not protected until T2.3.

**T2.3 — Secrets at rest** (E2.9)
- Deliverable: ADR comparing an encrypted database and encrypting the secrets only, with the key in the OS secure storage; implementation on Windows (DPAPI or Credential Manager) and Android (Keystore).
- Acceptance: no session key (Telegram, Matrix) is readable in the database file.
- 🧪 Test: on Windows CI and the Android emulator, with the real key stores.

**T2.4 — UI state and virtualized message list**
- Deliverable: ADR choosing state management and a virtualized list (`CLAUDE.md` §4, question 6), with measurements on a throttled Android emulator, then on a low-end phone 📞 when available.
- 🧪 Test: a performance test of a long conversation (thousands of messages), kept as a regression test.

**T2.5 — Unified inbox** (E2.3)
- Deliverable: the conversation list of every account, merged per person (T2.1), with pagination and offline display.
- 🧪 Test: Playwright on the desktop, Appium on Android.

**T2.6 — Signal on the device** 📞
- Deliverable: spike, then connector: libsignal (Rust, static C library) built for Windows, Linux, macOS and Android; mautrix-signal's connector in the core; a test account on Signal's staging servers, registered by the maintainer with a real phone number (ADR 0005).
- 🧪 Test: automated journey against staging, like Telegram's.

Epics:
- **E2.1** Domain model (`Account`, `Conversation`, `Message`, `Person`) and abstraction layer, with no Matrix type leaking to the UI.
- **E2.2** Onboarding without Matrix jargon, account management, network connection flows (QR, codes).
- **E2.3** Unified inbox, conversation list, pagination, offline.
- **E2.4** Conversation thread: text, replies, reactions, editing; media, voice messages, GIFs, files.
- **E2.5** **Conversation merging** per person and easy switching between threads.
- **E2.6** Android notifications (foreground service, FCM/UnifiedPush). T1.6 found that the core's Telegram connection does not stay up in deep Doze (ADR 0014, Battery): push must wake the app, then the core reconnects; measure the delivery delay and the cost of holding the connection.
- **E2.7** Customization: design tokens, themes per app, per conversation, per person.
- **E2.8** Encryption: honest indicators of the connection mode (on-device / hosted bridge), key backup and recovery.
- **E2.9** Secrets at rest: the core's database (network session keys, such as Telegram's, ADR 0014) encrypted with a key held in the OS secure storage (Android Keystore, Windows DPAPI or Credential Manager, macOS and iOS Keychain, Linux Secret Service). Required before any public release (`CLAUDE.md` §6.8).

## Phase 3 — Self-hosting, full desktop, iOS

- **E3.1** Hosted-bridge mode: Docker package (homeserver + bridges + push relay), self-hosting documentation.
- **E3.2** macOS and Linux (installers, signing, updates).
- **E3.3** iOS 🍎 💶: hosted mode first, then on-device if T1.7 allows it. Notification extension, TestFlight for the test user.
- **E3.4** Push relay (receiving native pushes, encrypted forwarding to the apps).

## Phase 4 — Extension

- **E4.1** WhatsApp (legal risk to be reassessed with a competent human).
- **E4.2** Other networks, native calls between Musubee users (MatrixRTC).
- **E4.3** Extension system / advanced customization.
- **E4.4** "Hosted core" web app.
