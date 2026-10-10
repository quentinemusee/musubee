// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

// The few recent JavaScript functions that the benchmark, the store and
// react-virtuoso call, for old engines such as the WebView 83 of the
// Android 11 emulator image (built with MUSUBEE_BENCH_TARGET=chrome83,
// see ui/README.md). Recent engines keep their own.

function relative(length: number, index: number): number {
  const i = Math.trunc(index) || 0;
  return i < 0 ? length + i : i;
}

function at<T>(this: T[], index: number): T | undefined {
  const i = relative(this.length, index);
  return i < 0 || i >= this.length ? undefined : this[i];
}

function withValue<T>(this: T[], index: number, value: T): T[] {
  const i = relative(this.length, index);
  if (i < 0 || i >= this.length) {
    throw new RangeError("Invalid index");
  }
  const copy = [...this];
  copy[i] = value;
  return copy;
}

function toSorted<T>(this: T[], compare?: (a: T, b: T) => number): T[] {
  // oxlint-disable-next-line unicorn/no-array-sort -- sorts a copy: this is toSorted.
  return [...this].sort(compare);
}

function hasOwn(object: object, key: PropertyKey): boolean {
  return Object.prototype.hasOwnProperty.call(object, key);
}

/** Adds the functions this engine lacks. Importing this module first calls it. */
// oxlint-disable no-extend-native -- a polyfill extends the built-ins by design.
export function polyfill(): void {
  Array.prototype.at ??= at;
  Array.prototype.with ??= withValue;
  Array.prototype.toSorted ??= toSorted;
  Object.hasOwn ??= hasOwn;
}

polyfill();
