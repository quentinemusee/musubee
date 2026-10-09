// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

import { mkdirSync, mkdtempSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { afterAll, describe, expect, test } from "vitest";
import { CONTENT_SECURITY_POLICY, resolveAppPath, serveAppFile } from "../src/app-protocol.ts";

const base = mkdtempSync(join(tmpdir(), "musubee-app-protocol-"));
const root = join(base, "ui");
mkdirSync(join(root, "assets"), { recursive: true });
writeFileSync(join(root, "index.html"), "<!doctype html>");
writeFileSync(join(root, "assets", "app.js"), "console.log(1)");
writeFileSync(join(root, "notes.txt"), "not served");
writeFileSync(join(base, "secret.json"), "{}");

afterAll(() => rmSync(base, { recursive: true, force: true }));

describe("resolveAppPath", () => {
  test("maps app://musubee URLs into the root", () => {
    expect(resolveAppPath(root, "app://musubee/index.html")).toBe(join(root, "index.html"));
    expect(resolveAppPath(root, "app://musubee/")).toBe(join(root, "index.html"));
    expect(resolveAppPath(root, "app://musubee/assets/app.js?v=1#x")).toBe(join(root, "assets", "app.js"));
  });

  test("stays in the root when the URL climbs out of it", () => {
    // The URL parser removes dot segments, encoded or not, before we see them.
    expect(resolveAppPath(root, "app://musubee/../secret.json")).toBe(join(root, "secret.json"));
    expect(resolveAppPath(root, "app://musubee/%2e%2e/%2E%2E/secret.json")).toBe(join(root, "secret.json"));
  });

  test.each([
    ["another host", "app://other/index.html"],
    ["another scheme", "file:///etc/passwd"],
    ["an encoded slash", "app://musubee/..%2fsecret.json"],
    ["an encoded backslash", "app://musubee/..%5csecret.json"],
    ["a NUL byte", "app://musubee/index.html%00.js"],
    ["a malformed escape", "app://musubee/%zz"],
    ["not a URL", "::"],
  ])("refuses %s", (_, url) => {
    expect(resolveAppPath(root, url)).toBeNull();
  });
});

describe("serveAppFile", () => {
  test("serves a file with its type and the security headers", async () => {
    const response = await serveAppFile(root, "app://musubee/assets/app.js");
    expect(response.status).toBe(200);
    expect(response.headers.get("Content-Type")).toBe("text/javascript; charset=utf-8");
    expect(response.headers.get("Content-Security-Policy")).toBe(CONTENT_SECURITY_POLICY);
    expect(response.headers.get("X-Content-Type-Options")).toBe("nosniff");
    expect(await response.text()).toBe("console.log(1)");
  });

  test.each([
    ["a missing file", "app://musubee/missing.js"],
    ["a file of an unknown type", "app://musubee/notes.txt"],
    ["a file outside the root", "app://musubee/..%2fsecret.json"],
  ])("answers 404 for %s, still with the policy", async (_, url) => {
    const response = await serveAppFile(root, url);
    expect(response.status).toBe(404);
    expect(response.headers.get("Content-Security-Policy")).toBe(CONTENT_SECURITY_POLICY);
  });
});
