// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

// The app:// protocol, which serves the interface's built files (ui/dist)
// to the window instead of file:// (docs/ADR/0013-desktop-shell.md): the
// page gets its own origin, every response carries the
// Content-Security-Policy, and nothing outside the interface's directory
// can be read.
//
// This module does not import Electron, so that it runs under plain Node in
// the tests; main.ts registers the handler.

import { readFile } from "node:fs/promises";
import { extname, isAbsolute, join, relative, resolve, sep } from "node:path";

export const APP_SCHEME = "app";
export const APP_HOST = "musubee";
export const APP_ORIGIN = `${APP_SCHEME}://${APP_HOST}`;
export const APP_URL = `${APP_ORIGIN}/index.html`;

/** The same policy as ui/index.html, which the header makes binding. */
export const CONTENT_SECURITY_POLICY =
  "default-src 'none'; script-src 'self'; style-src 'self'; img-src 'self' data:; font-src 'self'; connect-src 'none'; base-uri 'none'; form-action 'none'; frame-ancestors 'none'";

const MIME_TYPES: Record<string, string> = {
  ".html": "text/html; charset=utf-8",
  ".js": "text/javascript; charset=utf-8",
  ".css": "text/css; charset=utf-8",
  ".json": "application/json",
  ".svg": "image/svg+xml",
  ".png": "image/png",
  ".ico": "image/x-icon",
  ".woff2": "font/woff2",
};

/**
 * The file that an app:// URL names inside root, or null when the URL is not
 * of this app or leaves root.
 */
export function resolveAppPath(root: string, url: string): string | null {
  let parsed: URL;
  try {
    parsed = new URL(url);
  } catch {
    return null;
  }
  if (parsed.protocol !== `${APP_SCHEME}:` || parsed.host !== APP_HOST) {
    return null;
  }
  let path: string;
  try {
    path = decodeURIComponent(parsed.pathname);
  } catch {
    return null;
  }
  if (path.includes("\0") || path.includes("\\")) {
    return null;
  }
  const base = resolve(root);
  const file = resolve(join(base, path === "/" ? "index.html" : path));
  const inside = relative(base, file);
  if (inside === "" || inside.startsWith(`..${sep}`) || inside === ".." || isAbsolute(inside)) {
    return null;
  }
  return file;
}

/** Answers an app:// request with a file of root. */
export async function serveAppFile(root: string, url: string): Promise<Response> {
  const headers = {
    "Content-Security-Policy": CONTENT_SECURITY_POLICY,
    "X-Content-Type-Options": "nosniff",
    "Cache-Control": "no-store",
  };
  const file = resolveAppPath(root, url);
  const type = file === null ? undefined : MIME_TYPES[extname(file).toLowerCase()];
  if (file === null || type === undefined) {
    return new Response("Not found", { status: 404, headers });
  }
  try {
    return new Response(await readFile(file), { headers: { ...headers, "Content-Type": type } });
  } catch {
    return new Response("Not found", { status: 404, headers });
  }
}
