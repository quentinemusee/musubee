// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

// The one permission the page may get (docs/ADR/0038): the microphone, to
// record voice messages, for the main frame of the app's own origin. The
// camera, the screen and every other permission stay refused.
//
// This module does not import Electron, so that it runs under plain Node in
// the tests; main.ts installs the handlers.

import { APP_HOST, APP_SCHEME } from "./app-protocol.js";

/**
 * Whether a URL or origin is the app's own (app://musubee). URL.origin is
 * "null" for a scheme the URL standard does not know, so the scheme and
 * host are compared instead.
 */
export function isAppOrigin(url: string | undefined): boolean {
  if (url === undefined) {
    return false;
  }
  try {
    const parsed = new URL(url);
    return parsed.protocol === `${APP_SCHEME}:` && parsed.host === APP_HOST;
  } catch {
    return false;
  }
}

/** Answers a permission request: only audio capture for the app's main frame. */
export function allowRequest(permission: string, details: { isMainFrame: boolean; requestingUrl: string; mediaTypes?: readonly string[] }): boolean {
  return (
    permission === "media" &&
    details.isMainFrame &&
    isAppOrigin(details.requestingUrl) &&
    details.mediaTypes !== undefined &&
    details.mediaTypes.length > 0 &&
    details.mediaTypes.every((type) => type === "audio")
  );
}

/** Answers a permission check, which the page makes before a request (and to list devices). */
export function allowCheck(permission: string, requestingOrigin: string, details: { isMainFrame: boolean; mediaType?: string }): boolean {
  return permission === "media" && details.isMainFrame && isAppOrigin(requestingOrigin) && details.mediaType === "audio";
}
