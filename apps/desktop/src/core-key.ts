// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

// The master key of the core's secrets at rest, on macOS and Linux
// (docs/ADR/0027): the app keeps it in the core's key file (core.key in the
// core's data directory), encrypted by Electron's safeStorage (the macOS
// Keychain, or libsecret or KWallet on Linux), and gives it to the core on
// its standard input (core-process.ts).
//
// - Windows: the core keeps its own key file, protected by DPAPI
//   (docs/ADR/0019); the app does not take part.
// - No secure storage (a Linux desktop without a password store, where
//   safeStorage would fall back to a fixed password): the core keeps its
//   own plain key file, as before this task, and logs a warning.
// - A plain key file of the core, from before this task or from a session
//   without a secure storage, moves into the secure storage as soon as one
//   is available: the same key, so the sealed secrets still open.
// - A key file of the secure storage that does not open stops the core: a
//   new key would make every sealed secret unreadable. When the secure
//   storage cannot decrypt it, the key is lost (CoreKeyLostError,
//   docs/ADR/0031) and the user may start over.
//
// This module does not import Electron, so that it runs under plain Node in
// the tests.

import { randomBytes } from "node:crypto";
import { mkdirSync, readFileSync, renameSync, rmSync, writeFileSync } from "node:fs";
import { join } from "node:path";

/** What the app uses of Electron's safeStorage; the tests give a stand-in. */
export interface KeyStorage {
  isEncryptionAvailable(): boolean;
  /** Linux only: the password store behind safeStorage. */
  getSelectedStorageBackend?(): string;
  encryptString(plainText: string): Buffer;
  decryptString(encrypted: Buffer): string;
}

/** The core's key file, in its data directory (core/secrets/keyfile.go). */
export const KEY_FILE_NAME = "core.key";
/** The protection of a key file encrypted by safeStorage (core/secrets). */
export const SAFE_STORAGE_PROTECTION = "electron-safestorage";
const KEY_SIZE = 32;

export type CoreKey =
  /** The app gives this key, in base64, to the core. */
  | { source: "safe-storage"; key: string; event: "created" | "opened" | "moved" }
  /** The core keeps its own key file. */
  | { source: "core"; reason: string };

/** A key the app cannot give, and that the core must not replace. */
export class CoreKeyError extends Error {}

/**
 * The secure storage cannot decrypt the key: it is lost, and only starting
 * over gives a working core (docs/ADR/0031). An unavailable secure storage
 * is not a lost key: it may come back.
 */
export class CoreKeyLostError extends CoreKeyError {}

interface KeyFile {
  version: number;
  protection: string;
  key: string;
}

/**
 * Returns the key that the app gives to the core, creating it or moving the
 * core's plain key into the secure storage when needed, or says why the
 * core keeps its own. Throws a CoreKeyError when the key exists but cannot
 * be read: the core must not start then. Errors never quote the key.
 */
export function loadCoreKey(dataDir: string, storage: KeyStorage, platform: NodeJS.Platform = process.platform): CoreKey {
  if (platform === "win32") {
    return { source: "core", reason: "Windows: the core protects its key file with DPAPI" };
  }
  const path = join(dataDir, KEY_FILE_NAME);
  const file = readKeyFile(path);
  const unavailable = secureStorageUnavailable(storage, platform);
  if (file?.protection === SAFE_STORAGE_PROTECTION) {
    if (unavailable !== null) {
      throw new CoreKeyError(`the core's key is in the system's secure storage, which is not available: ${unavailable}`);
    }
    let key: string;
    try {
      key = storage.decryptString(Buffer.from(file.key, "base64"));
    } catch {
      throw new CoreKeyLostError("the system's secure storage could not decrypt the core's key");
    }
    if (!isKey(key)) {
      throw new CoreKeyError("the core's key file is not valid");
    }
    return { source: "safe-storage", key, event: "opened" };
  }
  if (unavailable !== null) {
    return { source: "core", reason: unavailable };
  }
  if (file === undefined) {
    const key = randomBytes(KEY_SIZE).toString("base64");
    writeKeyFile(dataDir, path, storage, key);
    return { source: "safe-storage", key, event: "created" };
  }
  if (file.protection === "none") {
    const key = file.key.trim();
    if (!isKey(key)) {
      throw new CoreKeyError("the core's key file is not valid");
    }
    writeKeyFile(dataDir, path, storage, key);
    return { source: "safe-storage", key, event: "moved" };
  }
  // DPAPI from a Windows machine, or a later format: the core reports it.
  return { source: "core", reason: `the core's key file has the protection "${file.protection}"` };
}

/** Why safeStorage cannot keep the key on this system, or null if it can. */
function secureStorageUnavailable(storage: KeyStorage, platform: NodeJS.Platform): string | null {
  if (!storage.isEncryptionAvailable()) {
    return "safeStorage cannot encrypt on this system";
  }
  if (platform === "linux") {
    // basic_text encrypts with a password written in Chromium's source
    // code: no better than the plain file, and misleading.
    const backend = storage.getSelectedStorageBackend?.() ?? "unknown";
    if (backend === "basic_text" || backend === "unknown") {
      return `no password store on this desktop (safeStorage backend: ${backend})`;
    }
  }
  return null;
}

function readKeyFile(path: string): KeyFile | undefined {
  let data: string;
  try {
    data = readFileSync(path, "utf8");
  } catch (error) {
    if ((error as NodeJS.ErrnoException).code === "ENOENT") {
      return undefined;
    }
    throw new CoreKeyError(`the core's key file cannot be read: ${(error as Error).message}`);
  }
  let file: unknown;
  try {
    file = JSON.parse(data);
  } catch {
    throw new CoreKeyError("the core's key file is not valid");
  }
  const f = file as Partial<KeyFile> | null;
  if (f === null || f.version !== 1 || typeof f.protection !== "string" || typeof f.key !== "string") {
    throw new CoreKeyError("the core's key file is not valid");
  }
  return f as KeyFile;
}

function isKey(key: string): boolean {
  return /^[A-Za-z0-9+/]+={0,2}$/.test(key) && Buffer.from(key, "base64").length === KEY_SIZE;
}

/**
 * Writes the key file encrypted by safeStorage, through a temporary file
 * renamed over the old one, so that a crash never leaves a partial key file.
 */
function writeKeyFile(dataDir: string, path: string, storage: KeyStorage, key: string): void {
  const file: KeyFile = { version: 1, protection: SAFE_STORAGE_PROTECTION, key: storage.encryptString(key).toString("base64") };
  mkdirSync(dataDir, { recursive: true, mode: 0o700 });
  const tmp = `${path}.tmp`;
  try {
    writeFileSync(tmp, JSON.stringify(file), { mode: 0o600 });
    renameSync(tmp, path);
  } catch (error) {
    rmSync(tmp, { force: true });
    throw new CoreKeyError(`the core's key file cannot be written: ${(error as Error).message}`);
  }
}
