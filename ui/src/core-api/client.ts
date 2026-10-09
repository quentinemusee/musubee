// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

// Typed client of the core API (core/api/schema, docs/ADR/0012). The types
// come from types.gen.ts, generated from the schema: this file only adds
// the request IDs, the errors and the version check. It knows nothing of
// the transport (JNI on Android, a child process on desktop).

import {
  API_VERSION,
  EVENT_TYPES,
  type CommandName,
  type Commands,
  type CoreError,
  type ErrorCode,
  type Event,
  type EventType,
} from "./types.gen";

/** Carries JSON documents to and from a core. */
export interface Transport {
  /** Sends one JSON request and resolves with the JSON response. */
  call(request: string): Promise<string>;
}

/** An error answered by the core. */
export class CoreApiError extends Error {
  readonly code: ErrorCode;

  constructor(readonly command: CommandName, error: CoreError) {
    super(`${command}: ${error.code}: ${error.message}`);
    this.name = "CoreApiError";
    this.code = error.code;
  }
}

// Params can be left out when every field is optional.
type ParamsArgs<K extends CommandName> =
  Record<string, never> extends Commands[K]["params"]
    ? [params?: Commands[K]["params"]]
    : [params: Commands[K]["params"]];

export class CoreClient {
  readonly #transport: Transport;
  #lastId = 0;

  constructor(transport: Transport) {
    this.#transport = transport;
  }

  /** Runs a command; rejects with a CoreApiError when the core answers an error. */
  async call<K extends CommandName>(command: K, ...[params]: ParamsArgs<K>): Promise<Commands[K]["result"]> {
    const id = ++this.#lastId;
    const request = params === undefined ? { id, command } : { id, command, params };
    const response: unknown = JSON.parse(await this.#transport.call(JSON.stringify(request)));
    if (!isObject(response) || response["id"] !== id) {
      throw new Error(`${command}: response does not answer request ${id}`);
    }
    if (isObject(response["error"])) {
      throw new CoreApiError(command, response["error"] as unknown as CoreError);
    }
    if (!("result" in response)) {
      throw new Error(`${command}: response has neither result nor error`);
    }
    return response["result"] as Commands[K]["result"];
  }

  /**
   * Calls core.hello and checks that the core speaks the same major version
   * of the API. A core with a newer minor version is accepted: it only adds
   * commands, events and fields, which this client ignores.
   */
  async hello(): Promise<Commands["core.hello"]["result"]> {
    const hello = await this.call("core.hello");
    if (!isCompatibleVersion(hello.api_version)) {
      throw new Error(`the core speaks API ${hello.api_version}, this interface speaks ${API_VERSION}`);
    }
    return hello;
  }
}

/** Whether a core's API version has the same major version as this client. */
export function isCompatibleVersion(version: string): boolean {
  return major(version) !== undefined && major(version) === major(API_VERSION);
}

function major(version: string): string | undefined {
  return /^(\d+)\.\d+$/.exec(version)?.[1];
}

/**
 * Parses an event of the core. Returns null for an event type this client
 * does not know: a newer core may send it, and it must be ignored.
 */
export function parseEvent(json: string): Event | null {
  const event: unknown = JSON.parse(json);
  if (!isObject(event) || typeof event["type"] !== "string" || !isObject(event["data"])) {
    throw new Error("invalid event");
  }
  if (!(EVENT_TYPES as readonly string[]).includes(event["type"])) {
    return null;
  }
  return { type: event["type"] as EventType, data: event["data"] } as Event;
}

function isObject(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}
