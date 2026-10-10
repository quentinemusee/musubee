// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

// Builds the thread list benchmark page (bench/, docs/ADR/0020) into
// dist-bench/, with the app's build settings. MUSUBEE_BENCH_TARGET lowers
// the output for an old engine (chrome83: the Android 11 emulator's WebView).
import react from "@vitejs/plugin-react";
import { defineConfig } from "vite";

export default defineConfig({
  plugins: [react()],
  root: "bench",
  base: "./",
  build: {
    outDir: "../dist-bench",
    ...(process.env.MUSUBEE_BENCH_TARGET ? { target: process.env.MUSUBEE_BENCH_TARGET } : {}),
    emptyOutDir: true,
    assetsInlineLimit: 0,
    modulePreload: { polyfill: false },
  },
});
