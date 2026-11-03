// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

import { describe, expect, test } from "vitest";
import type { Message } from "./core-api/types.gen";
import { hasActions, messageActions, ownReaction, reactionLabel, replyQuote, replyTo, sameEmoji } from "./message";

describe("reactionLabel", () => {
  test("says who reacted", () => {
    expect(reactionLabel({ key: "👍", count: 1 })).toBe("👍: 1 person");
    expect(reactionLabel({ key: "👍", count: 3 })).toBe("👍: 3 people");
    expect(reactionLabel({ key: "🎉", count: 1, from_me: true })).toBe("🎉: you");
    expect(reactionLabel({ key: "🎉", count: 2, from_me: true })).toBe("🎉: you and 1 other");
    expect(reactionLabel({ key: "🎉", count: 4, from_me: true })).toBe("🎉: you and 3 others");
  });
});

describe("replyQuote", () => {
  test("names who reacted when the core says (API 1.19)", () => {
    expect(reactionLabel({ key: "🎉", count: 1, names: ["Alice"] })).toBe("🎉: Alice");
    expect(reactionLabel({ key: "🎉", count: 2, from_me: true, names: ["Alice"] })).toBe("🎉: you and Alice");
    expect(reactionLabel({ key: "🎉", count: 4, from_me: true, names: ["Alice", "Bob"] })).toBe("🎉: you, Alice, Bob and 1 other");
    expect(reactionLabel({ key: "🎉", count: 12, names: ["Alice"] })).toBe("🎉: Alice and 11 others");
  });

  test("quotes the sender and the text", () => {
    expect(replyQuote({ message_id: "m1", sender_name: "Bob", text: "Hi" })).toEqual({ sender: "Bob", text: "Hi" });
    expect(replyQuote({ message_id: "m1", from_me: true, sender_name: "Me", text: "Hi" })).toEqual({ sender: "You", text: "Hi" });
  });

  test("says when the message is gone or not on the device", () => {
    expect(replyQuote({ message_id: "m1", sender_name: "Bob", deleted: true })).toEqual({ sender: "Bob", text: "Message deleted" });
    expect(replyQuote({ message_id: "m1" })).toEqual({ sender: "", text: "An earlier message" });
  });
});

describe("replyTo", () => {
  const message: Message = {
    message_id: "m1",
    conversation_id: "c1",
    from_me: false,
    sender_name: "Bob",
    timestamp_ms: 1,
    kind: "text",
    text: "Hi",
    status: "received",
  };

  test("quotes a message as the core does", () => {
    expect(replyTo(message)).toEqual({ message_id: "m1", sender_name: "Bob", text: "Hi" });
    expect(replyTo({ ...message, from_me: true, sender_name: "" })).toEqual({ message_id: "m1", from_me: true, text: "Hi" });
    expect(replyTo({ ...message, text: "", deleted: true })).toEqual({ message_id: "m1", sender_name: "Bob", deleted: true });
  });
});

describe("messageActions", () => {
  const all = { reply: true, react: true, edit: true, delete: true };
  const theirs: Message = {
    message_id: "m1",
    conversation_id: "c1",
    from_me: false,
    sender_name: "Bob",
    timestamp_ms: 1,
    kind: "text",
    text: "Hi",
    status: "received",
  };
  const mine: Message = { ...theirs, message_id: "m2", from_me: true, sender_name: "", status: "sent" };

  test("offers what the network allows", () => {
    expect(messageActions(mine, all)).toEqual(all);
    expect(messageActions(mine, { react: true })).toEqual({ reply: false, react: true, edit: false, delete: false });
    expect(hasActions(messageActions(mine, undefined))).toBe(false);
  });

  test("edits and deletes the user's own messages only, and edits text only", () => {
    expect(messageActions(theirs, all)).toEqual({ reply: true, react: true, edit: false, delete: false });
    expect(messageActions({ ...mine, kind: "notice" }, all)).toEqual({ reply: true, react: true, edit: false, delete: true });
  });

  test("offers nothing on a deleted message or one not on the network", () => {
    expect(hasActions(messageActions({ ...theirs, deleted: true, text: "" }, all))).toBe(false);
    expect(hasActions(messageActions({ ...mine, status: "sending" }, all))).toBe(false);
    expect(hasActions(messageActions({ ...mine, status: "failed" }, all))).toBe(false);
  });
});

describe("ownReaction", () => {
  test("offers to cancel a waiting message only, whatever the network allows (API 1.16)", () => {
    const waiting: Message = { message_id: "m.q", conversation_id: "c.1", from_me: true, sender_name: "", timestamp_ms: 1, kind: "text", text: "on the train", status: "queued" };
    expect(messageActions(waiting, {})).toEqual({ react: false, reply: false, edit: false, delete: true });
    // Without actions (no handlers), nothing.
    expect(messageActions(waiting, undefined)).toEqual({ react: false, reply: false, edit: false, delete: false });
    expect(messageActions({ ...waiting, from_me: false }, {}).delete).toBe(false);
  });

  test("finds the user's reaction whatever its variation selector", () => {
    const message = {
      message_id: "m1",
      conversation_id: "c1",
      from_me: false,
      sender_name: "Bob",
      timestamp_ms: 1,
      kind: "text",
      text: "Hi",
      status: "received",
      reactions: [
        { key: "👍️", count: 2, from_me: true },
        { key: "😂", count: 1 },
      ],
    } satisfies Message;
    expect(ownReaction(message, "👍")?.key).toBe("👍️");
    expect(ownReaction(message, "😂")).toBeUndefined();
    expect(sameEmoji("❤️", "❤")).toBe(true);
    expect(sameEmoji("❤️", "👍")).toBe(false);
  });
});
