// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

// The preload script: the only bridge between the interface and the main
// process (docs/ADR/0013-desktop-shell.md). It runs sandboxed, so it is
// CommonJS and may require nothing but Electron's renderer modules. It
// exposes window.musubee.core and window.musubee.notifications
// (ui/src/shell.ts) and nothing else: never ipcRenderer itself.

import electron = require("electron");
import type { IpcRendererEvent } from "electron";

const { contextBridge, ipcRenderer } = electron;

type Status = { state: string };

interface StatusMessage {
  seq: number;
  status: Status;
}

contextBridge.exposeInMainWorld("musubee", {
  core: {
    call(request: string): Promise<string> {
      return ipcRenderer.invoke("core:call", request) as Promise<string>;
    },
    onEvent(listener: (event: string) => void): () => void {
      const handler = (_: IpcRendererEvent, event: string) => listener(event);
      ipcRenderer.on("core:event", handler);
      return () => {
        ipcRenderer.removeListener("core:event", handler);
      };
    },
    onStatus(listener: (status: Status) => void): () => void {
      // The current status, then every change. The main process numbers the
      // changes, so that a reply to "core:status" that arrives after a newer
      // change does not overwrite it.
      let seq = -1;
      const deliver = (message: StatusMessage) => {
        if (message.seq > seq) {
          seq = message.seq;
          listener(message.status);
        }
      };
      const handler = (_: IpcRendererEvent, message: StatusMessage) => deliver(message);
      ipcRenderer.on("core:status", handler);
      void (ipcRenderer.invoke("core:status") as Promise<StatusMessage>).then(deliver);
      return () => {
        ipcRenderer.removeListener("core:status", handler);
      };
    },
    startOver(): Promise<void> {
      return ipcRenderer.invoke("core:startOver") as Promise<void>;
    },
  },
  // Links in messages open in the system's browser (docs/ADR/0066).
  links: {
    open(url: string): Promise<void> {
      return ipcRenderer.invoke("links:open", url) as Promise<void>;
    },
  },
  // The main process shows the notifications (docs/ADR/0047); desktop
  // systems need no permission for them.
  notifications: {
    ask(): Promise<boolean> {
      return Promise.resolve(true);
    },
    // What they show of a message (docs/ADR/0051), kept by the main process.
    preview(): Promise<string> {
      return ipcRenderer.invoke("notifications:preview") as Promise<string>;
    },
    setPreview(preview: string): Promise<void> {
      return ipcRenderer.invoke("notifications:setPreview", preview) as Promise<void>;
    },
    onOpen(listener: (conversationId: string) => void): () => void {
      const handler = (_: IpcRendererEvent, conversationId: unknown) => {
        if (typeof conversationId === "string") {
          listener(conversationId);
        }
      };
      ipcRenderer.on("notifications:open", handler);
      return () => {
        ipcRenderer.removeListener("notifications:open", handler);
      };
    },
  },
});
