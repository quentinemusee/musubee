// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

// Synthetic conversations for the thread list benchmark (docs/ADR/0020):
// deterministic, with the mix of lengths of a real chat.

import type { Message } from "../src/core-api/types.gen";

/** A small deterministic generator (mulberry32), so that every run sees the same messages. */
function random(seed: number): () => number {
  let a = seed >>> 0;
  return () => {
    a = (a + 0x6d2b79f5) >>> 0;
    let t = a;
    t = Math.imul(t ^ (t >>> 15), t | 1);
    t ^= t + Math.imul(t ^ (t >>> 7), t | 61);
    return ((t ^ (t >>> 14)) >>> 0) / 4294967296;
  };
}

const WORDS =
  "the a to and of you I it is that in we for on this be have are with just can was not so but what do like at all get know about will one if my your no up out time now good think see when then back more here how there".split(
    " ",
  );
// A long name with letters beyond ASCII, as real names have.
const SENDERS = ["Alice Martin", "Bob", "Łucja Dąbrowska-Nowak"];

function sentence(next: () => number, words: number): string {
  const out: string[] = [];
  for (let i = 0; i < words; i++) {
    out.push(WORDS[Math.floor(next() * WORDS.length)]!);
  }
  return out.join(" ");
}

function text(next: () => number): string {
  const r = next();
  if (r < 0.7) {
    return sentence(next, 1 + Math.floor(next() * 12)); // A short line.
  }
  if (r < 0.95) {
    return sentence(next, 12 + Math.floor(next() * 50)); // A few lines.
  }
  // A long message, in paragraphs.
  const paragraphs = 2 + Math.floor(next() * 4);
  return Array.from({ length: paragraphs }, () => sentence(next, 20 + Math.floor(next() * 40))).join("\n\n");
}

/**
 * Messages numbered from `from` (inclusive) to `to` (exclusive), oldest
 * first, one minute apart. The same number always gives the same message.
 */
export function messages(from: number, to: number): Message[] {
  const out: Message[] = [];
  for (let i = from; i < to; i++) {
    const next = random(i + 1_000_003);
    const fromMe = next() < 0.4;
    out.push({
      message_id: `m${i}`,
      conversation_id: "bench",
      from_me: fromMe,
      sender_name: fromMe ? "" : SENDERS[Math.floor(next() * SENDERS.length)]!,
      timestamp_ms: Date.UTC(2026, 0, 1) + i * 60_000,
      kind: next() < 0.02 ? "notice" : "text",
      text: text(next),
      status: fromMe ? "sent" : "received",
    });
  }
  return out;
}
