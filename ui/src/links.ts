// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

// Links in a message's text (docs/ADR/0066): web addresses only, shown as
// written, so that the user sees where a link goes before following it.

export type TextPart = { text: string } | { text: string; url: string };

const CANDIDATE = /\b(?:https?:\/\/|www\.)[^\s<>"]+/giu;
// Punctuation that ends a sentence rather than an address.
const TRAILING = /[.,;:!?'"*_~]+$/u;

/** The address a candidate links to, or null: http and https only. */
export function linkTarget(candidate: string): string | null {
  const withScheme = /^www\./iu.test(candidate) ? `https://${candidate}` : candidate;
  try {
    const url = new URL(withScheme);
    return url.protocol === "http:" || url.protocol === "https:" ? url.href : null;
  } catch {
    return null;
  }
}

/** Trims what follows an address but is not part of it: final punctuation, an unbalanced ")". */
function trimCandidate(candidate: string): string {
  let text = candidate.replace(TRAILING, "");
  while (text.endsWith(")") && (text.match(/\(/gu)?.length ?? 0) < (text.match(/\)/gu)?.length ?? 0)) {
    text = text.slice(0, -1).replace(TRAILING, "");
  }
  return text;
}

/** Splits a text into plain parts and links, in order; joined, the parts give the text back. */
export function linkParts(text: string): TextPart[] {
  const parts: TextPart[] = [];
  let last = 0;
  for (const match of text.matchAll(CANDIDATE)) {
    const candidate = trimCandidate(match[0]);
    const url = linkTarget(candidate);
    if (url === null || candidate === "") {
      continue;
    }
    const start = match.index;
    if (start > last) {
      parts.push({ text: text.slice(last, start) });
    }
    parts.push({ text: candidate, url });
    last = start + candidate.length;
  }
  if (last < text.length) {
    parts.push({ text: text.slice(last) });
  }
  return parts;
}
