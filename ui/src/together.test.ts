// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

import { describe, expect, test } from "vitest";
import type { Message } from "./core-api/types.gen";
import { togetherView } from "./together";

function message(id: string, conversation: string, timestamp: number): Message {
  return {
    message_id: id,
    conversation_id: conversation,
    from_me: false,
    sender_name: "Alice",
    timestamp_ms: timestamp,
    kind: "text",
    text: id,
    status: "received",
  };
}

const ids = (messages: readonly Message[]) => messages.map((m) => m.message_id);

describe("togetherView", () => {
  test("interleaves the conversations by time", () => {
    const view = togetherView([
      { conversationId: "tg", messages: [message("t1", "tg", 1), message("t3", "tg", 3)], history: "start" },
      { conversationId: "sg", messages: [message("s2", "sg", 2), message("s3", "sg", 3)], history: "start" },
    ]);
    expect(ids(view.messages)).toEqual(["t1", "s2", "t3", "s3"]);
    expect(view.next).toBeUndefined();
    expect(view.history).toBe("start");
  });

  test("hides what a conversation with more history could still come before", () => {
    const view = togetherView([
      // Read down to 10, more before: nothing older than 10 shows yet.
      { conversationId: "tg", messages: [message("t10", "tg", 10), message("t20", "tg", 20)], history: { cursor: "k" } },
      { conversationId: "sg", messages: [message("s5", "sg", 5), message("s15", "sg", 15)], history: "network" },
      { conversationId: "mx", messages: [message("m1", "mx", 1)], history: "start" },
    ]);
    expect(ids(view.messages)).toEqual(["t10", "s15", "t20"]);
    // The conversation that hides the most is read next.
    expect(view.next).toBe("tg");
    expect(view.history).toEqual({ cursor: "tg" });
  });

  test("reads a conversation with nothing read but more history first", () => {
    const view = togetherView([
      { conversationId: "tg", messages: [message("t10", "tg", 10)], history: { cursor: "k" } },
      { conversationId: "sg", messages: [], history: "network" },
    ]);
    expect(view.next).toBe("sg");
    expect(ids(view.messages)).toEqual(["t10"]);
  });

  test("waits while a conversation is read", () => {
    const view = togetherView([
      { conversationId: "tg", messages: [message("t10", "tg", 10)], history: { cursor: "k" } },
      { conversationId: "sg", messages: undefined, history: undefined },
    ]);
    expect(view.history).toBe("loading");
    expect(view.next).toBeUndefined();
  });
});
