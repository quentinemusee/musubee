// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

// TypeScript side of the contract tests (docs/ADR/0012). The Go side
// (core/api, core/embedded) checks the same schema and the same examples
// with another JSON Schema validator. Here:
// - Ajv checks the shared examples, valid and invalid;
// - examples written against the generated types, one per command and per
//   event, must pass the schema: the generated types cannot accept a
//   document that the core would reject;
// - the generated lists match the schema.

import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { Ajv2020 } from "ajv/dist/2020.js";
import type { ValidateFunction } from "ajv";
import { describe, expect, test } from "vitest";
import {
  API_VERSION,
  COMMAND_NAMES,
  EVENT_TYPES,
  type CommandName,
  type Commands,
  type Event,
  type EventType,
  type Events,
  type Message,
  type Request,
  type Response,
} from "./types.gen";

const SCHEMA_DIR = new URL("../../../core/api/schema/", import.meta.url);
const read = (name: string): unknown => JSON.parse(readFileSync(fileURLToPath(new URL(name, SCHEMA_DIR)), "utf8"));

interface Schema {
  $id: string;
  "x-musubee-api-version": string;
  $defs: { Commands: { properties: Record<string, unknown> }; Events: { properties: Record<string, unknown> } };
}
interface Example {
  target: string;
  why?: string;
  value: unknown;
}

const schema = read("core-api.schema.json") as Schema;
const examples = read("examples.json") as { valid: Example[]; invalid: Example[] };

// Strict, except for strictRequired: the if/then rules of LoginStep require
// properties that the object declares, not the then branch, which is valid
// JSON Schema.
const ajv = new Ajv2020({ strict: true, strictRequired: false, allErrors: true });
// The schema's own annotation of its version.
ajv.addKeyword("x-musubee-api-version");
ajv.addSchema(schema);

const escape = (token: string) => token.replaceAll("~", "~0").replaceAll("/", "~1");

// The JSON pointer of a target; the same syntax as core/api/apitest.
function pointer(target: string): string {
  const parts = target.split(":");
  if (parts.length === 1) {
    return `/$defs/${escape(target)}`;
  }
  const [kind, name, part] = parts;
  if (kind === "command" && name && (part === "params" || part === "result")) {
    return `/$defs/Commands/properties/${escape(name)}/properties/${part}`;
  }
  if (kind === "event" && name && parts.length === 2) {
    return `/$defs/Events/properties/${escape(name)}`;
  }
  throw new Error(`invalid target ${target}`);
}

function validator(target: string): ValidateFunction {
  const ref = `${schema.$id}#${pointer(target)}`;
  const validate = ajv.getSchema(ref);
  if (!validate) {
    throw new Error(`no schema at ${ref}`);
  }
  return validate;
}

function errors(target: string, value: unknown): string {
  const validate = validator(target);
  return validate(value) ? "" : ajv.errorsText(validate.errors);
}

describe("the shared examples", () => {
  test.each(examples.valid.map((e, i) => [i, e.target, e] as const))("valid #%i (%s)", (_, target, example) => {
    expect(errors(target, example.value)).toBe("");
  });

  test.each(examples.invalid.map((e, i) => [i, e.target, e] as const))("invalid #%i (%s)", (_, target, example) => {
    expect(errors(target, example.value), example.why).not.toBe("");
  });
});

describe("the generated types", () => {
  test("carry the schema's version", () => {
    expect(API_VERSION).toBe(schema["x-musubee-api-version"]);
  });

  test("list every command and event of the schema", () => {
    expect(COMMAND_NAMES.toSorted()).toEqual(Object.keys(schema.$defs.Commands.properties).toSorted());
    expect(EVENT_TYPES.toSorted()).toEqual(Object.keys(schema.$defs.Events.properties).toSorted());
  });

  const message: Message = {
    message_id: "m.1",
    conversation_id: "c.1",
    from_me: false,
    sender_name: "Instant Echo",
    timestamp_ms: 1_791_000_000_000,
    kind: "text",
    text: "hello",
    status: "received",
  };
  const conversation = { conversation_id: "c.1", account_id: "a.1", network_id: "echo", name: "Instant Echo", kind: "direct" } as const;
  const person = { person_id: "h.1", name: "Alice", conversation_ids: ["c.1", "c.2"] };

  // The mapped types make the compiler require one example per command and
  // per event: a command added to the schema breaks the typecheck here.
  const commands: { [K in CommandName]: Commands[K] } = {
    "core.hello": { params: {}, result: { api_version: "1.2", core_version: "dev" } },
    "networks.list": {
      params: {},
      result: { networks: [{ network_id: "echo", name: "Echo", login_flows: [{ flow_id: "username", name: "Username", description: "" }] }] },
    },
    "accounts.list": {
      params: {},
      result: {
        accounts: [
          { account_id: "a.1", network_id: "echo", name: "alice", state: "connected" },
          { account_id: "a.2", network_id: "echo", name: "bob", state: "bad_credentials", error: "Logged out remotely" },
        ],
      },
    },
    "accounts.logout": { params: { account_id: "a.1" }, result: {} },
    "login.start": {
      params: { network_id: "echo", flow_id: "username" },
      result: {
        process_id: "p.1",
        type: "user_input",
        step_id: "com.musubee.echo.username",
        instructions: "Choose a name",
        fields: [{ field_id: "username", type: "username", name: "Username", description: "", pattern: "^[a-z]+$" }],
      },
    },
    "login.submit": {
      params: { process_id: "p.1", values: { username: "alice" } },
      result: { process_id: "p.1", type: "complete", step_id: "done", instructions: "", account_id: "a.1" },
    },
    "login.wait": {
      params: { process_id: "p.2" },
      result: { process_id: "p.2", type: "display_and_wait", step_id: "code", instructions: "Enter the code", display: { type: "code", data: "1f2e3d", can_cancel: true } },
    },
    "login.cancel": { params: { process_id: "p.2" }, result: {} },
    "conversations.list": { params: { account_id: "a.1" }, result: { conversations: [conversation] } },
    "messages.list": { params: { conversation_id: "c.1", limit: 50 }, result: { messages: [message], before: "k.1" } },
    "messages.send": {
      params: { conversation_id: "c.1", text: "hello" },
      result: { message: { ...message, from_me: true, status: "sending", sender_name: "alice" } },
    },
    "persons.list": { params: {}, result: { persons: [person] } },
    "persons.create": { params: { conversation_ids: ["c.1", "c.2"] }, result: { person } },
    "persons.rename": { params: { person_id: "h.1", name: "Alice" }, result: { person } },
    "persons.link": { params: { person_id: "h.1", conversation_id: "c.2" }, result: { person } },
    "persons.unlink": { params: { conversation_id: "c.2" }, result: {} },
    "persons.delete": { params: { person_id: "h.1" }, result: {} },
    "debug.ping": { params: { payload: "x" }, result: { payload: "x" } },
    "debug.stats": { params: {}, result: { heap_alloc_bytes: 1, heap_sys_bytes: 2, goroutines: 3 } },
  };
  const events: { [K in EventType]: Events[K] } = {
    "account.updated": { account: { account_id: "a.1", network_id: "echo", name: "alice", state: "transient_disconnect", error: "No network" } },
    "conversation.updated": { conversation: { ...conversation, person_id: "h.1" } },
    "person.updated": { person },
    "person.deleted": { person_id: "h.1" },
    "message.added": { message },
    "message.updated": { message: { ...message, from_me: true, status: "failed", error: "Unreachable" } },
    "resync.required": {},
    "core.closed": {},
  };

  test.each(COMMAND_NAMES)("accept only valid params and results: %s", (name) => {
    expect(errors(`command:${name}:params`, commands[name].params)).toBe("");
    expect(errors(`command:${name}:result`, commands[name].result)).toBe("");
  });

  test.each(EVENT_TYPES)("accept only valid event data: %s", (type) => {
    expect(errors(`event:${type}`, events[type])).toBe("");
  });

  test("build valid envelopes", () => {
    const request: Request<"messages.send"> = { id: 1, command: "messages.send", params: commands["messages.send"].params };
    const ok: Response<"debug.ping"> = { id: 1, result: { payload: "x" } };
    const failed: Response = { id: 2, error: { code: "not_found", message: "no conversation" } };
    const event: Event = { type: "message.added", data: events["message.added"] };
    expect(errors("Request", request)).toBe("");
    expect(errors("Response", ok)).toBe("");
    expect(errors("Response", failed)).toBe("");
    expect(errors("Event", event)).toBe("");
  });
});
