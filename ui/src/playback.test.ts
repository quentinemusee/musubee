// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

import { expect, test } from "vitest";
import { nextRate, rateLabel } from "./app/MessageMedia";

test("voice messages play at 1×, 1.5× and 2×, in turn", () => {
  expect(nextRate(1)).toBe(1.5);
  expect(nextRate(1.5)).toBe(2);
  expect(nextRate(2)).toBe(1);
  expect(nextRate(3)).toBe(1);
  expect(rateLabel(1.5)).toBe("1.5×");
});
