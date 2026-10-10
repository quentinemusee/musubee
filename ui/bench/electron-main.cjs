// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

// The Electron main process of the benchmark (bench/run.mjs): one window of
// a phone's size, showing MUSUBEE_BENCH_URL. The window must be shown: a
// hidden one gets one frame per second. It does not take the focus, and is
// never throttled when covered.
const { app, BrowserWindow } = require("electron");

app.commandLine.appendSwitch("disable-backgrounding-occluded-windows");
app.commandLine.appendSwitch("disable-renderer-backgrounding");

app.whenReady().then(() => {
  const win = new BrowserWindow({
    width: 412,
    height: 860,
    x: 0,
    y: 0,
    show: false,
    focusable: false,
    skipTaskbar: true,
    webPreferences: { backgroundThrottling: false, contextIsolation: true, sandbox: true },
  });
  win.showInactive();
  void win.loadURL(process.env.MUSUBEE_BENCH_URL);
});
