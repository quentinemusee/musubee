// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

import { useMemo, type MouseEvent } from "react";
import { linkParts } from "../links";

/**
 * Opens a link through the shell where it has a way (the desktop), and
 * lets the link be followed elsewhere (Android opens web addresses in the
 * system's browser itself).
 */
function openLink(event: MouseEvent<HTMLAnchorElement>, url: string) {
  const links = window.musubee?.links;
  if (links === undefined) {
    return;
  }
  event.preventDefault();
  links.open(url).catch(() => undefined);
}

/**
 * A message's text, with its web addresses as links (docs/ADR/0066). A
 * link shows its address as written; the shell opens it in the system's
 * browser, never in the app.
 */
export function MessageText({ text }: { text: string }) {
  const parts = useMemo(() => linkParts(text), [text]);
  return (
    <>
      {parts.map((part, index) =>
        "url" in part ? (
          // Parts have no ID; their order never changes for one text.
          // oxlint-disable-next-line react/no-array-index-key
          <a key={index} href={part.url} rel="noopener noreferrer nofollow" onClick={(event) => openLink(event, part.url)}>
            {part.text}
          </a>
        ) : (
          // oxlint-disable-next-line react/no-array-index-key
          <span key={index}>{part.text}</span>
        ),
      )}
    </>
  );
}
