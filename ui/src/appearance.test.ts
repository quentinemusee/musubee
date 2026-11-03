// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

import { describe, expect, test } from "vitest";
import { applyAppearance, DEFAULT_APPEARANCE, loadAppearance, parseAppearance, saveAppearance, STORAGE_KEY, type AppearanceStorage } from "./appearance";

function memoryStorage(initial: Record<string, string> = {}) {
  const items = new Map(Object.entries(initial));
  const storage: AppearanceStorage = {
    getItem: (key) => items.get(key) ?? null,
    setItem: (key, value) => void items.set(key, value),
  };
  return { storage, items };
}

const brokenStorage: AppearanceStorage = {
  getItem: () => {
    throw new Error("SecurityError");
  },
  setItem: () => {
    throw new Error("QuotaExceededError");
  },
};

describe("appearance", () => {
  test("reads what it wrote", () => {
    const { storage, items } = memoryStorage();
    expect(loadAppearance(storage)).toEqual(DEFAULT_APPEARANCE);
    saveAppearance(storage, { theme: "dark", accent: "teal", textSize: "larger", density: "compact" });
    expect(JSON.parse(items.get(STORAGE_KEY) ?? "")).toEqual({ theme: "dark", accent: "teal", textSize: "larger", density: "compact" });
    expect(loadAppearance(storage)).toEqual({ theme: "dark", accent: "teal", textSize: "larger", density: "compact" });
  });

  test("falls back to the default, field by field", () => {
    expect(parseAppearance(null)).toEqual(DEFAULT_APPEARANCE);
    expect(parseAppearance("not json")).toEqual(DEFAULT_APPEARANCE);
    expect(parseAppearance("[1, 2]")).toEqual(DEFAULT_APPEARANCE);
    expect(parseAppearance('{"theme": "neon", "accent": "rose", "textSize": 3}')).toEqual({ ...DEFAULT_APPEARANCE, accent: "rose" });
  });

  test("a storage that refuses changes nothing for the run", () => {
    expect(loadAppearance(brokenStorage)).toEqual(DEFAULT_APPEARANCE);
    expect(() => saveAppearance(brokenStorage, DEFAULT_APPEARANCE)).not.toThrow();
    expect(loadAppearance(undefined)).toEqual(DEFAULT_APPEARANCE);
  });

  test("applies as data attributes of the root", () => {
    const root = { dataset: {} as Record<string, string> } as unknown as HTMLElement;
    applyAppearance(root, { theme: "light", accent: "amber", textSize: "small", density: "compact" });
    expect(root.dataset).toEqual({ theme: "light", accent: "amber", textSize: "small", density: "compact" });
  });
});
