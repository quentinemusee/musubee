// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

// Electron's main process (docs/ADR/0013-desktop-shell.md): it starts the
// core in a child process, serves the interface on app://musubee, and
// carries validated requests and the core's events between them.
//
// Security (apps/desktop/README.md): every window is sandboxed with context
// isolation and no Node integration; the page may not navigate, open
// windows, embed web views or get permissions; only the main frame of our
// own windows, on our own origin, may talk to the core.

import { readFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";
import { app, BrowserWindow, ipcMain, protocol, safeStorage, session, type IpcMainInvokeEvent } from "electron";
import { APP_ORIGIN, APP_SCHEME, APP_URL, serveAppFile } from "./app-protocol.js";
import { CoreKeyError, loadCoreKey } from "./core-key.js";
import { CoreProcess, type CoreStatus } from "./core-process.js";
import { RequestValidator } from "./ipc.js";

const appDir = join(dirname(fileURLToPath(import.meta.url)), "..");
// Written by scripts/build.mjs. Packaging (E3.2) will move them to
// process.resourcesPath.
const resources = join(appDir, "resources");
const coreExecutable = join(resources, "core", process.platform === "win32" ? "musubee-core.exe" : "musubee-core");
const uiRoot = join(resources, "ui");

// Tests run the app with their own data directory.
if (process.env["MUSUBEE_USER_DATA_DIR"]) {
  app.setPath("userData", process.env["MUSUBEE_USER_DATA_DIR"]);
}

// Playwright's Electron loader appends --password-store=basic and
// --use-mock-keychain to every app it starts: safeStorage would then use a
// hardcoded password on Linux and a mock Keychain on macOS, while the
// end-to-end tests check the system's secure storage (docs/ADR/0027).
// Remove them before the app is ready, when Chromium reads them; a test
// that needs a plain password store asks for it with
// MUSUBEE_E2E_PASSWORD_STORE.
if (process.env["MUSUBEE_E2E"] === "1") {
  app.commandLine.removeSwitch("password-store");
  app.commandLine.removeSwitch("use-mock-keychain");
  const passwordStore = process.env["MUSUBEE_E2E_PASSWORD_STORE"];
  if (passwordStore) {
    app.commandLine.appendSwitch("password-store", passwordStore);
  }
}

protocol.registerSchemesAsPrivileged([{ scheme: APP_SCHEME, privileges: { standard: true, secure: true } }]);

// Two cores on the same database would corrupt it: one app per data directory.
if (!app.requestSingleInstanceLock()) {
  app.quit();
} else {
  main();
}

function main(): void {
  const validator = new RequestValidator(JSON.parse(readFileSync(join(resources, "core-api.schema.json"), "utf8")));
  const coreDataDir = join(app.getPath("userData"), "core");
  const core = new CoreProcess({
    executable: coreExecutable,
    dataDir: coreDataDir,
    // The core's own log is in its data directory; this is only what it
    // writes when it crashes.
    onStderr: (line) => console.error(`[core] ${line}`),
  });
  const windows = new Set<BrowserWindow>();
  let statusSeq = 0;

  core.onStatus((status: CoreStatus) => {
    statusSeq++;
    if (status.state === "failed") {
      console.error(`The core failed: ${status.reason}`);
    }
    for (const window of windows) {
      window.webContents.send("core:status", { seq: statusSeq, status });
    }
  });
  core.onEvent((event) => {
    for (const window of windows) {
      window.webContents.send("core:event", event);
    }
  });

  if (process.env["MUSUBEE_E2E"] === "1") {
    // For the end-to-end tests (e2e/), which read it from the main process.
    // call and onEvent let them measure the core without the renderer.
    Object.assign(globalThis, {
      musubeeE2E: {
        corePid: () => core.pid,
        startupTimesMs: () => [...core.startupTimesMs],
        call: (request: Parameters<CoreProcess["call"]>[0]) => core.call(request),
        onEvent: (listener: (event: string) => void) => core.onEvent(listener),
      },
    });
  }

  const trusted = (event: IpcMainInvokeEvent): boolean => {
    const frame = event.senderFrame;
    return (
      frame !== null &&
      frame === event.sender.mainFrame &&
      frame.origin === APP_ORIGIN &&
      [...windows].some((window) => window.webContents === event.sender)
    );
  };
  ipcMain.handle("core:call", (event, payload: unknown) => {
    if (!trusted(event)) {
      throw new Error("refused: not a Musubee window");
    }
    const checked = validator.check(payload);
    return checked.ok ? core.call(checked.request) : checked.response;
  });
  ipcMain.handle("core:status", (event) => {
    if (!trusted(event)) {
      throw new Error("refused: not a Musubee window");
    }
    return { seq: statusSeq, status: core.status };
  });

  app.on("web-contents-created", (_, contents) => {
    contents.setWindowOpenHandler(() => ({ action: "deny" }));
    contents.on("will-navigate", (event) => event.preventDefault());
    contents.on("will-redirect", (event) => event.preventDefault());
    contents.on("will-attach-webview", (event) => event.preventDefault());
  });

  app.on("second-instance", () => {
    const [window] = windows;
    if (window !== undefined) {
      if (window.isMinimized()) {
        window.restore();
      }
      window.focus();
    }
  });

  let quitting = false;
  app.on("before-quit", (event) => {
    if (quitting) {
      return;
    }
    // Let the core close its database before the app exits.
    event.preventDefault();
    quitting = true;
    void core.stop().finally(() => app.quit());
  });
  app.on("window-all-closed", () => {
    if (process.platform !== "darwin") {
      app.quit();
    }
  });

  void app.whenReady().then(async () => {
    protocol.handle(APP_SCHEME, (request) => serveAppFile(uiRoot, request.url));
    session.defaultSession.setPermissionRequestHandler((_, __, callback) => callback(false));
    session.defaultSession.setPermissionCheckHandler(() => false);
    startCore(core, coreDataDir);
    app.on("activate", () => {
      if (windows.size === 0) {
        void openWindow(windows);
      }
    });
    await openWindow(windows);
  });
}

/**
 * Starts the core with the key that the app keeps in the system's secure
 * storage (core-key.ts), or lets it keep its own. safeStorage works only
 * once the app is ready.
 */
function startCore(core: CoreProcess, dataDir: string): void {
  let databaseKey: string | undefined;
  try {
    const key = loadCoreKey(dataDir, safeStorage);
    if (key.source === "safe-storage") {
      databaseKey = key.key;
      console.info(`The core's key is in the system's secure storage (${key.event})`);
    } else if (process.platform !== "win32") {
      console.warn(`The core's key is not in the system's secure storage: ${key.reason}`);
    }
  } catch (error) {
    const reason = error instanceof CoreKeyError ? error.message : `the core's key: ${String(error)}`;
    console.error(`The core cannot start: ${reason}`);
    core.fail(reason);
    return;
  }
  core.start(databaseKey === undefined ? {} : { databaseKey });
}

async function openWindow(windows: Set<BrowserWindow>): Promise<void> {
  const window = new BrowserWindow({
    width: 1000,
    height: 700,
    minWidth: 640,
    minHeight: 420,
    title: "Musubee",
    show: false,
    webPreferences: {
      preload: join(appDir, "dist", "preload.cjs"),
      contextIsolation: true,
      sandbox: true,
      nodeIntegration: false,
      webSecurity: true,
      spellcheck: false,
      devTools: !app.isPackaged,
    },
  });
  windows.add(window);
  window.on("closed", () => windows.delete(window));
  window.once("ready-to-show", () => window.show());
  window.webContents.on("render-process-gone", (_, details) => {
    console.error(`The interface stopped: ${details.reason}`);
    if (details.reason !== "clean-exit" && !window.isDestroyed()) {
      window.reload();
    }
  });
  await window.loadURL(APP_URL);
}
