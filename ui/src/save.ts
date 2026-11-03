// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

// Saving a file the user asked for (docs/ADR/0068): through the shell where
// the page cannot save a Blob download (Android's WebView), else as a
// download, which the desktop saves with its dialog.

import type { ShellFiles } from "./shell";

let shellFiles: ShellFiles | undefined;

/** Called once at start-up with the shell's way to save files, if it has one. */
export function setShellFiles(files: ShellFiles | undefined): void {
  shellFiles = files;
}

/** Saves a Blob under a name; resolves with whether it was saved (a download always counts). */
export async function saveBlob(blob: Blob, name: string, mimeType: string): Promise<boolean> {
  if (shellFiles !== undefined) {
    return shellFiles.save(name, mimeType, blob);
  }
  const url = URL.createObjectURL(blob);
  const link = document.createElement("a");
  link.href = url;
  link.download = name;
  link.rel = "noopener";
  link.click();
  // The browser has the Blob once the click is handled.
  setTimeout(() => URL.revokeObjectURL(url), 60_000);
  return true;
}
