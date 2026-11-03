// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

// A conversation as a text file (T2.75): what the device stores, the
// oldest first, one message per line. Media is named, not included.

import type { Message } from "./core-api/types.gen";
import { summary } from "./media";

const pad = (n: number) => String(n).padStart(2, "0");

/** One line of the export: "2026-11-03 14:30 Alice: text"; the text's own lines are indented. */
export function exportLine(message: Message): string {
  const at = new Date(message.timestamp_ms);
  const when = `${at.getFullYear()}-${pad(at.getMonth() + 1)}-${pad(at.getDate())} ${pad(at.getHours())}:${pad(at.getMinutes())}`;
  const who = message.from_me ? "You" : message.sender_name || "Someone";
  const what = message.deleted === true ? "(deleted)" : summary(message);
  return `${when} ${who}: ${what.replaceAll("\n", "\n    ")}`;
}

/** The whole export: a title line, then the messages, oldest first. */
export function exportText(name: string, messages: readonly Message[]): string {
  return [`${name}`, "", ...messages.map(exportLine), ""].join("\n");
}

/** A file name for the export: the conversation's name, without characters file systems refuse. */
export function exportFileName(name: string): string {
  // Control characters go too (code points below 32).
  const safe = [...name]
    .map((char) => (char.codePointAt(0)! < 32 || '\\/:*?"<>|'.includes(char) ? "_" : char))
    .join("")
    .trim();
  return `${safe === "" ? "conversation" : safe}.txt`;
}
