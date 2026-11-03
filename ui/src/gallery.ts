// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

// A conversation's media and files (T2.77): what the device stores, the
// newest first, pictures apart from the rest.

import type { Message } from "./core-api/types.gen";
import { hasMedia } from "./media";

export interface Gallery {
  /** Photos, GIFs and videos, the newest first. */
  pictures: Message[];
  /** Files, audio and voice messages, the newest first. */
  others: Message[];
}

export function gallery(messages: readonly Message[]): Gallery {
  const pictures: Message[] = [];
  const others: Message[] = [];
  for (let i = messages.length - 1; i >= 0; i--) {
    const message = messages[i]!;
    if (!hasMedia(message)) {
      continue;
    }
    (message.kind === "image" || message.kind === "video" ? pictures : others).push(message);
  }
  return { pictures, others };
}
