// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

import { useMemo } from "react";
import { qrSvg } from "../qr";

/**
 * A QR code: dark modules on a light square in every theme, since phone
 * cameras expect it. The data stays readable as text below it, for when the
 * code cannot be scanned.
 */
export function QrCode({ data }: { data: string }) {
  // An SVG image in a data: URI, which the Content-Security-Policy allows
  // (img-src data:), and which the browser scales without blurring.
  const src = useMemo(() => `data:image/svg+xml,${encodeURIComponent(qrSvg(data))}`, [data]);
  return (
    <div className="qr">
      <img className="qr-code" src={src} alt="QR code" />
      <details className="qr-text">
        <summary>Show the code as text</summary>
        <output className="login-link">{data}</output>
      </details>
    </div>
  );
}
