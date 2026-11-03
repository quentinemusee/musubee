// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

import { expect, test } from "vitest";
import { ACCENTS_KEY, accentKey, loadAccents, parseAccents, saveAccent } from "./conversation-accent";

test("a thread's accent belongs to its person, else its conversation", () => {
  expect(accentKey("c1", undefined)).toBe("c.c1");
  expect(accentKey("c1", "p9")).toBe("p.p9");
});

test("stored accents keep known keys and colors only", () => {
  expect(parseAccents(null)).toEqual({});
  expect(parseAccents("not json")).toEqual({});
  expect(parseAccents(JSON.stringify({ "c.c1": "teal", "p.p2": "rose", "c.c3": "neon", other: "teal" }))).toEqual({ "c.c1": "teal", "p.p2": "rose" });
});

test("accents are saved, replaced and forgotten, and survive a storage that throws", () => {
  const stored = new Map<string, string>();
  const storage = { getItem: (key: string) => stored.get(key) ?? null, setItem: (key: string, value: string) => void stored.set(key, value) };
  let accents = saveAccent(storage, {}, "c.c1", "teal");
  accents = saveAccent(storage, accents, "p.p2", "amber");
  accents = saveAccent(storage, accents, "c.c1", undefined);
  expect(accents).toEqual({ "p.p2": "amber" });
  expect(loadAccents(storage)).toEqual({ "p.p2": "amber" });
  expect(JSON.parse(stored.get(ACCENTS_KEY)!)).toEqual({ "p.p2": "amber" });

  const broken = {
    getItem: () => {
      throw new Error("denied");
    },
    setItem: () => {
      throw new Error("full");
    },
  };
  expect(loadAccents(broken)).toEqual({});
  expect(saveAccent(broken, {}, "c.c1", "rose")).toEqual({ "c.c1": "rose" });
});
