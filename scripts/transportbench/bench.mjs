// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

// Measures, from Node (the runtime of Electron's main process), the
// transports between a user interface and the core (docs/ADR/0012):
//
// - ffi-sync:  the shared library loaded with koffi, called on the main
//              thread (it blocks the event loop during each call);
// - ffi-async: the same library, each call on a koffi worker thread;
// - stdio:     the core in a child process, newline-delimited JSON on its
//              standard input and output;
// - tcp:       the same child process on a loopback TCP port, behind a
//              token (core/api/transportbench).
//
// Each transport runs the same requests of the core API: core.hello, a
// 1 KiB debug.ping loop, then the echo round trip (messages.send to the
// Instant Echo contact, until its message.added event arrives).
//
//   npm ci && node bench.mjs
//
// Needs Go and, for the shared library, a C toolchain (CLAUDE.md §8).
// Environment: MUSUBEE_BENCH_PINGS (default 20000), MUSUBEE_BENCH_ROUNDTRIPS
// (default 200), MUSUBEE_BENCH_ONLY (comma-separated transports).

import { execFileSync, spawn } from "node:child_process";
import { mkdtempSync, rmSync } from "node:fs";
import { connect } from "node:net";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";
import { createInterface } from "node:readline";
import { fileURLToPath } from "node:url";
import koffi from "koffi";

const ROOT = resolve(fileURLToPath(new URL("../..", import.meta.url)));
const PINGS = Number(process.env.MUSUBEE_BENCH_PINGS ?? 20_000);
const ROUNDTRIPS = Number(process.env.MUSUBEE_BENCH_ROUNDTRIPS ?? 200);
const WARMUP = 10;
const EVENT_TIMEOUT_MS = 15_000;
const CLOSED_EVENT = '{"type":"core.closed","data":{}}';

const work = mkdtempSync(join(tmpdir(), "musubee-transportbench-"));
const exe = process.platform === "win32" ? ".exe" : "";
const libName = { win32: "musubee.dll", darwin: "libmusubee.dylib" }[process.platform] ?? "libmusubee.so";

function build() {
  const go = (...args) => execFileSync("go", args, { cwd: ROOT, stdio: "inherit" });
  // goolm: the core's pure-Go Olm (docs/ADR/0018).
  go("build", "-tags=goolm", "-o", join(work, `transportbench${exe}`), "./core/api/transportbench");
  go("build", "-tags=goolm", "-buildmode=c-shared", "-o", join(work, libName), "./core/ffi");
}

// A core behind a transport: request() resolves with the result, events go
// to the listeners.
class Core {
  #lastId = 0;
  #pending = new Map();
  #listeners = new Set();

  request(command, params) {
    const id = ++this.#lastId;
    const json = JSON.stringify(params === undefined ? { id, command } : { id, command, params });
    return new Promise((resolve, reject) => {
      this.#pending.set(id, { resolve, reject, command });
      this.send(json);
    });
  }

  // Called by the transport with each document from the core.
  receive(json) {
    const doc = JSON.parse(json);
    if (doc.id !== undefined) {
      const pending = this.#pending.get(doc.id);
      this.#pending.delete(doc.id);
      if (doc.error) {
        pending.reject(new Error(`${pending.command}: ${doc.error.code}: ${doc.error.message}`));
      } else {
        pending.resolve(doc.result);
      }
    } else {
      for (const listener of this.#listeners) {
        listener(doc);
      }
    }
  }

  waitEvent(what, match) {
    return new Promise((resolve, reject) => {
      const timer = setTimeout(() => {
        this.#listeners.delete(listener);
        reject(new Error(`no ${what} within ${EVENT_TIMEOUT_MS} ms`));
      }, EVENT_TIMEOUT_MS);
      const listener = (event) => {
        if (match(event)) {
          clearTimeout(timer);
          this.#listeners.delete(listener);
          resolve(event);
        }
      };
      this.#listeners.add(listener);
    });
  }
}

let ffi;
function loadLibrary() {
  if (ffi) {
    return ffi;
  }
  const lib = koffi.load(join(work, libName));
  koffi.struct("musubee_buffer", { data: "uint8_t *", len: "size_t" });
  ffi = {
    open: lib.func("uint64_t musubee_open(const uint8_t *config, size_t config_len, _Out_ musubee_buffer *error)"),
    call: lib.func("musubee_buffer musubee_call(uint64_t handle, const uint8_t *request, size_t request_len)"),
    next: lib.func("musubee_buffer musubee_next_event(uint64_t handle, int32_t timeout_ms)"),
    close: lib.func("void musubee_close(uint64_t handle)"),
    free: lib.func("void musubee_free(musubee_buffer buffer)"),
  };
  return ffi;
}

// Copies a buffer of the library into a string and releases it.
function take(buffer) {
  const text = buffer.len > 0 ? koffi.decode(buffer.data, "char", Number(buffer.len)) : "";
  ffi.free(buffer);
  return text;
}

class FfiCore extends Core {
  #handle;
  #async;
  #pumping;

  constructor(dataDir, async) {
    super();
    const lib = loadLibrary();
    const config = Buffer.from(JSON.stringify({ data_dir: dataDir, log_level: "warn" }));
    const error = {};
    this.#handle = lib.open(config, config.length, error);
    if (!this.#handle) {
      throw new Error(`musubee_open: ${take(error)}`);
    }
    this.#async = async;
    // Events are read on a worker thread: musubee_next_event blocks.
    this.#pumping = new Promise((resolve) => {
      const pump = () =>
        lib.next.async(this.#handle, 1000, (err, buffer) => {
          const json = err ? CLOSED_EVENT : take(buffer);
          if (json) {
            this.receive(json);
          }
          if (json === CLOSED_EVENT) {
            resolve();
          } else {
            pump();
          }
        });
      pump();
    });
  }

  send(json) {
    const request = Buffer.from(json);
    if (this.#async) {
      ffi.call.async(this.#handle, request, request.length, (err, buffer) => {
        if (err) {
          throw err;
        }
        this.receive(take(buffer));
      });
    } else {
      this.receive(take(ffi.call(this.#handle, request, request.length)));
    }
  }

  async close() {
    await new Promise((resolve) => ffi.close.async(this.#handle, resolve));
    await this.#pumping;
  }
}

class StreamCore extends Core {
  #child;
  #output;
  #exited;

  static async start(dataDir, listen) {
    const core = new StreamCore();
    core.#child = spawn(join(work, `transportbench${exe}`), ["-data", dataDir, "-listen", listen], {
      stdio: ["pipe", "pipe", "inherit"],
    });
    core.#exited = new Promise((resolve) => core.#child.once("exit", resolve));
    const lines = createInterface({ input: core.#child.stdout });
    if (listen === "stdio") {
      core.#output = core.#child.stdin;
      lines.on("line", (line) => core.receive(line));
      return core;
    }
    const [port, token] = (await lines[Symbol.asyncIterator]().next()).value.split(" ");
    const socket = connect({ host: "127.0.0.1", port: Number(port) });
    socket.setNoDelay(true);
    await new Promise((resolve, reject) => socket.once("connect", resolve).once("error", reject));
    socket.write(`${token}\n`);
    createInterface({ input: socket }).on("line", (line) => core.receive(line));
    core.#output = socket;
    return core;
  }

  send(json) {
    this.#output.write(`${json}\n`);
  }

  async close() {
    this.#output.end();
    await this.#exited;
  }
}

async function measure(name, core) {
  const result = { transport: name };
  const hello = await core.request("core.hello");
  if (!hello.api_version.startsWith("1.")) {
    throw new Error(`unexpected API ${hello.api_version}`);
  }
  const payload = { payload: "x".repeat(1024) };
  for (let i = 0; i < 1000; i++) {
    await core.request("debug.ping", payload);
  }
  let start = process.hrtime.bigint();
  for (let i = 0; i < PINGS; i++) {
    await core.request("debug.ping", payload);
  }
  result.ping_us = Number(process.hrtime.bigint() - start) / 1e3 / PINGS;

  const conversation = core.waitEvent("Instant Echo conversation", (e) =>
    e.type === "conversation.updated" && e.data.conversation.name === "Instant Echo");
  const step = await core.request("login.start", { network_id: "echo", flow_id: "username" });
  await core.request("login.submit", { process_id: step.process_id, values: { [step.fields[0].field_id]: "bench" } });
  const conversationId = (await conversation).data.conversation.conversation_id;
  const roundTrip = async (text) => {
    const echo = core.waitEvent(`echo of ${text}`, (e) =>
      e.type === "message.added" && !e.data.message.from_me && e.data.message.text.includes(text));
    await core.request("messages.send", { conversation_id: conversationId, text });
    await echo;
  };
  for (let i = 0; i < WARMUP; i++) {
    await roundTrip(`warm-up ${i}`);
  }
  const times = [];
  for (let i = 0; i < ROUNDTRIPS; i++) {
    start = process.hrtime.bigint();
    await roundTrip(`message ${i}`);
    times.push(Number(process.hrtime.bigint() - start) / 1e6);
  }
  times.sort((a, b) => a - b);
  result.roundtrip_mean_ms = times.reduce((a, b) => a + b, 0) / times.length;
  result.roundtrip_p50_ms = times[Math.floor(times.length / 2)];
  result.roundtrip_max_ms = times[times.length - 1];
  return result;
}

const transports = {
  "ffi-sync": (dir) => new FfiCore(dir, false),
  "ffi-async": (dir) => new FfiCore(dir, true),
  stdio: (dir) => StreamCore.start(dir, "stdio"),
  tcp: (dir) => StreamCore.start(dir, "tcp"),
};

try {
  build();
  const only = process.env.MUSUBEE_BENCH_ONLY?.split(",");
  for (const [name, start] of Object.entries(transports)) {
    if (only && !only.includes(name)) {
      continue;
    }
    const dir = mkdtempSync(join(work, `${name}-`));
    const core = await start(dir);
    try {
      const result = await measure(name, core);
      console.log(JSON.stringify({ ...result, pings: PINGS, roundtrips: ROUNDTRIPS, platform: process.platform, node: process.version }));
    } finally {
      await core.close();
    }
  }
} finally {
  // The loaded library stays mapped until the process exits; on Windows
  // its file cannot be deleted before.
  try {
    rmSync(work, { recursive: true, force: true, maxRetries: 2 });
  } catch {
    // Left in the temporary directory.
  }
}
