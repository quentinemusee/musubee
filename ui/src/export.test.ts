// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

import { expect, test } from "vitest";
import type { Message } from "./core-api/types.gen";
import { exportFileName, exportLine, exportText } from "./export";

const base: Message = {
  message_id: "m1",
  conversation_id: "c1",
  from_me: false,
  sender_name: "Alice",
  timestamp_ms: new Date(2026, 10, 3, 14, 5).getTime(),
  kind: "text",
  text: "hello\nthere",
  status: "received",
};

test("a line per message, the text's own lines indented", () => {
  expect(exportLine(base)).toBe("2026-11-03 14:05 Alice: hello\n    there");
  expect(exportLine({ ...base, from_me: true, text: "hi" })).toBe("2026-11-03 14:05 You: hi");
  expect(exportLine({ ...base, deleted: true, text: "" })).toBe("2026-11-03 14:05 Alice: (deleted)");
  expect(exportLine({ ...base, kind: "image", text: "", media: { media_id: "f", mime_type: "image/png" } })).toBe("2026-11-03 14:05 Alice: Photo");
});

test("the whole export, and its file name", () => {
  expect(exportText("Alice", [{ ...base, text: "hi" }])).toBe("Alice\n\n2026-11-03 14:05 Alice: hi\n");
  expect(exportFileName("Book club: 2026/11")).toBe("Book club_ 2026_11.txt");
  expect(exportFileName("  ")).toBe("conversation.txt");
});
