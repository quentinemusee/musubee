// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

// Runs the core in a child process (core/cmd/musubee-core) and supervises it
// (docs/ADR/0013-desktop-shell.md): requests and responses travel as
// newline-delimited JSON on its standard input and output (docs/ADR/0012).
//
// - Request IDs are the shell's own: each window numbers its requests from
//   1, so the supervisor gives every request a fresh ID and puts the
//   caller's back into the response.
// - When the core stops unexpectedly, the requests waiting for it fail with
//   the "closed" error, and it is started again after a growing delay,
//   unless it failed too often in a short time.
// - stop() ends its input, which makes it close cleanly, and kills it if it
//   does not exit in time.
//
// This module does not import Electron, so that it runs under plain Node in
// the tests.

import { spawn, type ChildProcessWithoutNullStreams } from "node:child_process";
import { createInterface } from "node:readline";

export type CoreStatus =
  | { state: "starting" }
  | { state: "ready" }
  | { state: "restarting"; attempt: number }
  | { state: "failed"; reason: string };

export interface CoreProcessOptions {
  /** Path of the musubee-core executable. */
  executable: string;
  /** Data directory of the core. */
  dataDir: string;
  logLevel?: string;
  /** More crashes than this within crashWindowMs: give up. */
  maxCrashes?: number;
  crashWindowMs?: number;
  /** Delay before the restart after the nth crash in a row (n from 1). */
  restartDelayMs?: (attempt: number) => number;
  /** How long stop() waits for the core to exit before killing it. */
  stopTimeoutMs?: number;
  /** Receives the lines the core writes on its standard error (crash reports). */
  onStderr?: (line: string) => void;
}

interface Pending {
  callerId: number;
  resolve: (response: string) => void;
}

/** The ID of the start-up probe, which no caller uses (they use positive IDs). */
const PROBE_ID = 0;

export class CoreProcess {
  readonly #options: Required<Omit<CoreProcessOptions, "onStderr">> & Pick<CoreProcessOptions, "onStderr">;
  #child: ChildProcessWithoutNullStreams | null = null;
  #status: CoreStatus = { state: "starting" };
  #pending = new Map<number, Pending>();
  #lastId = PROBE_ID;
  #crashes: number[] = [];
  #attempt = 0;
  #stopping = false;
  #restartTimer: NodeJS.Timeout | null = null;
  #exited: Promise<void> = Promise.resolve();
  #startedAt = 0;
  readonly #statusListeners = new Set<(status: CoreStatus) => void>();
  readonly #eventListeners = new Set<(event: string) => void>();
  /** Start-up times of the core, from spawn to its first response, in milliseconds. */
  readonly startupTimesMs: number[] = [];

  constructor(options: CoreProcessOptions) {
    this.#options = {
      logLevel: "info",
      maxCrashes: 5,
      crashWindowMs: 60_000,
      restartDelayMs: (attempt) => Math.min(250 * 2 ** (attempt - 1), 30_000),
      stopTimeoutMs: 10_000,
      ...options,
    };
  }

  get status(): CoreStatus {
    return this.#status;
  }

  /** The process ID of the running core, if any. */
  get pid(): number | undefined {
    return this.#child?.pid;
  }

  onStatus(listener: (status: CoreStatus) => void): () => void {
    this.#statusListeners.add(listener);
    return () => this.#statusListeners.delete(listener);
  }

  onEvent(listener: (event: string) => void): () => void {
    this.#eventListeners.add(listener);
    return () => this.#eventListeners.delete(listener);
  }

  start(): void {
    if (this.#child !== null || this.#stopping) {
      return;
    }
    this.#startedAt = performance.now();
    const child = spawn(this.#options.executable, ["-data", this.#options.dataDir, "-log-level", this.#options.logLevel], {
      stdio: ["pipe", "pipe", "pipe"],
      windowsHide: true,
    });
    this.#child = child;
    let exit!: () => void;
    this.#exited = new Promise((resolve) => (exit = resolve));

    createInterface({ input: child.stdout, crlfDelay: Infinity }).on("line", (line) => this.#receive(line));
    createInterface({ input: child.stderr, crlfDelay: Infinity }).on("line", (line) => this.#options.onStderr?.(line));
    // A failed write means the core is gone; "exit" reports it.
    child.stdin.on("error", () => {});
    child.on("error", (error) => {
      // spawn failed (no such file...): there will be no "exit" event.
      if (this.#child === child) {
        this.#onExit(child, `could not start the core: ${error.message}`);
        exit();
      }
    });
    child.on("exit", (code, signal) => {
      if (this.#child === child) {
        this.#onExit(child, `the core exited (${signal ?? `status ${code}`})`);
      }
      exit();
    });
    // The core reads its input as soon as it has opened its database; this
    // first request tells when it is ready.
    this.#write(child, { id: PROBE_ID, command: "core.hello" });
  }

  /**
   * Sends a request, already validated (ipc.ts), and resolves with the JSON
   * response, carrying the caller's ID. Never rejects: when the core is not
   * running, the response is the "closed" error.
   */
  call(request: { id: number; command: string; params?: object }): Promise<string> {
    const child = this.#child;
    if (child === null || this.#status.state !== "ready") {
      return Promise.resolve(closedResponse(request.id, "the core is not running"));
    }
    const id = ++this.#lastId;
    return new Promise((resolve) => {
      this.#pending.set(id, { callerId: request.id, resolve });
      this.#write(child, { ...request, id });
    });
  }

  /** Closes the core and waits for it to exit; kills it if it takes too long. */
  async stop(): Promise<void> {
    this.#stopping = true;
    if (this.#restartTimer !== null) {
      clearTimeout(this.#restartTimer);
      this.#restartTimer = null;
    }
    const child = this.#child;
    if (child === null) {
      return;
    }
    child.stdin.end();
    let timer: NodeJS.Timeout | undefined;
    const timedOut = new Promise<boolean>((resolve) => {
      timer = setTimeout(() => resolve(true), this.#options.stopTimeoutMs);
    });
    if (await Promise.race([this.#exited.then(() => false), timedOut])) {
      child.kill();
      await this.#exited;
    }
    clearTimeout(timer);
  }

  #write(child: ChildProcessWithoutNullStreams, request: object): void {
    child.stdin.write(`${JSON.stringify(request)}\n`);
  }

  #receive(line: string): void {
    let doc: { id?: unknown };
    try {
      doc = JSON.parse(line) as { id?: unknown };
    } catch {
      return;
    }
    if (typeof doc.id !== "number") {
      for (const listener of this.#eventListeners) {
        listener(line);
      }
      return;
    }
    if (doc.id === PROBE_ID) {
      this.startupTimesMs.push(performance.now() - this.#startedAt);
      this.#attempt = 0;
      this.#setStatus({ state: "ready" });
      return;
    }
    const pending = this.#pending.get(doc.id);
    if (pending !== undefined) {
      this.#pending.delete(doc.id);
      pending.resolve(JSON.stringify({ ...doc, id: pending.callerId }));
    }
  }

  #onExit(child: ChildProcessWithoutNullStreams, reason: string): void {
    this.#child = null;
    child.stdin.destroy();
    for (const pending of this.#pending.values()) {
      pending.resolve(closedResponse(pending.callerId, reason));
    }
    this.#pending.clear();
    if (this.#stopping) {
      return;
    }
    const now = performance.now();
    this.#crashes = [...this.#crashes.filter((t) => now - t < this.#options.crashWindowMs), now];
    if (this.#crashes.length > this.#options.maxCrashes) {
      this.#setStatus({ state: "failed", reason });
      return;
    }
    this.#attempt++;
    this.#setStatus({ state: "restarting", attempt: this.#attempt });
    this.#restartTimer = setTimeout(() => {
      this.#restartTimer = null;
      this.start();
    }, this.#options.restartDelayMs(this.#attempt));
  }

  #setStatus(status: CoreStatus): void {
    this.#status = status;
    for (const listener of this.#statusListeners) {
      listener(status);
    }
  }
}

/** The response of the core API for a request that never reached the core. */
export function closedResponse(id: number, message: string): string {
  return JSON.stringify({ id, error: { code: "closed", message } });
}
