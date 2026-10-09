// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { App } from "./app/App";
import { StoreContext } from "./app/state";
import { Store } from "./store";
import "./styles.css";

const root = createRoot(document.getElementById("root")!);
const shell = window.musubee;

if (shell === undefined) {
  root.render(<p className="empty">Musubee's interface runs inside the Musubee app.</p>);
} else {
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
}
