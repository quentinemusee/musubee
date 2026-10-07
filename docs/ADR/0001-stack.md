# ADR 0001 — Tech stack: Go core, React UI, Electron and Capacitor

- **Status**: accepted
- **Date**: 2026-10-06 (translated into English on 2026-10-07, no change in substance; see "Revisions")
- **Task**: T0.1
- **Deciders**: Quentin Raimbaud (maintainer), with the design assistant

## Context

Musubee is a universal messenger built on Matrix, for Android, iOS, Windows, macOS and Linux. The core must be able to run bridges (Telegram, Signal…) **on the device** for real end-to-end encryption. The most mature Matrix bridges (the mautrix family) are written in Go. We need:

1. a single core, shared by every platform, that reuses these bridges;
2. a single, customizable interface with no visible notion of Matrix;
3. thin native shells for desktop and mobile;
4. licenses compatible with AGPL-3.0-or-later (see [ADR 0002](0002-license-and-reuse.md)).

This ADR records the decision summarized in `CLAUDE.md` §2 and checks its justifications.

## Options considered

### Core

- **Go + mautrix-go (chosen).** A complete Matrix library, the `bridgev2` framework to write and embed connectors, Olm/Megolm encryption in pure Go (`goolm`), hence no C dependency for cryptography. Same language as the existing bridges.
- **Rust + matrix-rust-sdk.** An excellent client SDK, but the mautrix bridges are in Go: they would have to be rewritten or run in a second runtime.
- **TypeScript (matrix-js-sdk) in the UI.** No embeddable bridges; the core could not run in the iOS notification extension, nor in the Android background outside a webview.

### Interface and shells

- **React + TypeScript + Vite, with Electron (desktop) and Capacitor (mobile) (chosen).** A single UI codebase; the same UI will later serve the "hosted core" web app (E4.4). The Go core is embedded as a sidecar or a library (cgo) on the Electron side, and through a native plugin on the Capacitor side.
- **Flutter.** A performant native interface. A Go core can be used through a C FFI: the "Nexus" Flutter frontend of gomuks does it. The "no natural path to a Go core" argument in the initial version of `CLAUDE.md` is therefore **too strong**. What tips the balance against Flutter: no reuse of the UI for the future web app without Flutter's web renderer (less suited to a messenger), no CSS-based customization ecosystem, and the existence of a proven model (gomuks: Go backend + web frontend + Electron).
- **Wails.** Native Go and lightweight, but it uses the system webview: **WebKitGTK on Linux**, judged too unstable in practice by the gomuks project, which left Wails for Electron. Wails v3 mobile support is officially **experimental**.
- **Tauri.** Same system-webview problem on Linux (WebKitGTK); Rust core, so the Go core would be a sidecar with no benefit over Electron.

### Storage

- **SQLite (chosen)**, like mautrix-go and gomuks. The driver choice (cgo `mattn/go-sqlite3` or pure Go) is to be settled in T1.2 according to cross-compilation constraints.

## Decision

| Topic | Choice |
|---|---|
| Core | Go, `mautrix-go` (MPL-2.0), `bridgev2` framework, Olm/Megolm in pure Go (`goolm` build tag), SQLite |
| Interface | TypeScript + React + Vite, a single codebase |
| Desktop | Electron, embedded core (sidecar or library) or remote core |
| Mobile | Capacitor + native plugin embedding the Go core; Kotlin (Android) and Swift (iOS) shells |
| iOS notifications | Notification Service Extension (NSE) in Swift, a separate process |
| Server tests | Synapse in Docker, no mocks for integration |

"On-device" feasibility is **not** established: it is the subject of spikes T1.1 to T1.8. If a spike fails, the fallback is the "hosted bridge" mode, without changing this stack.

## State of knowledge

| Claim | Status | Source |
|---|---|---|
| mautrix-go is licensed MPL-2.0 | **verified** (repository page) | [github.com/mautrix/go](https://github.com/mautrix/go), accessed 2026-10-06 |
| mautrix-go contains a `bridgev2` package | **verified** (repository tree) | same |
| The pure-Go Olm implementation is enabled with the `goolm` build tag | **verified** (file `crypto/registergoolm.go`: `//go:build goolm`, call to `goolm.Register()`) | [crypto/registergoolm.go](https://github.com/mautrix/go/blob/main/crypto/registergoolm.go), `main` branch, 2026-10-06 |
| Latest mautrix-go release: 0.28.1 (June 2026) | **verified** (author's blog post) | [mau.fi, June 2026](https://mau.fi/blog/2026-06-mautrix-release/) |
| gomuks left Wails for Electron because WebKitGTK is too unstable on Linux | **verified** (author's blog post, "Electron wrapper" section) | [mau.fi, June 2026](https://mau.fi/blog/2026-06-mautrix-release/) |
| gomuks exposes its backend through a C FFI interface, used by a Flutter frontend | **verified** (repository README) | [github.com/gomuks/gomuks](https://github.com/gomuks/gomuks), 2026-10-06 |
| gomuks is licensed AGPL-3.0 | **verified** (repository page). Variant `-only` or `-or-later`: **unknown**, to read in the headers before any code reuse (T1.4). `AGPL-3.0-only` code would make the distributed whole effectively "only". | same |
| Wails v3 mobile support is experimental | **verified** (official documentation) | [v3.wails.io/guides/mobile](https://v3.wails.io/guides/mobile), 2026-10-06 |
| npm licenses: Electron 44.5.1 MIT, @capacitor/core 8.5.2 MIT, React 19.3.0 MIT, Vite 8.3.3 MIT, TypeScript 7.0.2 Apache-2.0 | **verified in the npm registry** (`npm view <package> license`); to be confirmed in each repository by the automated audit (T0.6). All compatible with AGPL-3.0. | npm registry, 2026-10-06 |
| A `bridgev2` connector can run without a homeserver, inside the client process | **unknown** → spike T1.1 | — |
| The core builds as a shared library for Windows, Android and iOS with acceptable size and memory | **unknown** → T1.2, T1.3, T1.7 | — |
| A Go runtime fits in the memory of the iOS notification extension | **unknown**, high risk → T1.7. iOS starts in "hosted bridge" mode. | — |
| Signal bridges rely on libsignal (Rust, through cgo), which complicates mobile builds | **assumed** → to verify before adding Signal in "on-device" mode | — |

## Consequences

- **Easier**: reusing the mautrix connectors; sharing the UI across desktop, mobile and the future web app; building on the gomuks architecture model (Go backend + web frontend).
- **Harder**: cgo and cross-compilation (C toolchain on Windows, Android NDK, Xcode for iOS); Electron's weight on desktop; the memory constraint of the iOS extension; two languages (Go and TypeScript) linked by a versioned API contract (T1.4).
- **To watch**: the fast evolution of mautrix-go (0.x versions, unstable API); the maturity of Wails v3 mobile (if it becomes stable and WebKitGTK improves, the question could be reopened for Linux desktop).
- **Revisit if**: T1.1 shows that `bridgev2` cannot run without a homeserver and no reasonable workaround exists; or T1.3/T1.7 show that the Go core is unusable on mobile.

## Revisions

- 2026-10-07 (T0.3): mautrix-go's latest release is now **v0.31.0** (2026-09-16, GitHub API); the "0.28.1 (June 2026)" row above was correct when written. The decision is unchanged.

## Sources

- mautrix-go: <https://github.com/mautrix/go> (accessed 2026-10-06); `crypto/registergoolm.go`, `main` branch.
- Tulir Asokan, "June 2026 releases // Full-text search in gomuks", <https://mau.fi/blog/2026-06-mautrix-release/> (accessed 2026-10-06).
- gomuks: <https://github.com/gomuks/gomuks>, `desktop/` directory (Electron Forge) and README (accessed 2026-10-06).
- Wails v3, mobile guide: <https://v3.wails.io/guides/mobile> (accessed 2026-10-06).
- npm registry, `npm view` (accessed 2026-10-06).
