// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

import { describe, expect, test } from "vitest";
import { announcement, MAX_AGE_MS, messageSummary } from "../src/notification-rules.ts";

const now = 1_800_000_000_000;
const added = (message: object, type = "message.added") =>
  JSON.stringify({
    type,
    data: { message: { conversation_id: "c.1", from_me: false, sender_name: "Alice", timestamp_ms: now - 1000, kind: "text", text: "hi", status: "received", ...message } },
  });

describe("desktop notifications", () => {
  test("announce a recent message from someone else while the app is in the background", () => {
    expect(announcement(added({}), now, false)).toEqual({ conversationId: "c.1", title: "Alice", body: "hi" });
  });

  test("announce nothing else", () => {
    expect(announcement(added({}), now, true)).toBeNull();
    expect(announcement(added({ from_me: true }), now, false)).toBeNull();
    expect(announcement(added({ deleted: true }), now, false)).toBeNull();
    // History that comes in a batch is not news.
    expect(announcement(added({ timestamp_ms: now - MAX_AGE_MS - 1 }), now, false)).toBeNull();
    expect(announcement(added({}, "message.updated"), now, false)).toBeNull();
    expect(announcement("not json", now, false)).toBeNull();
  });

  test("say what a message is, as the previews do", () => {
    expect(messageSummary({ kind: "image", text: "a sunset" })).toBe("Photo: a sunset");
    expect(messageSummary({ kind: "video", text: "", media: { animated: true } })).toBe("GIF");
    expect(messageSummary({ kind: "file", text: "", media: { file_name: "plan.pdf" } })).toBe("File: plan.pdf");
    expect(messageSummary({ kind: "text", text: "" })).toBe("New message");
    expect(announcement(added({ sender_name: "" }), now, false)?.title).toBe("New message");
  });
});
