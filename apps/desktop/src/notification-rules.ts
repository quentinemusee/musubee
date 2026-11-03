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

/** What a notification shows of a message (docs/ADR/0051), as the interface names it. */
export type NotificationPreview = "name_and_text" | "name_only" | "nothing";

const PREVIEWS: readonly NotificationPreview[] = ["name_and_text", "name_only", "nothing"];

/** A stored or requested preview; null when unknown. */
export function parsePreview(value: unknown): NotificationPreview | null {
  return PREVIEWS.includes(value as NotificationPreview) ? (value as NotificationPreview) : null;
}

/** An announcement as the preview lets it show: names without the text, or neither. */
export function hide(announced: Announcement, preview: NotificationPreview): Announcement {
  switch (preview) {
    case "name_and_text":
      return announced;
    case "name_only":
      return { ...announced, body: "New message" };
    case "nothing":
      return { ...announced, title: "Musubee", body: "New message" };
  }
}

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
  mentions_me?: unknown;
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

interface KnownConversation {
  name: string;
  group: boolean;
  /** The conversation's muted_until_ms (API 1.23): -1 until unmuted, 0 not muted. */
  mutedUntil: number;
}

/**
 * What the main process learns of the conversations from the core's
 * answers to the interface and from its events, which all pass through
 * it: their names, for a group's notification title, and their mutes. It
 * keeps nothing of the messages.
 */
export class KnownConversations {
  readonly #known = new Map<string, KnownConversation>();

  /** Learns from one of the core's events (JSON): conversation.updated. */
  learnEvent(eventJson: string): void {
    const event = parse(eventJson) as { type?: unknown; data?: { conversation?: unknown } } | undefined;
    if (event?.type === "conversation.updated") {
      this.#learn(event.data?.conversation);
    }
  }

  /** Learns from one of the core's answers (JSON): any conversation, or list of them, in its result. */
  learnResponse(responseJson: string): void {
    const response = parse(responseJson) as { result?: { conversation?: unknown; conversations?: unknown } } | undefined;
    const result = response?.result;
    if (typeof result !== "object" || result === null) {
      return;
    }
    this.#learn(result.conversation);
    if (Array.isArray(result.conversations)) {
      for (const conversation of result.conversations) {
        this.#learn(conversation);
      }
    }
  }

  #learn(value: unknown): void {
    if (typeof value !== "object" || value === null) {
      return;
    }
    const conversation = value as { conversation_id?: unknown; name?: unknown; kind?: unknown; muted_until_ms?: unknown };
    if (typeof conversation.conversation_id !== "string") {
      return;
    }
    this.#known.set(conversation.conversation_id, {
      name: typeof conversation.name === "string" ? conversation.name : "",
      group: conversation.kind === "group",
      mutedUntil: typeof conversation.muted_until_ms === "number" ? conversation.muted_until_ms : 0,
    });
  }

  /** Whether the user muted the conversation, as of now. */
  muted(conversationId: string, now: number): boolean {
    const until = this.#known.get(conversationId)?.mutedUntil ?? 0;
    return until === -1 || until > now;
  }

  /** A notification's title: the sender, and the group's name in a group. */
  title(conversationId: string, sender: string): string {
    const known = this.#known.get(conversationId);
    return known?.group === true && known.name !== "" ? `${sender} in ${known.name}` : sender;
  }
}

function parse(json: string): unknown {
  try {
    return JSON.parse(json);
  } catch {
    return undefined;
  }
}

/** The conversation that one of the core's events (JSON) deleted (API 1.25), or null. */
export function deletedConversation(eventJson: string): string | null {
  const event = parse(eventJson) as { type?: unknown; data?: { conversation_id?: unknown } } | undefined;
  return event?.type === "conversation.deleted" && typeof event.data?.conversation_id === "string" ? event.data.conversation_id : null;
}

/**
 * The announcement of one of the core's events (JSON), or null: only a
 * message.added from someone else, not deleted, at most MAX_AGE_MS old,
 * in a conversation not muted (unless it mentions the user), while no
 * window is focused.
 */
export function announcement(eventJson: string, now: number, focused: boolean, conversations?: KnownConversations): Announcement | null {
  if (focused) {
    return null;
  }
  const event = parse(eventJson) as { type?: unknown; data?: { message?: AnnouncedMessage } } | undefined;
  const message = event?.data?.message;
  if (event?.type !== "message.added" || message === undefined || message.from_me === true || message.deleted === true) {
    return null;
  }
  if (typeof message.conversation_id !== "string" || typeof message.timestamp_ms !== "number" || now - message.timestamp_ms > MAX_AGE_MS) {
    return null;
  }
  // A mention of the user gets through a mute (docs/ADR/0055).
  if (conversations?.muted(message.conversation_id, now) === true && message.mentions_me !== true) {
    return null;
  }
  const sender = typeof message.sender_name === "string" && message.sender_name !== "" ? message.sender_name : "New message";
  const title = conversations === undefined ? sender : conversations.title(message.conversation_id, sender);
  return { conversationId: message.conversation_id, title, body: messageSummary(message) };
}
