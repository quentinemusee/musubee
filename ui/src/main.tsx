// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { App } from "./app/App";
import { StoreContext } from "./app/state";
import type { Shell } from "./shell";
import { Store } from "./store";
import "./styles.css";

/**
 * The shell the interface runs in: the desktop's preload sets window.musubee;
 * in the mobile app, Capacitor's native bridge is there before any script.
 */
async function findShell(): Promise<Shell | undefined> {
  if (window.musubee !== undefined) {
    return window.musubee;
  }
  const capacitor = (window as { Capacitor?: { isNativePlatform?: () => boolean } }).Capacitor;
  if (capacitor?.isNativePlatform?.() === true) {
    const { capacitorShell } = await import("./capacitor");
    return capacitorShell();
  }
  return undefined;
}

const root = createRoot(document.getElementById("root")!);

void findShell().then((shell) => {
  if (shell === undefined) {
    root.render(<p className="empty">Musubee's interface runs inside the Musubee app.</p>);
    return;
  }
  const store = new Store(shell.core);
  // Started once for the page's whole life, outside React.
  store.start();
  root.render(
    <StrictMode>
      <StoreContext value={store}>
        <App />
      </StoreContext>
    </StrictMode>,
  );
});
