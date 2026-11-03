// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

// The appearance the user chose (docs/ADR/0040): theme, accent color and
// text size, for the whole app. It is kept per device in the page's
// storage (nothing in it is private), and applied as data attributes on
// the root element, which styles.css turns into its design tokens.

export const THEMES = ["system", "light", "dark"] as const;
export const ACCENTS = ["indigo", "teal", "rose", "amber", "slate"] as const;
export const TEXT_SIZES = ["small", "normal", "large", "larger"] as const;
export const DENSITIES = ["comfortable", "compact"] as const;

export type Theme = (typeof THEMES)[number];
export type Accent = (typeof ACCENTS)[number];
export type TextSize = (typeof TEXT_SIZES)[number];
export type Density = (typeof DENSITIES)[number];

export interface Appearance {
  theme: Theme;
  accent: Accent;
  textSize: TextSize;
  /** Since T2.41: how much room the lists and bubbles take. */
  density: Density;
}

export const DEFAULT_APPEARANCE: Appearance = { theme: "system", accent: "indigo", textSize: "normal", density: "comfortable" };

/** The names the settings show. */
export const THEME_NAMES: Record<Theme, string> = { system: "Same as the system", light: "Light", dark: "Dark" };
export const ACCENT_NAMES: Record<Accent, string> = { indigo: "Indigo", teal: "Teal", rose: "Rose", amber: "Amber", slate: "Slate" };
export const TEXT_SIZE_NAMES: Record<TextSize, string> = { small: "Small", normal: "Normal", large: "Large", larger: "Larger" };
export const DENSITY_NAMES: Record<Density, string> = { comfortable: "Comfortable", compact: "Compact" };

/** The key of the page's storage that holds the appearance. */
export const STORAGE_KEY = "musubee.appearance";

function oneOf<T extends string>(values: readonly T[], value: unknown, fallback: T): T {
  return values.includes(value as T) ? (value as T) : fallback;
}

/**
 * An appearance from stored JSON: anything missing, unknown or malformed
 * (a newer version's value, a hand edit) falls back to the default.
 */
export function parseAppearance(json: string | null): Appearance {
  let value: unknown;
  try {
    value = json === null ? undefined : JSON.parse(json);
  } catch {
    value = undefined;
  }
  const fields = typeof value === "object" && value !== null ? (value as Record<string, unknown>) : {};
  return {
    theme: oneOf(THEMES, fields["theme"], DEFAULT_APPEARANCE.theme),
    accent: oneOf(ACCENTS, fields["accent"], DEFAULT_APPEARANCE.accent),
    textSize: oneOf(TEXT_SIZES, fields["textSize"], DEFAULT_APPEARANCE.textSize),
    density: oneOf(DENSITIES, fields["density"], DEFAULT_APPEARANCE.density),
  };
}

/** The part of Web Storage this module uses; replaced in the tests. */
export type AppearanceStorage = Pick<Storage, "getItem" | "setItem">;

/** The stored appearance; the default when the storage cannot be read. */
export function loadAppearance(storage: AppearanceStorage | undefined): Appearance {
  try {
    return parseAppearance(storage?.getItem(STORAGE_KEY) ?? null);
  } catch {
    return DEFAULT_APPEARANCE;
  }
}

/** Keeps an appearance; a storage that refuses (full, disabled) keeps it for this run only. */
export function saveAppearance(storage: AppearanceStorage | undefined, appearance: Appearance): void {
  try {
    storage?.setItem(STORAGE_KEY, JSON.stringify(appearance));
  } catch {
    // The appearance still applies until the app closes.
  }
}

/** Applies an appearance to the root element, before the first render and on every change. */
export function applyAppearance(root: HTMLElement, appearance: Appearance): void {
  root.dataset["theme"] = appearance.theme;
  root.dataset["accent"] = appearance.accent;
  root.dataset["textSize"] = appearance.textSize;
  root.dataset["density"] = appearance.density;
}

/** The page's storage, when the page may use it. */
export function pageStorage(): AppearanceStorage | undefined {
  try {
    return window.localStorage;
  } catch {
    return undefined;
  }
}
