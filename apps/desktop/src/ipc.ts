// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

// Validation of the requests that the interface sends to the core through
// IPC (docs/ADR/0013-desktop-shell.md). The renderer is treated like a web
// page: whatever it sends is checked against the core API schema
// (core/api/schema, docs/ADR/0012) before it reaches the core. A request
// that fails gets the error the core itself would give, with the same codes;
// a payload that is not even a string of reasonable size is refused outright.
//
// This module does not import Electron, so that it runs under plain Node in
// the tests.

import { Ajv2020 } from "ajv/dist/2020.js";
import type { ValidateFunction } from "ajv";

/** The largest request accepted from the interface, in UTF-16 code units. */
export const MAX_REQUEST_LENGTH = 1 << 20;

export interface ValidRequest {
  id: number;
  command: string;
  params?: object;
}

export type Checked = { ok: true; request: ValidRequest } | { ok: false; response: string };

interface Schema {
  $id: string;
  $defs: { Commands: { properties: Record<string, unknown> } };
}

export class RequestValidator {
  readonly #envelope: ValidateFunction;
  readonly #params = new Map<string, ValidateFunction>();

  constructor(schema: Schema) {
    const ajv = new Ajv2020({ strict: true, strictRequired: false, allErrors: false });
    // The schema's own annotation of its version.
    ajv.addKeyword("x-musubee-api-version");
    ajv.addSchema(schema);
    const compile = (pointer: string) => {
      const validate = ajv.getSchema(`${schema.$id}#${pointer}`);
      if (validate === undefined) {
        throw new Error(`no schema at ${pointer}`);
      }
      return validate;
    };
    this.#envelope = compile("/$defs/Request");
    for (const command of Object.keys(schema.$defs.Commands.properties)) {
      this.#params.set(command, compile(`/$defs/Commands/properties/${command.replaceAll("~", "~0").replaceAll("/", "~1")}/properties/params`));
    }
  }

  /** The commands of the schema. */
  get commands(): string[] {
    return [...this.#params.keys()];
  }

  /**
   * Checks a request from the interface. Throws when the payload is not a
   * string within MAX_REQUEST_LENGTH: no well-behaved interface sends that.
   */
  check(payload: unknown): Checked {
    if (typeof payload !== "string") {
      throw new TypeError("a core request must be a string");
    }
    if (payload.length > MAX_REQUEST_LENGTH) {
      throw new RangeError(`a core request must be at most ${MAX_REQUEST_LENGTH} characters`);
    }
    let request: unknown;
    try {
      request = JSON.parse(payload);
    } catch {
      return refuse(0, "invalid_request", "invalid request: not JSON");
    }
    const id = isObject(request) && Number.isSafeInteger(request["id"]) ? (request["id"] as number) : 0;
    if (!this.#envelope(request) || !isObject(request) || !Number.isSafeInteger(request["id"]) || (request["id"] as number) <= 0) {
      return refuse(id, "invalid_request", `invalid request: ${describe(this.#envelope)}`);
    }
    const command = request["command"] as string;
    const validateParams = this.#params.get(command);
    if (validateParams === undefined) {
      return refuse(id, "unknown_command", `unknown command ${command}`);
    }
    // Absent params count as empty; commands with required params refuse that.
    const params = request["params"] ?? {};
    if (!validateParams(params)) {
      return refuse(id, "invalid_params", `invalid params for ${command}: ${describe(validateParams)}`);
    }
    return { ok: true, request: request as unknown as ValidRequest };
  }
}

function refuse(id: number, code: string, message: string): Checked {
  return { ok: false, response: JSON.stringify({ id, error: { code, message } }) };
}

function describe(validate: ValidateFunction): string {
  const error = validate.errors?.[0];
  return error ? `${error.instancePath || "/"} ${error.message ?? "is invalid"}` : "does not match the schema";
}

function isObject(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}
