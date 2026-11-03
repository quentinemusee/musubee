// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

import { describe, expect, test } from "vitest";
import { forwardWarning, privacyWords } from "./privacy";

describe("privacyWords", () => {
  test("says end-to-end encryption, and from where Musubee connects", () => {
    const words = privacyWords({ level: "end_to_end" }, "Signal", "on_device");
    expect(words).toMatchObject({ safe: true, label: "End-to-end encrypted" });
    expect(words.detail).toContain("from this device");
    // Through a hosted bridge, it is not as safe as it sounds.
    expect(privacyWords({ level: "end_to_end" }, "Signal", "hosted_bridge")).toMatchObject({ safe: false });
  });

  test("names who else can read", () => {
    const telegram = privacyWords({ level: "network_can_read", readers: "Telegram" }, "Telegram", "on_device");
    expect(telegram).toMatchObject({ safe: false, label: "Not end-to-end encrypted" });
    expect(telegram.detail).toMatch(/^Telegram can read this conversation's messages, as in Telegram's own app\./);
    const matrix = privacyWords({ level: "network_can_read", readers: "the Matrix servers of the room's members" }, "Matrix", undefined);
    expect(matrix.detail).toMatch(/^The Matrix servers of the room's members can read/);
    expect(privacyWords({ level: "bridge_can_read", readers: "bridge.example.org" }, "WhatsApp", "hosted_bridge").detail).toMatch(
      /^Bridge\.example\.org can read/,
    );
  });

  test("never claims more than the core knows", () => {
    for (const privacy of [undefined, { level: "unknown" as const }]) {
      const words = privacyWords(privacy, "Telegram", "on_device");
      expect(words).toMatchObject({ safe: false, label: "Encryption unknown" });
      expect(words.detail).toContain("readable by Telegram");
    }
    expect(privacyWords({ level: "device_only" }, "Echo", "on_device")).toMatchObject({ safe: true, label: "Test conversation" });
  });
});

test("forwardWarning: says who can read a message that leaves an end-to-end encrypted conversation", () => {
  const e2e = { level: "end_to_end" } as const;
  expect(forwardWarning(e2e, { level: "network_can_read", readers: "Telegram" })).toBe("Telegram can read it there");
  expect(forwardWarning(e2e, { level: "bridge_can_read", readers: "the bridge's host" })).toBe("the bridge's host can read it there");
  expect(forwardWarning(e2e, { level: "unknown" })).toBe("it may not stay end-to-end encrypted there");
  expect(forwardWarning(e2e, undefined)).toBe("it may not stay end-to-end encrypted there");
  expect(forwardWarning(e2e, e2e)).toBe("");
  expect(forwardWarning(e2e, { level: "device_only" })).toBe("");
  expect(forwardWarning({ level: "network_can_read", readers: "Telegram" }, { level: "network_can_read", readers: "Discord" })).toBe("");
  expect(forwardWarning(undefined, { level: "network_can_read", readers: "Telegram" })).toBe("");
});
