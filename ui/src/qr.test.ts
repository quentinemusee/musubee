// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

import jsQR from "jsqr";
import { describe, expect, test } from "vitest";
import { QR_QUIET_ZONE, qrImage, qrSvg, type QrImage } from "./qr";

const PIXELS_PER_MODULE = 4;

/**
 * Draws the image's path as a decoder would see it on a screen: each "M x y"
 * of the path darkens one module, everything else stays light.
 */
function rasterize({ size, path }: QrImage): { pixels: Uint8ClampedArray; width: number } {
  const width = size * PIXELS_PER_MODULE;
  const pixels = new Uint8ClampedArray(width * width * 4).fill(255);
  for (const [, x, y] of path.matchAll(/M(\d+) (\d+)h1v1h-1z/g)) {
    for (let dy = 0; dy < PIXELS_PER_MODULE; dy++) {
      for (let dx = 0; dx < PIXELS_PER_MODULE; dx++) {
        const offset = ((Number(y) * PIXELS_PER_MODULE + dy) * width + Number(x) * PIXELS_PER_MODULE + dx) * 4;
        pixels.fill(0, offset, offset + 3);
      }
    }
  }
  return { pixels, width };
}

function decode(image: QrImage): string | undefined {
  const { pixels, width } = rasterize(image);
  return jsQR(pixels, width, width)?.data;
}

describe("qrImage", () => {
  test("draws a code that a decoder reads back", () => {
    expect(decode(qrImage("musubee-echo:1a2b3c"))).toBe("musubee-echo:1a2b3c");
  });

  test("holds a Signal linking URI", () => {
    // The shape of signalmeow's provisioning URI (provisioning.go): the
    // capabilities, a 33-byte public key in standard base64 and the
    // provisioning address, query-escaped; the values are made up.
    const publicKey = btoa(String.fromCharCode(...Array.from({ length: 33 }, (_, i) => (i * 37 + 5) % 256)));
    const query = new URLSearchParams({ capabilities: "backup5", pub_key: publicKey, uuid: "Xq0Zb7-hT3aV9cL2mW8yKe" });
    const uri = `sgnl://linkdevice?${query.toString()}`;
    expect(decode(qrImage(uri))).toBe(uri);
  });

  test("keeps a light quiet zone around the code", () => {
    const { size, path } = qrImage("musubee-echo:1a2b3c");
    for (const [, x, y] of path.matchAll(/M(\d+) (\d+)h1v1h-1z/g)) {
      for (const coordinate of [Number(x), Number(y)]) {
        expect(coordinate).toBeGreaterThanOrEqual(QR_QUIET_ZONE);
        expect(coordinate).toBeLessThan(size - QR_QUIET_ZONE);
      }
    }
  });

  test("makes an SVG document of the same path", () => {
    const svg = qrSvg("musubee-echo:1a2b3c");
    const { size, path } = qrImage("musubee-echo:1a2b3c");
    expect(svg).toContain(`viewBox="0 0 ${size} ${size}"`);
    expect(svg).toContain(`<path d="${path}" fill="#000"/>`);
    expect(svg.startsWith('<svg xmlns="http://www.w3.org/2000/svg"')).toBe(true);
  });

  test("only uses whole modules", () => {
    const { path } = qrImage("x");
    expect(path.replaceAll(/M\d+ \d+h1v1h-1z/g, "")).toBe("");
  });
});
