// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

import { describe, expect, test } from "vitest";
import { isMuted, MUTE_CHOICES, MUTED_FOREVER, mutedLabel, muteUntil } from "./mute";

const now = new Date(2026, 10, 3, 10, 0).getTime();

describe("mute", () => {
  test("a mute ends, or lasts until unmuted", () => {
    expect(isMuted({}, now)).toBe(false);
    expect(isMuted({ muted_until_ms: MUTED_FOREVER }, now)).toBe(true);
    expect(isMuted({ muted_until_ms: now + 1 }, now)).toBe(true);
    expect(isMuted({ muted_until_ms: now }, now)).toBe(false);
    expect(isMuted({ muted_until_ms: now - 1 }, now)).toBe(false);
  });

  test("the choices give their end", () => {
    expect(MUTE_CHOICES.map((c) => muteUntil(c.durationMs, now))).toEqual([now + 3_600_000, now + 8 * 3_600_000, now + 7 * 24 * 3_600_000, MUTED_FOREVER]);
  });

  test("the label says until when", () => {
    expect(mutedLabel(MUTED_FOREVER, now, "en-GB")).toBe("Muted");
    expect(mutedLabel(new Date(2026, 10, 3, 14, 30).getTime(), now, "en-GB")).toBe("Muted until 14:30");
    // 4 November 2026 is a Wednesday.
    expect(mutedLabel(new Date(2026, 10, 4, 9, 5).getTime(), now, "en-GB")).toBe("Muted until Wed 09:05");
    expect(mutedLabel(new Date(2026, 10, 10, 10, 0).getTime(), now, "en-GB")).toBe("Muted until 10 Nov 10:00");
  });
});
