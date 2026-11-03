// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

// Which of the core's events the desktop announces (docs/ADR/0047), as the
// Android service does with its own rules (ADR 0033): a new message from
// someone else, recent, while no window of the app is focused. History
// that comes in batches (ADR 0041) is not news.
//
// This module does not import Electron, so that it runs under plain Node
// in the tests; main.ts shows the notifications.

/** Messages older than this are never announced. */
export const MAX_AGE_MS = 5 * 60_000;

/** A notification to show. */
export interface Announcement {
  conversationId: string;
  title: string;
  body: string;
}

const MEDIA_LABELS: Record<string, string> = {
  image: "Photo",
  video: "Video",
  audio: "Audio",
  voice: "Voice message",
  file: "File",
};

interface AnnouncedMessage {
  conversation_id?: unknown;
  from_me?: unknown;
  deleted?: unknown;
  sender_name?: unknown;
  timestamp_ms?: unknown;
  kind?: unknown;
  text?: unknown;
  media?: { animated?: unknown; file_name?: unknown };
}

/** What a notification says of a message: as the interface's previews (ui/src/media.ts). */
export function messageSummary(message: AnnouncedMessage): string {
  const text = typeof message.text === "string" ? message.text : "";
  const kind = typeof message.kind === "string" ? message.kind : "text";
  let label = MEDIA_LABELS[kind];
  if (label === undefined) {
    return text || "New message";
  }
  if ((kind === "image" || kind === "video") && message.media?.animated === true) {
    label = "GIF";
  }
  if (text !== "") {
    return `${label}: ${text}`;
  }
  const fileName = message.media?.file_name;
  return kind === "file" && typeof fileName === "string" && fileName !== "" ? `${label}: ${fileName}` : label;
}

/**
 * The announcement of one of the core's events (JSON), or null: only a
 * message.added from someone else, not deleted, at most MAX_AGE_MS old,
 * while no window is focused.
 */
export function announcement(eventJson: string, now: number, focused: boolean): Announcement | null {
  if (focused) {
    return null;
  }
  let parsed: unknown;
  try {
    parsed = JSON.parse(eventJson);
  } catch {
    return null;
  }
  const event = parsed as { type?: unknown; data?: { message?: AnnouncedMessage } };
  const message = event.data?.message;
  if (event.type !== "message.added" || message === undefined || message.from_me === true || message.deleted === true) {
    return null;
  }
  if (typeof message.conversation_id !== "string" || typeof message.timestamp_ms !== "number" || now - message.timestamp_ms > MAX_AGE_MS) {
    return null;
  }
  const sender = typeof message.sender_name === "string" && message.sender_name !== "" ? message.sender_name : "New message";
  return { conversationId: message.conversation_id, title: sender, body: messageSummary(message) };
}
