// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

// Unit tests of the typed client, with a scripted transport. The client
// against a real core is covered by the transport measurements of
// docs/ADR/0012.

import { describe, expect, test } from "vitest";
import { CoreApiError, CoreClient, isCompatibleVersion, parseEvent, type Transport } from "./client";

// Answers each request with reply(request), and records the requests.
function scripted(reply: (request: { id: number; command: string; params?: unknown }) => unknown) {
  const requests: unknown[] = [];
  const transport: Transport = {
    call: async (json) => {
      const request = JSON.parse(json);
      requests.push(request);
      return JSON.stringify(reply(request));
    },
  };
  return { transport, requests };
}

describe("CoreClient", () => {
  test("numbers requests and returns results", async () => {
    const { transport, requests } = scripted((r) => ({ id: r.id, result: r.params ?? {} }));
    const client = new CoreClient(transport);
    expect(await client.call("debug.ping", { payload: "a" })).toEqual({ payload: "a" });
    await client.call("conversations.list");
    expect(requests).toEqual([
      { id: 1, command: "debug.ping", params: { payload: "a" } },
      { id: 2, command: "conversations.list" },
    ]);
  });

  test("rejects with the core's error", async () => {
    const { transport } = scripted((r) => ({ id: r.id, error: { code: "not_found", message: "no conversation" } }));
    const error = await new CoreClient(transport).call("messages.send", { conversation_id: "c.x", text: "hi" }).catch((e) => e);
    expect(error).toBeInstanceOf(CoreApiError);
    expect(error).toMatchObject({ code: "not_found", command: "messages.send" });
  });

  test("rejects a response to another request", async () => {
    const { transport } = scripted(() => ({ id: 99, result: {} }));
    await expect(new CoreClient(transport).call("core.hello")).rejects.toThrow("does not answer request 1");
  });

  test("checks the API version", async () => {
    const newer = scripted((r) => ({ id: r.id, result: { api_version: "1.7", core_version: "x" } }));
    expect(await new CoreClient(newer.transport).hello()).toMatchObject({ api_version: "1.7" });
    const breaking = scripted((r) => ({ id: r.id, result: { api_version: "2.0", core_version: "x" } }));
    await expect(new CoreClient(breaking.transport).hello()).rejects.toThrow("speaks API 2.0");
  });
});

test("isCompatibleVersion compares major versions", () => {
  expect(isCompatibleVersion("1.0")).toBe(true);
  expect(isCompatibleVersion("1.12")).toBe(true);
  expect(isCompatibleVersion("2.0")).toBe(false);
  expect(isCompatibleVersion("1")).toBe(false);
  expect(isCompatibleVersion("")).toBe(false);
});

describe("parseEvent", () => {
  test("returns known events", () => {
    expect(parseEvent('{"type":"core.closed","data":{}}')).toEqual({ type: "core.closed", data: {} });
  });

  test("ignores the events of a newer core", () => {
    expect(parseEvent('{"type":"reaction.added","data":{"emoji":"👍"}}')).toBeNull();
  });

  test("rejects documents that are not events", () => {
    expect(() => parseEvent('{"type":"core.closed"}')).toThrow("invalid event");
    expect(() => parseEvent("[]")).toThrow("invalid event");
  });
});
