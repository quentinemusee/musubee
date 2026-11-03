// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

import { expect, test } from "vitest";
import { externalLink } from "../src/external-links.ts";

const app = "app://musubee";

test("web addresses leave for the system's browser", () => {
  expect(externalLink("https://example.org/a?b=1", app)).toBe("https://example.org/a?b=1");
  expect(externalLink("http://example.org", app)).toBe("http://example.org/");
});

test("everything else is only refused", () => {
  expect(externalLink("app://musubee/index.html", app)).toBeNull();
  expect(externalLink("file:///etc/passwd", app)).toBeNull();
  expect(externalLink("javascript:alert(1)", app)).toBeNull();
  expect(externalLink("smb://server/share", app)).toBeNull();
  expect(externalLink("https://user:pass@example.org", app)).toBeNull();
  expect(externalLink("not a url", app)).toBeNull();
});
