// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

import { use, type CSSProperties } from "react";
import { hueOf, initials } from "../avatar";
import { useMediaURL } from "./MessageMedia";
import { StoreContext } from "./state";

/**
 * A conversation's picture (API 1.32, docs/ADR/0061), or its initials on
 * the name's hue. Decorative: the name is always written next to it.
 */
export function Avatar({ mediaId, name, size = "normal" }: { mediaId: string | undefined; name: string; size?: "normal" | "small" }) {
  const cache = use(StoreContext)?.media;
  // No type: the picture's own (PNG, JPEG, WebP…) is sniffed by the image.
  const loaded = useMediaURL(cache, mediaId, "", mediaId !== undefined);
  return (
    <span className="avatar" data-size={size} aria-hidden="true" style={{ "--avatar-hue": hueOf(name) } as CSSProperties}>
      {loaded.state === "ready" ? <img src={loaded.url} alt="" /> : initials(name)}
    </span>
  );
}
