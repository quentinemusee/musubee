// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

import { readFileSync } from "node:fs";
import { describe, expect, test } from "vitest";
import { MAX_REQUEST_LENGTH, RequestValidator } from "../src/ipc.ts";

const schema = JSON.parse(readFileSync(new URL("../../../core/api/schema/core-api.schema.json", import.meta.url), "utf8"));
const validator = new RequestValidator(schema);

function code(payload: string): string | undefined {
  const checked = validator.check(payload);
  return checked.ok ? undefined : (JSON.parse(checked.response) as { error: { code: string } }).error.code;
}

describe("RequestValidator", () => {
  test("knows every command of the schema", () => {
    expect(validator.commands.toSorted()).toEqual(Object.keys(schema.$defs.Commands.properties).toSorted());
  });

  test("accepts valid requests", () => {
    expect(validator.check(JSON.stringify({ id: 1, command: "core.hello" }))).toEqual({ ok: true, request: { id: 1, command: "core.hello" } });
    expect(code(JSON.stringify({ id: 2, command: "core.hello", params: {} }))).toBeUndefined();
    expect(code(JSON.stringify({ id: 3, command: "messages.send", params: { conversation_id: "c", text: "hi" } }))).toBeUndefined();
    expect(code(JSON.stringify({ id: 4, command: "conversations.list" }))).toBeUndefined();
  });

  test.each([
    ["not JSON", "{", "invalid_request"],
    ["an array", "[]", "invalid_request"],
    ["no command", JSON.stringify({ id: 1 }), "invalid_request"],
    ["an extra field", JSON.stringify({ id: 1, command: "core.hello", extra: 1 }), "invalid_request"],
    ["a string ID", JSON.stringify({ id: "1", command: "core.hello" }), "invalid_request"],
    ["ID zero", JSON.stringify({ id: 0, command: "core.hello" }), "invalid_request"],
    ["a negative ID", JSON.stringify({ id: -4, command: "core.hello" }), "invalid_request"],
    ["a fractional ID", JSON.stringify({ id: 1.5, command: "core.hello" }), "invalid_request"],
    ["params that are not an object", JSON.stringify({ id: 1, command: "core.hello", params: [] }), "invalid_request"],
    ["an unknown command", JSON.stringify({ id: 1, command: "core.exec" }), "unknown_command"],
    ["a prototype key as command", JSON.stringify({ id: 1, command: "__proto__" }), "unknown_command"],
    ["missing params", JSON.stringify({ id: 1, command: "messages.send" }), "invalid_params"],
    ["a param of the wrong type", JSON.stringify({ id: 1, command: "messages.send", params: { conversation_id: 1, text: "x" } }), "invalid_params"],
    ["an unknown param", JSON.stringify({ id: 1, command: "core.hello", params: { x: 1 } }), "invalid_params"],
  ])("refuses %s", (_, payload, expected) => {
    expect(code(payload)).toBe(expected);
  });

  test("keeps the caller's ID in refusals when it has one", () => {
    const checked = validator.check(JSON.stringify({ id: 9, command: "nope" }));
    expect(checked.ok ? null : JSON.parse(checked.response)).toMatchObject({ id: 9, error: { code: "unknown_command" } });
  });

  test("throws for payloads no interface sends", () => {
    expect(() => validator.check(42)).toThrow(TypeError);
    expect(() => validator.check({ id: 1, command: "core.hello" })).toThrow(TypeError);
    expect(() => validator.check("x".repeat(MAX_REQUEST_LENGTH + 1))).toThrow(RangeError);
  });
});
