// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

// Which navigations of the app's pages leave for the system's browser
// (docs/ADR/0066): a link in a message, to a web address. Everything else
// stays refused, as before.
//
// No Electron import, so that it runs under plain Node in the tests.

/** The address to open in the system's browser for a navigation, or null to only refuse it. */
export function externalLink(url: string, appOrigin: string): string | null {
  let parsed: URL;
  try {
    parsed = new URL(url);
  } catch {
    return null;
  }
  if (parsed.protocol !== "http:" && parsed.protocol !== "https:") {
    return null;
  }
  if (parsed.origin === appOrigin || parsed.username !== "" || parsed.password !== "") {
    return null;
  }
  return parsed.href;
}
