# apps/desktop — Electron shell

Desktop application for Windows, macOS and Linux. It displays the UI from [`ui/`](../../ui/) and embeds the Go core (sidecar or library), or connects to a remote core.

**Status: empty.** Work planned in T1.5 (Electron shell) then E3.2 (installers, signing, updates). See [`docs/TASKS.md`](../../docs/TASKS.md).

Why Electron and not Wails or Tauri: [`docs/ADR/0001-stack.md`](../../docs/ADR/0001-stack.md).

## Security requirements (non-negotiable)

- `contextIsolation: true`, `nodeIntegration: false`, `sandbox: true` for every window displaying the UI.
- The `preload` bridge exposes a minimal API; **every IPC message is validated** in the main process.
- Keys and tokens live in the OS secure storage, never in plaintext on disk.

## Tests

E2E with Playwright (Electron support) on Windows, macOS and Linux runners. Skills: `electron`, `playwright-testing`, `playwright-cli`.
