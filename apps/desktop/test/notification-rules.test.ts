// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

import { describe, expect, test } from "vitest";
import { announcement, deletedConversation, hide, KnownConversations, MAX_AGE_MS, messageSummary, parsePreview } from "../src/notification-rules.ts";

const now = 1_800_000_000_000;
const added = (message: object, type = "message.added") =>
  JSON.stringify({
    type,
    data: { message: { conversation_id: "c.1", from_me: false, sender_name: "Alice", timestamp_ms: now - 1000, kind: "text", text: "hi", status: "received", ...message } },
  });

const updated = (conversation: object) =>
  JSON.stringify({ type: "conversation.updated", data: { conversation: { conversation_id: "c.1", name: "Alice", kind: "direct", ...conversation } } });

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

  test("stay quiet in a muted conversation, and name the group (ADR 0050)", () => {
    const known = new KnownConversations();
    expect(announcement(added({}), now, false, known)?.title).toBe("Alice");
    known.learnEvent(updated({ muted_until_ms: -1 }));
    expect(announcement(added({}), now, false, known)).toBeNull();
    known.learnEvent(updated({ muted_until_ms: now + 1000 }));
    expect(announcement(added({}), now, false, known)).toBeNull();
    // A mention gets through (ADR 0055).
    expect(announcement(added({ mentions_me: true }), now, false, known)?.body).toBe("hi");
    // A mute that ended.
    known.learnEvent(updated({ muted_until_ms: now - 1000 }));
    expect(announcement(added({}), now, false, known)).not.toBeNull();
    known.learnEvent(updated({}));
    expect(announcement(added({}), now, false, known)).not.toBeNull();

    // From the core's answers to the interface.
    known.learnResponse(JSON.stringify({ id: 4, result: { conversations: [{ conversation_id: "c.1", name: "Book club", kind: "group", muted_until_ms: -1 }] } }));
    expect(announcement(added({}), now, false, known)).toBeNull();
    known.learnResponse(JSON.stringify({ id: 5, result: { conversation: { conversation_id: "c.1", name: "Book club", kind: "group" } } }));
    expect(announcement(added({}), now, false, known)?.title).toBe("Alice in Book club");
    known.learnResponse("not json");
    known.learnResponse(JSON.stringify({ id: 6, error: { code: "not_found", message: "no" } }));
    expect(announcement(added({}), now, false, known)?.title).toBe("Alice in Book club");
  });

  test("hide the text, or the names too, as the user chose (ADR 0051)", () => {
    const announced = { conversationId: "c.1", title: "Alice in Book club", body: "secret plans" };
    expect(hide(announced, "name_and_text")).toEqual(announced);
    expect(hide(announced, "name_only")).toEqual({ conversationId: "c.1", title: "Alice in Book club", body: "New message" });
    expect(hide(announced, "nothing")).toEqual({ conversationId: "c.1", title: "Musubee", body: "New message" });
    expect(parsePreview("nothing")).toBe("nothing");
    expect(parsePreview("everything")).toBeNull();
    expect(parsePreview(undefined)).toBeNull();
  });

  test("know which conversation was deleted", () => {
    expect(deletedConversation(JSON.stringify({ type: "conversation.deleted", data: { conversation_id: "c.1" } }))).toBe("c.1");
    expect(deletedConversation(added({}))).toBeNull();
    expect(deletedConversation("not json")).toBeNull();
  });

  test("count the unread messages for the app's icon, without muted conversations (ADR 0059)", () => {
    const known = new KnownConversations();
    expect(known.unreadTotal(now)).toBe(0);
    known.learnResponse(
      JSON.stringify({
        id: 1,
        result: {
          conversations: [
            { conversation_id: "c.1", name: "Alice", kind: "direct", unread_count: 3 },
            { conversation_id: "c.2", name: "Bob", kind: "direct", marked_unread: true },
            { conversation_id: "c.3", name: "Club", kind: "group", unread_count: 40, muted_until_ms: -1 },
            { conversation_id: "c.4", name: "Cy", kind: "direct" },
          ],
        },
      }),
    );
    expect(known.unreadTotal(now)).toBe(4);
    known.learnEvent(updated({ unread_count: 0 }));
    expect(known.unreadTotal(now)).toBe(1);
    known.forget("c.2");
    expect(known.unreadTotal(now)).toBe(0);
  });
});
