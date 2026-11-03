// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

import { expect, test } from "vitest";
import { hueOf, initials } from "./avatar";

test("initials: two words, one word, symbols and nothing", () => {
  expect(initials("Instant Echo")).toBe("IE");
  expect(initials("alice")).toBe("AL");
  expect(initials("  Ørsted   ally ")).toBe("ØA");
  expect(initials("🎉 party people")).toBe("PP");
  expect(initials("(Bob) Smith")).toBe("BS");
  expect(initials("")).toBe("?");
  expect(initials("   ")).toBe("?");
});

test("hueOf: the same name, the same hue, within the circle", () => {
  expect(hueOf("Instant Echo")).toBe(hueOf("Instant Echo"));
  for (const name of ["", "a", "Instant Echo", "Ørsted", "🎉"]) {
    expect(hueOf(name)).toBeGreaterThanOrEqual(0);
    expect(hueOf(name)).toBeLessThan(360);
  }
});
