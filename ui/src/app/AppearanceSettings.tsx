// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

import { useEffect, useRef, useState } from "react";
import {
  ACCENT_NAMES,
  ACCENTS,
  applyAppearance,
  loadAppearance,
  pageStorage,
  saveAppearance,
  TEXT_SIZE_NAMES,
  TEXT_SIZES,
  THEME_NAMES,
  THEMES,
  type Appearance,
} from "../appearance";

/**
 * The appearance settings, in a modal dialog (docs/ADR/0040): theme,
 * accent color and text size, each a group of radio buttons that applies
 * at once, so that the user sees the change behind the dialog. Escape and
 * Done close it.
 */
export function AppearanceSettings({ onClose }: { onClose: () => void }) {
  const dialog = useRef<HTMLDialogElement>(null);
  const [appearance, setAppearance] = useState<Appearance>(() => loadAppearance(pageStorage()));

  useEffect(() => {
    const element = dialog.current;
    // No close() on cleanup: it fires "close", which would end the dialog
    // when React runs the effect twice (StrictMode). Leaving the DOM ends
    // the modal state.
    if (element !== null && !element.open) {
      element.showModal();
    }
  }, []);

  function change(next: Appearance) {
    setAppearance(next);
    applyAppearance(document.documentElement, next);
    saveAppearance(pageStorage(), next);
  }

  return (
    <dialog ref={dialog} className="settings" aria-labelledby="settings-title" onClose={onClose}>
      <h2 id="settings-title">Appearance</h2>
      <fieldset>
        <legend>Theme</legend>
        {THEMES.map((theme) => (
          <label key={theme} className="settings-choice">
            <input type="radio" name="theme" value={theme} checked={appearance.theme === theme} onChange={() => change({ ...appearance, theme })} />
            {THEME_NAMES[theme]}
          </label>
        ))}
      </fieldset>
      <fieldset>
        <legend>Accent color</legend>
        {ACCENTS.map((accent) => (
          <label key={accent} className="settings-choice">
            <input type="radio" name="accent" value={accent} checked={appearance.accent === accent} onChange={() => change({ ...appearance, accent })} />
            <span className="accent-swatch" data-accent={accent} aria-hidden="true" />
            {ACCENT_NAMES[accent]}
          </label>
        ))}
      </fieldset>
      <fieldset>
        <legend>Text size</legend>
        {TEXT_SIZES.map((textSize) => (
          <label key={textSize} className="settings-choice">
            <input
              type="radio"
              name="text-size"
              value={textSize}
              checked={appearance.textSize === textSize}
              onChange={() => change({ ...appearance, textSize })}
            />
            {TEXT_SIZE_NAMES[textSize]}
          </label>
        ))}
      </fieldset>
      <div className="settings-buttons">
        <button type="button" onClick={() => dialog.current?.close()}>
          Done
        </button>
      </div>
    </dialog>
  );
}
