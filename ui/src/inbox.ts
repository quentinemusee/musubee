// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

// The unified inbox (docs/ADR/0021): the conversations of every account,
// merged per person.

import type { Conversation, Message, Person } from "./core-api/types.gen";

/** One row of the inbox: a person, with all their conversations, or a conversation linked to no one. */
export interface InboxEntry {
  /** Stable across updates: "p." and the person ID, or "c." and the conversation ID. */
  key: string;
  name: string;
  person?: Person;
  /** At least one; the most recently active first. */
  conversations: readonly Conversation[];
  /** The last message of the most recently active conversation that has one. */
  lastMessage?: Message;
}

/**
 * Merges the conversations, the most recently active first, into inbox
 * entries in the same order: a person's entry takes the place of their most
 * recently active conversation. A conversation whose person is not known
 * (yet) stays an entry of its own.
 */
export function inboxEntries(conversations: readonly Conversation[], persons: readonly Person[]): InboxEntry[] {
  const personsById = new Map(persons.map((p) => [p.person_id, p]));
  const entries: InboxEntry[] = [];
  const byPerson = new Map<string, { entry: InboxEntry; conversations: Conversation[] }>();
  for (const conversation of conversations) {
    const person = conversation.person_id === undefined ? undefined : personsById.get(conversation.person_id);
    if (person === undefined) {
      entries.push({
        key: `c.${conversation.conversation_id}`,
        name: conversation.name || "Unnamed conversation",
        conversations: [conversation],
        ...(conversation.last_message ? { lastMessage: conversation.last_message } : {}),
      });
      continue;
    }
    const known = byPerson.get(person.person_id);
    if (known !== undefined) {
      known.conversations.push(conversation);
      if (known.entry.lastMessage === undefined && conversation.last_message !== undefined) {
        known.entry.lastMessage = conversation.last_message;
      }
      continue;
    }
    const merged: Conversation[] = [conversation];
    const entry: InboxEntry = {
      key: `p.${person.person_id}`,
      name: person.name,
      person,
      conversations: merged,
      ...(conversation.last_message ? { lastMessage: conversation.last_message } : {}),
    };
    byPerson.set(person.person_id, { entry, conversations: merged });
    entries.push(entry);
  }
  return entries;
}

/** The entry that holds a conversation, if any. */
export function entryOf(entries: readonly InboxEntry[], conversationId: string | undefined): InboxEntry | undefined {
  return conversationId === undefined ? undefined : entries.find((e) => e.conversations.some((c) => c.conversation_id === conversationId));
}

/** The last message of an entry, in one line. */
export function preview(entry: InboxEntry): string {
  const message = entry.lastMessage;
  if (message === undefined) {
    return "No messages yet";
  }
  const conversation = entry.conversations.find((c) => c.conversation_id === message.conversation_id);
  let sender = "";
  if (message.from_me) {
    sender = "You: ";
  } else if (conversation?.kind === "group" && message.sender_name !== "") {
    sender = `${message.sender_name}: `;
  }
  return sender + message.text;
}

const timeFormat = new Intl.DateTimeFormat(undefined, { hour: "2-digit", minute: "2-digit" });
const dayFormat = new Intl.DateTimeFormat(undefined, { day: "numeric", month: "short" });
const dateFormat = new Intl.DateTimeFormat(undefined, { day: "numeric", month: "short", year: "numeric" });

/** When an entry was last active: the time today, the day this year, the date before. */
export function activityLabel(ms: number, now: number): string {
  const date = new Date(ms);
  const today = new Date(now);
  if (date.toDateString() === today.toDateString()) {
    return timeFormat.format(date);
  }
  return date.getFullYear() === today.getFullYear() ? dayFormat.format(date) : dateFormat.format(date);
}
