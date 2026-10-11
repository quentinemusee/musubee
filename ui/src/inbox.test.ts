// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

import { describe, expect, test } from "vitest";
import type { Conversation, Message, Person } from "./core-api/types.gen";
import { activityLabel, entryOf, inboxEntries, preview, unreadBadge, type InboxEntry } from "./inbox";

function conversation(id: string, name: string, activity: number, patch: Partial<Conversation> = {}): Conversation {
  return { conversation_id: id, account_id: "a1", network_id: "echo", name, kind: "direct", activity_ms: activity, ...patch };
}

function message(id: string, conversationId: string): Message {
  return {
    message_id: id,
    conversation_id: conversationId,
    from_me: false,
    sender_name: "",
    timestamp_ms: 1,
    kind: "text",
    text: id,
    status: "received",
  };
}

describe("inboxEntries", () => {
  test("merges a person's conversations at the place of the most recent one", () => {
    const alice: Person = { person_id: "h1", name: "Alice", conversation_ids: ["c2", "c4"] };
    const conversations = [
      conversation("c1", "Bob", 50, { last_message: message("m1", "c1") }),
      conversation("c2", "alice (Echo)", 40, { person_id: "h1" }),
      conversation("c3", "Family", 30, { kind: "group" }),
      conversation("c4", "Alice T.", 20, { network_id: "telegram", person_id: "h1", last_message: message("m4", "c4") }),
      conversation("c5", "", 10),
    ];
    const entries = inboxEntries(conversations, [alice]);
    expect(entries.map((e) => `${e.key}:${e.name}:${e.conversations.map((c) => c.conversation_id).join("+")}`)).toEqual([
      "c.c1:Bob:c1",
      "p.h1:Alice:c2+c4",
      "c.c3:Family:c3",
      "c.c5:Unnamed conversation:c5",
    ]);
    // The most recent conversation has no message yet: the entry shows the next one's.
    expect(entries[1]?.lastMessage?.message_id).toBe("m4");
    expect(entries[0]?.lastMessage?.message_id).toBe("m1");
    expect(entries[2]?.lastMessage).toBeUndefined();
    expect(entryOf(entries, "c4")?.key).toBe("p.h1");
    expect(entryOf(entries, undefined)).toBeUndefined();
  });

  test("adds up the unread messages of a person's conversations", () => {
    const alice: Person = { person_id: "h1", name: "Alice", conversation_ids: ["c1", "c2", "c3"] };
    const entries = inboxEntries(
      [
        conversation("c1", "alice", 30, { person_id: "h1", unread_count: 2 }),
        conversation("c2", "Alice T.", 20, { person_id: "h1", unread_count: 3 }),
        conversation("c3", "Alice S.", 10, { person_id: "h1", marked_unread: true }),
        conversation("c4", "Bob", 5),
      ],
      [alice],
    );
    expect(entries.map((e) => [e.key, e.unread, e.markedUnread])).toEqual([
      ["p.h1", 5, true],
      ["c.c4", 0, false],
    ]);
  });

  test("keeps a conversation of an unknown person on its own", () => {
    const entries = inboxEntries([conversation("c1", "Alice", 1, { person_id: "h9" })], []);
    expect(entries.map((e) => e.key)).toEqual(["c.c1"]);
    expect(entries[0]?.person).toBeUndefined();
  });
});

function entry(kind: Conversation["kind"], last?: Partial<Message>): InboxEntry {
  const c = conversation("c1", "Chat", 1, { kind });
  return {
    key: "c.c1",
    name: "Chat",
    conversations: [c],
    ...(last ? { lastMessage: { ...message("m1", "c1"), text: "Hi", ...last } } : {}),
    unread: 0,
    markedUnread: false,
  };
}

describe("preview", () => {
  test("names the sender where it helps", () => {
    expect(preview(entry("direct"))).toBe("No messages yet");
    expect(preview(entry("direct", { sender_name: "Bob" }))).toBe("Hi");
    expect(preview(entry("direct", { from_me: true }))).toBe("You: Hi");
    expect(preview(entry("group", { sender_name: "Bob" }))).toBe("Bob: Hi");
    expect(preview(entry("group", { from_me: true, sender_name: "Me" }))).toBe("You: Hi");
  });
});

describe("unreadBadge", () => {
  test("shows the count, 99+ beyond, a dot when only marked unread", () => {
    expect(unreadBadge(entry("direct"))).toBeUndefined();
    expect(unreadBadge({ ...entry("direct"), unread: 1 })).toEqual({ text: "1", label: "1 unread message" });
    expect(unreadBadge({ ...entry("direct"), unread: 42, markedUnread: true })).toEqual({ text: "42", label: "42 unread messages" });
    expect(unreadBadge({ ...entry("direct"), unread: 1000 })).toEqual({ text: "99+", label: "99+ unread messages" });
    expect(unreadBadge({ ...entry("direct"), markedUnread: true })).toEqual({ text: "", label: "Marked as unread" });
  });
});

describe("activityLabel", () => {
  const now = new Date(2026, 9, 10, 18, 0).getTime();
  const time = new Intl.DateTimeFormat(undefined, { hour: "2-digit", minute: "2-digit" });

  test("shows the time today, the day this year, the date before", () => {
    const today = new Date(2026, 9, 10, 9, 5);
    expect(activityLabel(today.getTime(), now)).toBe(time.format(today));
    const thisYear = activityLabel(new Date(2026, 2, 3, 9, 5).getTime(), now);
    expect(thisYear).not.toBe(time.format(new Date(2026, 2, 3, 9, 5)));
    expect(thisYear).not.toContain("2026");
    expect(activityLabel(new Date(2024, 2, 3, 9, 5).getTime(), now)).toContain("2024");
  });
});
