// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

import { describe, expect, test } from "vitest";
import type { Message } from "./core-api/types.gen";
import { canForward, hasActions, isReactionKey, seenLabel, typingText, messageActions, MORE_REACTIONS, ownReaction, reactionLabel, replyQuote, replyTo, sameEmoji } from "./message";

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
  test("says who saw the user's message (API 1.21)", () => {
    expect(seenLabel(["Alice"], false)).toBe("Seen");
    expect(seenLabel(["Alice"], true)).toBe("Seen by Alice");
    expect(seenLabel(["Alice", "Bob", "Carol"], true)).toBe("Seen by Alice, Bob and Carol");
    expect(seenLabel(["Alice", "", "Bob"], true)).toBe("Seen by 3 people");
    expect(seenLabel(["a", "b", "c", "d"], true)).toBe("Seen by 4 people");
  });

  test("says who is typing (API 1.20)", () => {
    expect(typingText([])).toBe("");
    expect(typingText(["Alice"])).toBe("Alice is typing…");
    expect(typingText(["Alice", "Bob"])).toBe("Alice and Bob are typing…");
    expect(typingText(["Alice", "Bob", "Carol"])).toBe("Several people are typing…");
    expect(typingText([""])).toBe("Someone is typing…");
  });

  test("takes a typed reaction when it is short and trimmed", () => {
    expect(isReactionKey("🦄")).toBe(true);
    expect(isReactionKey("👍🏽")).toBe(true);
    expect(isReactionKey("")).toBe(false);
    expect(isReactionKey(" 🦄")).toBe(false);
    expect(isReactionKey("a very long reaction")).toBe(false);
    expect(new Set(MORE_REACTIONS.map(([emoji]) => emoji)).size).toBe(MORE_REACTIONS.length);
  });

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

test("canForward: a message on its network, not deleted, with text or media", () => {
  const sent: Message = { message_id: "m1", conversation_id: "c1", from_me: true, sender_name: "", timestamp_ms: 1, kind: "text", text: "hi", status: "sent" };
  expect(canForward(sent)).toBe(true);
  expect(canForward({ ...sent, from_me: false, status: "received" })).toBe(true);
  expect(canForward({ ...sent, status: "sending" })).toBe(false);
  expect(canForward({ ...sent, status: "queued" })).toBe(false);
  expect(canForward({ ...sent, deleted: true, text: "" })).toBe(false);
  expect(canForward({ ...sent, text: "" })).toBe(false);
  expect(canForward({ ...sent, kind: "image", text: "", media: { media_id: "f1", mime_type: "image/png" } })).toBe(true);
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
