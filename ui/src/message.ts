// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

// What the interface says about a message beyond its text: its reactions,
// the message it answers, and what replaced it once deleted.

import type { ConversationActions, Message, MessageReaction, MessageReply } from "./core-api/types.gen";

/** What a deleted message shows instead of its text. */
export const DELETED_TEXT = "Message deleted";

/** How a reaction is read out: who reacted, as the chip only shows a count. */
export function reactionLabel(reaction: MessageReaction): string {
  const others = reaction.from_me ? reaction.count - 1 : reaction.count;
  let who: string;
  if (!reaction.from_me) {
    who = others === 1 ? "1 person" : `${others} people`;
  } else if (others === 0) {
    who = "you";
  } else {
    who = `you and ${others === 1 ? "1 other" : `${others} others`}`;
  }
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
  return { sender, text: reply.text ?? "" };
}

/** The reply_to of a message that answers this one, as this one is now. */
export function replyTo(message: Message): MessageReply {
  return {
    message_id: message.message_id,
    ...(message.from_me ? { from_me: true } : {}),
    ...(message.sender_name !== "" ? { sender_name: message.sender_name } : {}),
    ...(message.deleted ? { deleted: true } : message.text !== "" ? { text: message.text } : {}),
  };
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
