// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

// What the interface says about a message beyond its text: its reactions,
// the message it answers, and what replaced it once deleted.

import type { ConversationActions, Message, MessageReaction, MessageReply } from "./core-api/types.gen";
import { summary } from "./media";

/** What a deleted message shows instead of its text. */
export const DELETED_TEXT = "Message deleted";

/**
 * How a reaction is read out and shown on hover: who reacted, by name when
 * the core gives them (API 1.19), as the chip only shows a count.
 */
export function reactionLabel(reaction: MessageReaction): string {
  const others = reaction.from_me ? reaction.count - 1 : reaction.count;
  const names = (reaction.names ?? []).slice(0, Math.max(others, 0));
  const people: string[] = [...(reaction.from_me ? ["you"] : []), ...names];
  const unnamed = others - names.length;
  if (unnamed > 0) {
    if (people.length === 0) {
      people.push(unnamed === 1 ? "1 person" : `${unnamed} people`);
    } else {
      people.push(unnamed === 1 ? "1 other" : `${unnamed} others`);
    }
  }
  const who = people.length <= 1 ? (people[0] ?? "") : `${people.slice(0, -1).join(", ")} and ${people.at(-1)}`;
  return `${reaction.key}: ${who}`;
}

/** The two lines that quote an answered message: who wrote it, and what. */
export function replyQuote(reply: MessageReply): { sender: string; text: string } {
  const sender = reply.from_me ? "You" : (reply.sender_name ?? "");
  if (reply.deleted) {
    return { sender, text: DELETED_TEXT };
  }
  if (reply.text === undefined && reply.sender_name === undefined && !reply.from_me) {
    // Not on this device: older than the history it keeps.
    return { sender: "", text: "An earlier message" };
  }
  return { sender, text: summary({ kind: reply.kind ?? "text", text: reply.text ?? "" }) };
}

/** The reply_to of a message that answers this one, as this one is now. */
export function replyTo(message: Message): MessageReply {
  return {
    message_id: message.message_id,
    ...(message.from_me ? { from_me: true } : {}),
    ...(message.sender_name !== "" ? { sender_name: message.sender_name } : {}),
    ...(message.deleted ? { deleted: true } : message.text !== "" ? { text: message.text } : {}),
    ...(!message.deleted && message.kind !== "text" ? { kind: message.kind } : {}),
  };
}

/**
 * More reactions, behind "More reactions": common emoji, by theme, each
 * with the name that screen readers say (their Unicode short names).
 */
export const MORE_REACTIONS: readonly (readonly [string, string])[] = [
  ["😀", "grinning face"],
  ["😊", "smiling face with smiling eyes"],
  ["😍", "smiling face with heart-eyes"],
  ["🥰", "smiling face with hearts"],
  ["😘", "face blowing a kiss"],
  ["😎", "smiling face with sunglasses"],
  ["🤔", "thinking face"],
  ["😅", "grinning face with sweat"],
  ["🤣", "rolling on the floor laughing"],
  ["😭", "loudly crying face"],
  ["😡", "enraged face"],
  ["😱", "face screaming in fear"],
  ["🥳", "partying face"],
  ["😴", "sleeping face"],
  ["🙄", "face with rolling eyes"],
  ["🤯", "exploding head"],
  ["👏", "clapping hands"],
  ["🙌", "raising hands"],
  ["👎", "thumbs down"],
  ["👌", "OK hand"],
  ["✌️", "victory hand"],
  ["🤝", "handshake"],
  ["💪", "flexed biceps"],
  ["👀", "eyes"],
  ["🔥", "fire"],
  ["✨", "sparkles"],
  ["🎉", "party popper"],
  ["💯", "hundred points"],
  ["✅", "check mark button"],
  ["❌", "cross mark"],
  ["⭐", "star"],
  ["💔", "broken heart"],
];

/**
 * Whether a typed reaction is one: a short text without spaces at its
 * ends, as networks take for a reaction key (most take emoji only, and
 * refuse the rest: the command then fails).
 */
export function isReactionKey(text: string): boolean {
  return text.length > 0 && text === text.trim() && [...text].length <= 8;
}

/** The reactions offered in a message's actions, one press away. */
export const QUICK_REACTIONS: readonly string[] = ["👍", "❤️", "😂", "😮", "😢", "🙏"];

/**
 * Whether two reaction keys are the same emoji: networks and the core may add
 * the emoji variation selector (U+FE0F) to a key or not.
 */
export function sameEmoji(a: string, b: string): boolean {
  return a.replaceAll("️", "") === b.replaceAll("️", "");
}

/** The user's own reaction with this emoji, as the core keyed it, if any. */
export function ownReaction(message: Message, emoji: string): MessageReaction | undefined {
  return message.reactions?.find((r) => r.from_me === true && sameEmoji(r.key, emoji));
}

/** What the user can do to one message (API 1.8). */
export interface MessageActions {
  react: boolean;
  reply: boolean;
  edit: boolean;
  delete: boolean;
}

/**
 * What the user can do to a message, given what its conversation's network
 * allows: nothing to a deleted message, nor to one that has not reached the
 * network (sending or failed); edits and deletions only to the user's own
 * messages, and edits only to text.
 */
export function messageActions(message: Message, allowed: ConversationActions | undefined): MessageActions {
  // A message waiting for its account (API 1.14) can only be cancelled,
  // on this device, whatever the network allows (API 1.16).
  if (message.status === "queued" && message.from_me && message.deleted !== true && allowed !== undefined) {
    return { react: false, reply: false, edit: false, delete: true };
  }
  const onNetwork = message.status === "sent" || message.status === "received";
  const usable = allowed !== undefined && onNetwork && message.deleted !== true;
  const own = usable && message.from_me;
  return {
    react: usable && allowed.react === true,
    reply: usable && allowed.reply === true,
    edit: own && allowed.edit === true && (message.kind === "text" || message.kind === "emote"),
    delete: own && allowed.delete === true,
  };
}

/** Whether a message offers any action. */
export function hasActions(actions: MessageActions): boolean {
  return actions.react || actions.reply || actions.edit || actions.delete;
}

/** "Alice is typing…", "Alice and Bob are typing…", "Several people are typing…". */
export function typingText(names: readonly string[]): string {
  const named = names.filter((n) => n !== "");
  if (names.length === 0) {
    return "";
  }
  if (names.length > 2 || named.length < names.length) {
    return names.length === 1 ? "Someone is typing…" : "Several people are typing…";
  }
  return names.length === 1 ? `${named[0]} is typing…` : `${named[0]} and ${named[1]} are typing…`;
}

/**
 * What the user's latest read message says after "Sent" (API 1.21):
 * "Seen" in a direct conversation, "Seen by Alice and Bob" in a group.
 */
export function seenLabel(names: readonly string[], group: boolean): string {
  if (!group || names.length === 0) {
    return "Seen";
  }
  const named = names.filter((n) => n !== "");
  if (named.length < names.length || names.length > 3) {
    return `Seen by ${names.length} ${names.length === 1 ? "person" : "people"}`;
  }
  return `Seen by ${named.length === 1 ? named[0] : `${named.slice(0, -1).join(", ")} and ${named.at(-1)}`}`;
}

