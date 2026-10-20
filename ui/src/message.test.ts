// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

import { describe, expect, test } from "vitest";
import type { Message } from "./core-api/types.gen";
import { reactionLabel, replyQuote, replyTo } from "./message";

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
