// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

import react from "@vitejs/plugin-react";
import { defineConfig } from "vitest/config";

export default defineConfig({
  plugins: [react()],
  // Relative paths: the shells serve the build from their own origin.
  base: "./",
  build: {
    outDir: "dist",
    emptyOutDir: true,
    // No inline scripts or styles: the Content-Security-Policy forbids them.
    assetsInlineLimit: 0,
    modulePreload: { polyfill: false },
  },
  test: {
    include: ["src/**/*.test.ts", "src/**/*.test.tsx"],
  },
});
