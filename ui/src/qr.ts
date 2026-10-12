// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

// QR codes of login steps (a display of type "qr"): the linking codes of
// Signal and Telegram, which the phone that runs the network's app scans.

import { encode } from "uqr";

/** The quiet zone around the code, in modules, as ISO/IEC 18004 asks. */
export const QR_QUIET_ZONE = 4;

export interface QrImage {
  /** Width and height in modules, quiet zone included. */
  size: number;
  /** The dark modules, as one SVG path in module units. */
  path: string;
}

/**
 * The QR code of data. Error correction level M (15%): a code on a screen,
 * scanned by a phone's camera, may catch glare or moire patterns.
 */
export function qrImage(data: string): QrImage {
  const { size, data: modules } = encode(data, { ecc: "M", border: QR_QUIET_ZONE });
  const parts: string[] = [];
  modules.forEach((row, y) => {
    row.forEach((dark, x) => {
      if (dark) {
        parts.push(`M${x} ${y}h1v1h-1z`);
      }
    });
  });
  return { size, path: parts.join("") };
}

/** The QR code of data as an SVG document: black modules on white. */
export function qrSvg(data: string): string {
  const { size, path } = qrImage(data);
  return (
    `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 ${size} ${size}" shape-rendering="crispEdges">` +
    `<rect width="${size}" height="${size}" fill="#fff"/><path d="${path}" fill="#000"/></svg>`
  );
}
