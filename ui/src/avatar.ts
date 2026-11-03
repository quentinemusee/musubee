// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

// What an avatar shows without a picture (docs/ADR/0061): the name's
// initials, on a hue taken from the name, so that a conversation keeps
// its color everywhere.

/** A word without the symbols it starts with: "(Bob)" is "Bob)". */
function letters(word: string): string[] {
  return [...word.replace(/^[^\p{L}\p{N}]+/u, "")];
}

/** Up to two initials: of the first two words, or the first two letters of one word. */
export function initials(name: string): string {
  const words = name
    .trim()
    .split(/\s+/u)
    .filter((w) => /[\p{L}\p{N}]/u.test(w));
  if (words.length === 0) {
    return "?";
  }
  if (words.length === 1) {
    return letters(words[0]!).slice(0, 2).join("").toUpperCase();
  }
  return ((letters(words[0]!)[0] ?? "") + (letters(words[1]!)[0] ?? "")).toUpperCase();
}

/** A hue from 0 to 359 for a name, always the same for the same name. */
export function hueOf(name: string): number {
  let hash = 0;
  for (const char of name) {
    hash = (hash * 31 + char.codePointAt(0)!) % 360;
  }
  return hash;
}
