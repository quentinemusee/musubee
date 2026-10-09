# apps/desktop — Electron shell

Desktop application for Windows, macOS and Linux. It displays the interface from [`ui/`](../../ui/) and runs the Go core in a child process ([ADR 0013](../../docs/ADR/0013-desktop-shell.md)). A remote core comes later.

**Status: T1.5 done.** One window with the minimal interface, the core supervised over stdio, validated IPC, unit and Playwright end-to-end tests on the three systems in CI. Installers, signing and updates: E3.2 (see [`docs/TASKS.md`](../../docs/TASKS.md)).

Why Electron and not Wails or Tauri: [`docs/ADR/0001-stack.md`](../../docs/ADR/0001-stack.md). Why a child process rather than the shared library: [ADR 0012](../../docs/ADR/0012-core-api-contract.md).

## Layout

| File | Role |
|---|---|
| `src/main.ts` | The main process: the window, the `app://musubee` protocol, the IPC handlers, the hardening of every web contents, the single-instance lock |
| `src/core-process.ts` | `CoreProcess`, the supervisor of the core (`core/cmd/musubee-core`): its own request IDs, restart with backoff, `failed` after five crashes in a minute, clean stop |
| `src/ipc.ts` | `RequestValidator`: checks every request of the interface against the core API schema (Ajv) before it reaches the core |
| `src/app-protocol.ts` | Serves the built interface on `app://musubee/` with the content security policy |
| `src/preload.cts` | The preload: exposes `window.musubee.core` (`call`, `onEvent`, `onStatus`), nothing else |
| `scripts/build.mjs` | Builds the core, the interface and the main process into `resources/` and `dist/` |
| `test/` | Unit tests (Vitest); the supervisor's tests run the real core, built by `test/global-setup.ts` |
| `e2e/` | End-to-end tests (Playwright for Electron) |

## Security requirements (non-negotiable)

- `contextIsolation: true`, `nodeIntegration: false`, `sandbox: true`, `webSecurity: true` for every window displaying the interface. The end-to-end tests check that the page has no Node.js and that the renderer is sandboxed.
- The preload exposes a minimal API; **every IPC message is validated** in the main process: only the app's own main frame on `app://musubee` is answered, and each request must match the schema.
- The interface is served by a custom protocol with a strict content security policy (no inline script, no `eval`, no network). Navigation, new windows, `<webview>` and every permission request are refused.
- Keys and tokens live in the OS secure storage, never in plaintext on disk (with the first real network, T1.6).

## Commands

Verified in T1.5 (Windows, Node 24.20, Go 1.27.1; CI on Linux, Windows and macOS). Needs Go, and the interface's packages (`cd ui && npm ci`).

```
npm ci
npm test               (unit tests, with the real core)
npm run typecheck
npm run lint           (oxlint)
npm run build          (core, interface, main process)
npm start              (runs the built app)
npm run test:e2e       (Playwright; run npm run build first)
```

The end-to-end tests open real windows on your screen. On Linux they need a display (`xvfb-run` in CI) and Chromium's sandbox needs unprivileged user namespaces: on Ubuntu 24.04, `sudo sysctl -w kernel.apparmor_restrict_unprivileged_userns=0`. Environment variables of the app: `MUSUBEE_USER_DATA_DIR` (another profile directory, used by the tests), `MUSUBEE_E2E=1` (exposes the test hooks in the main process).

## Skills to re-read before coding

`electron`, `playwright-testing`, `playwright-cli`.
