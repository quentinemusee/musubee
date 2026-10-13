// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

// The core's key on macOS and Linux (core-key.ts), with a stand-in for
// safeStorage. The real safeStorage is in the end-to-end tests (e2e/).

import { mkdirSync, mkdtempSync, readFileSync, rmSync, statSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { afterEach, expect, test } from "vitest";
import { CoreKeyError, CoreKeyLostError, KEY_FILE_NAME, loadCoreKey, SAFE_STORAGE_PROTECTION, type KeyStorage } from "../src/core-key.ts";

const dirs: string[] = [];

afterEach(() => {
  for (const dir of dirs.splice(0)) {
    rmSync(dir, { recursive: true, force: true, maxRetries: 5 });
  }
});

function newDataDir(): string {
  const dir = mkdtempSync(join(tmpdir(), "musubee-core-key-"));
  dirs.push(dir);
  // The core's data directory, which the app creates when it is missing.
  return join(dir, "core");
}

const scramble = (data: Buffer) => Buffer.from(data.map((b) => b ^ 0x5a));

/** A stand-in for safeStorage: a reversible scrambling, and a switch. */
function fakeStorage(options: { available?: boolean; backend?: string; failDecrypt?: boolean } = {}): KeyStorage {
  return {
    isEncryptionAvailable: () => options.available ?? true,
    getSelectedStorageBackend: () => options.backend ?? "gnome_libsecret",
    encryptString: (plain) => Buffer.concat([Buffer.from("v11"), scramble(Buffer.from(plain))]),
    decryptString: (encrypted) => {
      if (options.failDecrypt || encrypted.subarray(0, 3).toString() !== "v11") {
        throw new Error("Error while decrypting the ciphertext provided to safeStorage.decryptString.");
      }
      return scramble(encrypted.subarray(3)).toString();
    },
  };
}

function readKeyFile(dataDir: string): { version: number; protection: string; key: string } {
  return JSON.parse(readFileSync(join(dataDir, KEY_FILE_NAME), "utf8")) as { version: number; protection: string; key: string };
}

function writeKeyFile(dataDir: string, content: object | string): void {
  mkdirSync(dataDir, { recursive: true });
  writeFileSync(join(dataDir, KEY_FILE_NAME), typeof content === "string" ? content : JSON.stringify(content));
}

const plainKey = Buffer.alloc(32, 7).toString("base64");

for (const platform of ["darwin", "linux"] as const) {
  test(`${platform}: a new key goes into the secure storage, and opens again`, () => {
    const dataDir = newDataDir();
    const storage = fakeStorage();
    const created = loadCoreKey(dataDir, storage, platform);
    if (created.source !== "safe-storage") {
      throw new Error(`source ${created.source}`);
    }
    expect(created.event).toBe("created");
    expect(Buffer.from(created.key, "base64")).toHaveLength(32);

    const file = readKeyFile(dataDir);
    expect(file).toMatchObject({ version: 1, protection: SAFE_STORAGE_PROTECTION });
    expect(storage.decryptString(Buffer.from(file.key, "base64"))).toBe(created.key);
    expect(readFileSync(join(dataDir, KEY_FILE_NAME), "utf8")).not.toContain(created.key);
    if (process.platform !== "win32") {
      expect(statSync(join(dataDir, KEY_FILE_NAME)).mode & 0o777).toBe(0o600);
      expect(statSync(dataDir).mode & 0o777).toBe(0o700);
    }

    expect(loadCoreKey(dataDir, storage, platform)).toEqual({ source: "safe-storage", key: created.key, event: "opened" });
  });
}

test("Windows: the core keeps its DPAPI key file, the app writes nothing", () => {
  const dataDir = newDataDir();
  expect(loadCoreKey(dataDir, fakeStorage(), "win32")).toMatchObject({ source: "core" });
  expect(() => statSync(dataDir)).toThrow();
});

test("without a secure storage, the core keeps its own key file", () => {
  for (const [storage, platform] of [
    [fakeStorage({ available: false }), "darwin"],
    [fakeStorage({ backend: "basic_text" }), "linux"],
    [fakeStorage({ backend: "unknown" }), "linux"],
  ] as const) {
    const dataDir = newDataDir();
    const key = loadCoreKey(dataDir, storage, platform);
    expect(key.source).toBe("core");
    expect(() => statSync(dataDir)).toThrow();
  }
  // The backend matters on Linux only.
  expect(loadCoreKey(newDataDir(), fakeStorage({ backend: "basic_text" }), "darwin").source).toBe("safe-storage");
});

test("the core's plain key moves into the secure storage, unchanged", () => {
  const dataDir = newDataDir();
  writeKeyFile(dataDir, { version: 1, protection: "none", key: plainKey });
  const storage = fakeStorage();
  expect(loadCoreKey(dataDir, storage, "linux")).toEqual({ source: "safe-storage", key: plainKey, event: "moved" });
  const file = readKeyFile(dataDir);
  expect(file.protection).toBe(SAFE_STORAGE_PROTECTION);
  expect(storage.decryptString(Buffer.from(file.key, "base64"))).toBe(plainKey);
  expect(readFileSync(join(dataDir, KEY_FILE_NAME), "utf8")).not.toContain(plainKey);
  expect(loadCoreKey(dataDir, storage, "linux")).toEqual({ source: "safe-storage", key: plainKey, event: "opened" });
});

test("the core's plain key stays where it is without a secure storage", () => {
  const dataDir = newDataDir();
  const content = JSON.stringify({ version: 1, protection: "none", key: plainKey });
  writeKeyFile(dataDir, content);
  expect(loadCoreKey(dataDir, fakeStorage({ backend: "basic_text" }), "linux").source).toBe("core");
  expect(readFileSync(join(dataDir, KEY_FILE_NAME), "utf8")).toBe(content);
});

test("a key of the secure storage that does not open stops the core, and stays", () => {
  const dataDir = newDataDir();
  const storage = fakeStorage();
  const created = loadCoreKey(dataDir, storage, "linux");
  const content = readFileSync(join(dataDir, KEY_FILE_NAME), "utf8");
  for (const other of [fakeStorage({ failDecrypt: true }), fakeStorage({ available: false }), fakeStorage({ backend: "basic_text" })]) {
    let error: unknown;
    try {
      loadCoreKey(dataDir, other, "linux");
    } catch (e) {
      error = e;
    }
    expect(error).toBeInstanceOf(CoreKeyError);
    // Only a key that the secure storage cannot decrypt is lost
    // (docs/ADR/0031): an unavailable secure storage may come back.
    expect(error instanceof CoreKeyLostError, (error as Error).message).toBe(other.isEncryptionAvailable() && other.getSelectedStorageBackend?.() !== "basic_text");
    expect((error as Error).message).not.toContain(created.source === "safe-storage" ? created.key : "");
    expect(readFileSync(join(dataDir, KEY_FILE_NAME), "utf8")).toBe(content);
  }
});

test("an invalid key file stops the core, and stays", () => {
  for (const content of [
    "not json",
    "null",
    JSON.stringify({ version: 2, protection: "none", key: plainKey }),
    JSON.stringify({ version: 1, protection: "none", key: Buffer.alloc(16).toString("base64") }),
    JSON.stringify({ version: 1, protection: "none", key: "not base64!" }),
    JSON.stringify({ version: 1, protection: SAFE_STORAGE_PROTECTION, key: fakeStorage().encryptString("short").toString("base64") }),
  ]) {
    const dataDir = newDataDir();
    writeKeyFile(dataDir, content);
    expect(() => loadCoreKey(dataDir, fakeStorage(), "linux"), content).toThrow(CoreKeyError);
    expect(() => loadCoreKey(dataDir, fakeStorage(), "linux"), content).not.toThrow(CoreKeyLostError);
    expect(readFileSync(join(dataDir, KEY_FILE_NAME), "utf8")).toBe(content);
  }
});

test("a DPAPI key file from Windows is left to the core, which reports it", () => {
  const dataDir = newDataDir();
  writeKeyFile(dataDir, { version: 1, protection: "dpapi", key: "AQID" });
  expect(loadCoreKey(dataDir, fakeStorage(), "darwin")).toMatchObject({ source: "core" });
});
