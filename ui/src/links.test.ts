// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

import { describe, expect, test } from "vitest";
import { linkParts, linkTarget } from "./links";

describe("linkParts", () => {
  test("finds web addresses in a text, and gives the text back whole", () => {
    const text = "See https://example.org/a?b=1, or www.example.com. Thanks!";
    const parts = linkParts(text);
    expect(parts).toEqual([
      { text: "See " },
      { text: "https://example.org/a?b=1", url: "https://example.org/a?b=1" },
      { text: ", or " },
      { text: "www.example.com", url: "https://www.example.com/" },
      { text: ". Thanks!" },
    ]);
    expect(parts.map((p) => p.text).join("")).toBe(text);
  });

  test("keeps balanced parentheses, drops a closing one that ends a sentence", () => {
    expect(linkParts("(see https://en.wikipedia.org/wiki/Bee_(disambiguation))")).toEqual([
      { text: "(see " },
      { text: "https://en.wikipedia.org/wiki/Bee_(disambiguation)", url: "https://en.wikipedia.org/wiki/Bee_(disambiguation)" },
      { text: ")" },
    ]);
  });

  test("links nothing else", () => {
    expect(linkParts("no link here")).toEqual([{ text: "no link here" }]);
    expect(linkParts("javascript:alert(1) file:///etc/passwd ftp://x.org")).toEqual([{ text: "javascript:alert(1) file:///etc/passwd ftp://x.org" }]);
    expect(linkParts("")).toEqual([]);
  });
});

test("linkTarget: http and https only", () => {
  expect(linkTarget("https://example.org")).toBe("https://example.org/");
  expect(linkTarget("www.example.org")).toBe("https://www.example.org/");
  expect(linkTarget("https://")).toBeNull();
});
