// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

import { expect, test } from "vitest";
import type { Message, MessageKind } from "./core-api/types.gen";
import { gallery } from "./gallery";

const item = (id: string, kind: MessageKind, deleted = false): Message => ({
  message_id: id,
  conversation_id: "c1",
  from_me: false,
  sender_name: "",
  timestamp_ms: 1,
  kind,
  text: "",
  status: "received",
  ...(kind === "text" ? {} : { media: { media_id: `f${id}`, mime_type: "application/octet-stream" } }),
  ...(deleted ? { deleted: true } : {}),
});

test("pictures apart from files, the newest first, without text or deleted messages", () => {
  const messages = [item("1", "image"), item("2", "text"), item("3", "file"), item("4", "video"), item("5", "voice"), item("6", "image", true)];
  const { pictures, others } = gallery(messages);
  expect(pictures.map((m) => m.message_id)).toEqual(["4", "1"]);
  expect(others.map((m) => m.message_id)).toEqual(["5", "3"]);
});
