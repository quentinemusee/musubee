// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

// The supervisor with the real core (built by global-setup.ts).

import { existsSync, mkdtempSync, readFileSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { afterEach, describe, expect, inject, test } from "vitest";
import { CoreProcess, type CoreProcessOptions, type CoreStatus } from "../src/core-process.ts";
import { setAside } from "../src/start-over.ts";

const running: CoreProcess[] = [];
const dirs: string[] = [];

afterEach(async () => {
  await Promise.all(running.splice(0).map((core) => core.stop()));
  for (const dir of dirs.splice(0)) {
    rmSync(dir, { recursive: true, force: true, maxRetries: 5 });
  }
});

function newCore(options: Partial<CoreProcessOptions> = {}): { core: CoreProcess; statuses: CoreStatus[]; dataDir: string } {
  const dataDir = mkdtempSync(join(tmpdir(), "musubee-core-process-"));
  dirs.push(dataDir);
  const core = new CoreProcess({ executable: inject("coreExecutable"), dataDir, restartDelayMs: () => 50, ...options });
  const statuses: CoreStatus[] = [];
  core.onStatus((status) => statuses.push(status));
  running.push(core);
  return { core, statuses, dataDir };
}

function until(predicate: () => boolean, what: string, timeoutMs = 30_000): Promise<void> {
  return new Promise((resolve, reject) => {
    const started = Date.now();
    const timer = setInterval(() => {
      if (predicate()) {
        clearInterval(timer);
        resolve();
      } else if (Date.now() - started > timeoutMs) {
        clearInterval(timer);
        reject(new Error(`timed out waiting for ${what}`));
      }
    }, 10);
  });
}

const ready = (core: CoreProcess) => until(() => core.status.state === "ready", "the core to be ready");

function isRunning(pid: number): boolean {
  try {
    process.kill(pid, 0);
    return true;
  } catch {
    return false;
  }
}

describe("CoreProcess", () => {
  test("starts the core and reports its start-up time", async () => {
    const { core, statuses } = newCore();
    core.start();
    await ready(core);
    expect(statuses).toEqual([{ state: "ready" }]);
    expect(core.startupTimesMs).toHaveLength(1);
    expect(core.startupTimesMs[0]).toBeGreaterThan(0);
  });

  test("gives each request its own ID and returns the caller's", async () => {
    const { core } = newCore();
    core.start();
    await ready(core);
    // Two windows both use ID 1.
    const [a, b] = await Promise.all([
      core.call({ id: 1, command: "debug.ping", params: { payload: "a" } }),
      core.call({ id: 1, command: "debug.ping", params: { payload: "b" } }),
    ]);
    expect(JSON.parse(a)).toEqual({ id: 1, result: { payload: "a" } });
    expect(JSON.parse(b)).toEqual({ id: 1, result: { payload: "b" } });
  });

  test("forwards the core's events", async () => {
    const { core } = newCore();
    const events: string[] = [];
    core.onEvent((event) => events.push(event));
    core.start();
    await ready(core);
    const step = JSON.parse(await core.call({ id: 1, command: "login.start", params: { network_id: "echo", flow_id: "username" } }));
    await core.call({ id: 2, command: "login.submit", params: { process_id: step.result.process_id, values: { username: "events" } } });
    await until(() => events.some((e) => e.includes('"conversation.updated"')), "a conversation.updated event");
  });

  test("answers closed while the core is not running", async () => {
    const { core } = newCore();
    expect(JSON.parse(await core.call({ id: 3, command: "core.hello" }))).toMatchObject({ id: 3, error: { code: "closed" } });
  });

  test("starts the core again after a crash, and fails the requests it was running", async () => {
    const { core, statuses } = newCore();
    core.start();
    await ready(core);
    const step = JSON.parse(await core.call({ id: 1, command: "login.start", params: { network_id: "echo", flow_id: "code" } }));
    // login.wait waits for the echo delay (a minute by default): still running when the core dies.
    const waiting = core.call({ id: 2, command: "login.wait", params: { process_id: step.result.process_id } });
    const pid = core.pid!;
    process.kill(pid);
    expect(JSON.parse(await waiting)).toMatchObject({ id: 2, error: { code: "closed" } });
    await until(() => core.status.state === "ready" && core.pid !== pid, "the restarted core");
    expect(statuses).toEqual([{ state: "ready" }, { state: "restarting", attempt: 1 }, { state: "ready" }]);
    expect(JSON.parse(await core.call({ id: 3, command: "core.hello" }))).toMatchObject({ id: 3, result: { api_version: "1.7" } });
  });

  test("gives up when the core keeps failing", async () => {
    const { core, statuses } = newCore({ executable: join(tmpdir(), "no-such-musubee-core"), maxCrashes: 2 });
    core.start();
    await until(() => core.status.state === "failed", "the failed status");
    expect(statuses.map((s) => s.state)).toEqual(["restarting", "restarting", "failed"]);
    expect(core.status).toMatchObject({ reason: expect.stringContaining("could not start the core") });
  });

  test("gives the core the app's key on its input, at every start", async () => {
    const { core, statuses, dataDir } = newCore();
    const databaseKey = Buffer.alloc(32, 9).toString("base64");
    core.start({ databaseKey });
    await ready(core);
    const pid = core.pid!;
    process.kill(pid);
    // Without the key on its input, the restarted core would read the first
    // request as its key and exit.
    await until(() => core.status.state === "ready" && core.pid !== pid, "the restarted core");
    expect(statuses).toEqual([{ state: "ready" }, { state: "restarting", attempt: 1 }, { state: "ready" }]);
    await core.stop();
    // The core kept no key file of its own, and logged where its key came from, not the key.
    expect(existsSync(join(dataDir, "core.key"))).toBe(false);
    const log = readFileSync(join(dataDir, "core.log"), "utf8");
    expect(log.match(/"protection":"host"/g)).toHaveLength(2);
    expect(log).not.toContain(databaseKey);
  });

  test("fail() reports a core that cannot start, without starting it", () => {
    const { core, statuses } = newCore();
    core.fail("the core's key file is not valid");
    expect(statuses).toEqual([{ state: "failed", reason: "the core's key file is not valid" }]);
    expect(core.pid).toBeUndefined();
  });

  test("fails at once when the core's key is lost, and starts over once the data is set aside", async () => {
    const first = newCore();
    first.core.start({ databaseKey: Buffer.alloc(32, 1).toString("base64") });
    await ready(first.core);
    await first.core.stop();

    // Another key on the same data: the core exits with status 3, which no
    // restart can help (docs/ADR/0031).
    const { core, statuses } = newCore({ dataDir: first.dataDir });
    core.start({ databaseKey: Buffer.alloc(32, 2).toString("base64") });
    await until(() => core.status.state === "failed", "the failed status");
    expect(statuses).toEqual([{ state: "failed", reason: "the core exited (status 3): the core's key is lost", keyLost: true }]);

    const aside = setAside(first.dataDir);
    expect(aside).not.toBeNull();
    dirs.push(aside!);
    core.prepareRestart();
    core.start({ databaseKey: Buffer.alloc(32, 3).toString("base64") });
    await ready(core);
    expect(statuses.slice(1)).toEqual([{ state: "starting" }, { state: "ready" }]);
    expect(existsSync(join(aside!, "core.db"))).toBe(true);
  });

  test("prepareRestart() refuses while the core has not failed", async () => {
    const { core } = newCore();
    expect(() => core.prepareRestart()).toThrow("not stopped after a failure");
    core.start();
    await ready(core);
    expect(() => core.prepareRestart()).toThrow("not stopped after a failure");
  });

  test("stop() closes the core cleanly", async () => {
    const { core, statuses } = newCore();
    core.start();
    await ready(core);
    const pid = core.pid!;
    await core.stop();
    expect(isRunning(pid)).toBe(false);
    expect(core.pid).toBeUndefined();
    // A stop is not a crash.
    expect(statuses).toEqual([{ state: "ready" }]);
  });
});
