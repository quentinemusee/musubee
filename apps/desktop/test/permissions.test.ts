// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

import { describe, expect, test } from "vitest";
import { allowCheck, allowRequest, isAppOrigin } from "../src/permissions.ts";

describe("permissions", () => {
  test("knows the app's origin", () => {
    expect(isAppOrigin("app://musubee/index.html")).toBe(true);
    expect(isAppOrigin("app://musubee")).toBe(true);
    expect(isAppOrigin("app://other/index.html")).toBe(false);
    expect(isAppOrigin("https://musubee/")).toBe(false);
    expect(isAppOrigin("not a url")).toBe(false);
    expect(isAppOrigin(undefined)).toBe(false);
  });

  test("grants the microphone to the app's main frame, and nothing else", () => {
    const app = { isMainFrame: true, requestingUrl: "app://musubee/index.html" };
    expect(allowRequest("media", { ...app, mediaTypes: ["audio"] })).toBe(true);
    expect(allowRequest("media", { ...app, mediaTypes: ["audio", "video"] })).toBe(false);
    expect(allowRequest("media", { ...app, mediaTypes: ["video"] })).toBe(false);
    expect(allowRequest("media", { ...app, mediaTypes: [] })).toBe(false);
    expect(allowRequest("media", { ...app })).toBe(false);
    expect(allowRequest("media", { ...app, isMainFrame: false, mediaTypes: ["audio"] })).toBe(false);
    expect(allowRequest("media", { isMainFrame: true, requestingUrl: "https://example.org/", mediaTypes: ["audio"] })).toBe(false);
    expect(allowRequest("notifications", app)).toBe(false);
    expect(allowRequest("display-capture", app)).toBe(false);

    expect(allowCheck("media", "app://musubee", { isMainFrame: true, mediaType: "audio" })).toBe(true);
    expect(allowCheck("media", "app://musubee", { isMainFrame: true, mediaType: "video" })).toBe(false);
    expect(allowCheck("media", "app://musubee", { isMainFrame: true })).toBe(false);
    expect(allowCheck("media", "https://example.org", { isMainFrame: true, mediaType: "audio" })).toBe(false);
    expect(allowCheck("geolocation", "app://musubee", { isMainFrame: true })).toBe(false);
  });
});
