// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

// The thread list benchmark page (docs/ADR/0020). It shows one list
// (?list=NAME) of n synthetic messages (?n=N) and exposes the measurements
// as window.bench, driven by bench/run.mjs. Nothing is shown until the
// driver calls mount(), so that it can throttle the CPU first.

// First: the modules below evaluate after it.
import { polyfill } from "./legacy";
import { useSyncExternalStore } from "react";
import { flushSync } from "react-dom";
import { createRoot } from "react-dom/client";
import type { Message } from "../src/core-api/types.gen";
import { mergeMessages, OLDER_PAGE, type HistoryState } from "../src/store";
import "../src/styles.css";
import "./bench.css";
import { messages as generate } from "./data";
import { LISTS, type ListName } from "./lists";

polyfill();

const params = new URLSearchParams(location.search);
const listName = (params.get("list") ?? "plain") as ListName;
const List = LISTS[listName];
if (List === undefined) {
  throw new Error(`unknown list ${listName}`);
}
const count = Number(params.get("n") ?? "1000");
/** The number of the oldest message shown, lowered by prepend(). */
const FIRST = 1_000_000;

interface Shown {
  messages: readonly Message[];
  prepended: number;
  /** "start" until older() offers more, as the store does once the core has some. */
  history: HistoryState;
}

// The state of the page: the smallest external store, as in src/store.ts.
// ?history=more: the core has older messages from the start, as in the app.
let shown: Shown = {
  messages: generate(FIRST, FIRST + count),
  prepended: 0,
  history: params.get("history") === "more" ? { cursor: "older" } : "start",
};
let next = FIRST + count;
const listeners = new Set<() => void>();
function set(update: Shown): void {
  shown = update;
  for (const listener of listeners) {
    listener();
  }
}
function subscribe(listener: () => void): () => void {
  listeners.add(listener);
  return () => listeners.delete(listener);
}

/** The simulated core's answer for older messages, held until older() lets it go. */
let answerOlder: (() => void) | undefined;

/** Reads older messages as the store does (Store.loadOlder). */
function loadOlder(): void {
  if (typeof shown.history !== "object") {
    return;
  }
  set({ ...shown, history: "loading" });
  answerOlder = () => {
    answerOlder = undefined;
    const oldest = Number(shown.messages[0]!.message_id.slice(1));
    set({
      messages: mergeMessages(shown.messages, generate(oldest - OLDER_PAGE, oldest)),
      prepended: shown.prepended + OLDER_PAGE,
      history: { cursor: "older" },
    });
  };
}

function Bench() {
  const state = useSyncExternalStore(subscribe, () => shown);
  return (
    <main className="bench-main">
      <List messages={state.messages} prepended={state.prepended} history={state.history} onOlder={loadOlder} />
    </main>
  );
}

function frame(): Promise<number> {
  return new Promise((resolve) => requestAnimationFrame(resolve));
}

function scroller(): HTMLElement {
  const el = document.querySelector<HTMLElement>(".bench-scroller, [role=log]");
  if (el === null) {
    throw new Error("no scroller");
  }
  return el;
}

function row(id: string): HTMLElement | null {
  return document.querySelector<HTMLElement>(`[data-message-id="${id}"]`);
}

/** Whether the message is entirely inside the scroller's viewport. */
function visible(id: string): boolean {
  const el = row(id);
  if (el === null) {
    return false;
  }
  const box = el.getBoundingClientRect();
  const view = scroller().getBoundingClientRect();
  return box.top >= view.top - 1 && box.bottom <= view.bottom + 1;
}

/** Waits until a condition holds on a painted frame; resolves with the time it took, or -1. */
async function until(condition: () => boolean, timeoutMs = 10_000): Promise<number> {
  const start = performance.now();
  for (;;) {
    await frame();
    if (condition()) {
      return performance.now() - start;
    }
    if (performance.now() - start > timeoutMs) {
      return -1;
    }
  }
}

function lastId(): string {
  return shown.messages.at(-1)!.message_id;
}

function percentile(sorted: number[], p: number): number {
  return sorted[Math.min(sorted.length - 1, Math.floor((sorted.length * p) / 100))] ?? 0;
}

/** The message whose row is at a height of the scroller's viewport. */
function rowAt(y: number): HTMLElement | null {
  const view = scroller().getBoundingClientRect();
  const hit = document.elementFromPoint(view.left + view.width / 2, view.top + y);
  return hit?.closest<HTMLElement>("[data-message-id]") ?? null;
}

/**
 * Scrolls to `offset` pixels from the top, then waits until the position
 * holds: lists that measure rows move it. It jumps near it first, as
 * dragging the scroll bar does (this setup is not measured, and stepping
 * through a long thread took minutes), then scrolls the last screens as a
 * user does.
 */
async function scrollNear(offset: number): Promise<HTMLElement> {
  const el = scroller();
  el.scrollTop = Math.min(el.scrollTop, offset + 3 * el.clientHeight);
  await frame();
  // Positions snap to device pixels: stop within a pixel, and in any case.
  for (let i = 0; i < 1000 && el.scrollTop > offset + 1; i++) {
    el.scrollTop = Math.max(offset, el.scrollTop - el.clientHeight);
    await frame();
  }
  let top = -1;
  for (let i = 0; i < 50 && el.scrollTop !== top; i++) {
    top = el.scrollTop;
    for (let j = 0; j < 5; j++) {
      await frame();
    }
  }
  return el;
}

/** The message at the middle of the viewport, and where it is. */
function anchorAt(): { id: string | undefined; top: number } {
  const anchor = rowAt(scroller().clientHeight / 2);
  return { id: anchor?.dataset.messageId, top: anchor?.getBoundingClientRect().top ?? 0 };
}

/** How far that message moved since, or null if it left the DOM. */
function driftOf(before: { id: string | undefined; top: number }): number | null {
  const after = before.id === undefined ? null : row(before.id);
  return after === null ? null : Math.abs(after.getBoundingClientRect().top - before.top);
}

const root = createRoot(document.getElementById("root")!);

const bench = {
  list: listName,
  count,

  /**
   * Shows the list; resolves when the newest message is on screen. With
   * ?history=more, also tells whether the list asked for older messages
   * while opening at the bottom (it should not), then offers no more
   * history to the next scenarios.
   */
  async mount(): Promise<{ mountMs: number; bottomVisible: boolean; askedAtMount: boolean }> {
    const start = performance.now();
    flushSync(() => root.render(<Bench />));
    const waited = await until(() => visible(lastId()));
    // More frames: lists that measure their rows settle after the first.
    for (let i = 0; i < 10; i++) {
      await frame();
    }
    const askedAtMount = answerOlder !== undefined;
    answerOlder = undefined;
    flushSync(() => set({ ...shown, history: "start" }));
    return { mountMs: waited < 0 ? -1 : performance.now() - start, bottomVisible: visible(lastId()), askedAtMount };
  },

  /**
   * Scrolls up by `step` pixels per frame for `frames` frames, as a fast
   * fling does. Returns the frame intervals; with `checkBlank`, also the
   * share of probes (3 per frame) that found no message under them.
   */
  async scroll(frames: number, step: number, checkBlank: boolean) {
    const el = scroller();
    el.scrollTop = el.scrollHeight;
    await frame();
    let last = await frame();
    const intervals: number[] = [];
    let probes = 0;
    let blanks = 0;
    for (let i = 0; i < frames; i++) {
      el.scrollTop -= step;
      const now = await frame();
      intervals.push(now - last);
      last = now;
      if (checkBlank) {
        const height = el.clientHeight;
        for (const y of [height * 0.2, height * 0.5, height * 0.8]) {
          probes++;
          if (rowAt(y) === null) {
            blanks++;
          }
        }
      }
    }
    intervals.sort((a, b) => a - b);
    return {
      p50: percentile(intervals, 50),
      p95: percentile(intervals, 95),
      max: intervals.at(-1) ?? 0,
      over33: intervals.filter((t) => t > 33.4).length / intervals.length,
      blank: probes === 0 ? null : blanks / probes,
    };
  },

  /**
   * Adds k older messages while the user reads near the top; returns the
   * time to commit and to the next painted frame, and how far the message
   * the user was reading moved (null if it left the DOM).
   */
  async prepend(k: number) {
    await scrollNear(2000);
    const before = anchorAt();
    const start = performance.now();
    const oldest = Number(shown.messages[0]!.message_id.slice(1));
    flushSync(() => set({ ...shown, messages: [...generate(oldest - k, oldest), ...shown.messages], prepended: shown.prepended + k }));
    const commitMs = performance.now() - start;
    await frame();
    const frameMs = performance.now() - start;
    // Lists that measure new rows correct the position over a few frames.
    for (let i = 0; i < 10; i++) {
      await frame();
    }
    return { commitMs, frameMs, drift: driftOf(before) };
  },

  /**
   * The user scrolls up towards the top of what was read, and the list asks
   * for older messages by itself (onOlder). Returns how many came (0 if
   * the list did not ask within 10 s), and how far the message being read
   * moved when they came. The simulated core answers once that message is
   * on screen and still, so that the result does not depend on timing.
   */
  async older() {
    const el = await scrollNear(3 * scroller().clientHeight);
    // There is more to read from now on.
    flushSync(() => set({ ...shown, history: { cursor: "older" } }));
    const before = shown.messages.length;
    // A user's scroll up, into the last screen: the list starts reading.
    const target = Math.round(el.clientHeight / 2);
    for (let i = 0; i < 1000 && el.scrollTop > target + 1; i++) {
      el.scrollTop = Math.max(target, el.scrollTop - 100);
      await frame();
    }
    const asked = await until(() => answerOlder !== undefined);
    let anchor = anchorAt();
    if (asked >= 0) {
      // Lists render the rows of a new position over a few frames.
      await until(() => {
        const now = anchorAt();
        const still = now.id !== undefined && now.id === anchor.id && now.top === anchor.top;
        anchor = now;
        return still;
      });
      flushSync(() => answerOlder?.());
      for (let i = 0; i < 10; i++) {
        await frame();
      }
    }
    // Leave the page as it was for the next scenario.
    answerOlder = undefined;
    flushSync(() => set({ ...shown, history: "start" }));
    return { loaded: shown.messages.length - before, drift: driftOf(anchor) };
  },

  /** A new message arrives while the user is at the bottom; returns the time until it is on screen. */
  async append() {
    const el = scroller();
    el.scrollTop = el.scrollHeight;
    await until(() => visible(lastId()), 2_000);
    await frame();
    const start = performance.now();
    const message = generate(next, next + 1)[0]!;
    next++;
    flushSync(() => set({ ...shown, messages: [...shown.messages, message] }));
    const commitMs = performance.now() - start;
    const shownMs = await until(() => visible(message.message_id), 2_000);
    return { commitMs, shownMs: shownMs < 0 ? -1 : performance.now() - start, followed: visible(message.message_id) };
  },

  /** The status of the newest message changes (message.updated). */
  async update() {
    const start = performance.now();
    const index = shown.messages.length - 1;
    const message = { ...shown.messages[index]!, status: "failed" as const, error: "Not sent" };
    flushSync(() => set({ ...shown, messages: shown.messages.with(index, message) }));
    const commitMs = performance.now() - start;
    await frame();
    return { commitMs, frameMs: performance.now() - start };
  },

  /**
   * The cost of the provisional store's merge (src/store.ts) for one new
   * message in a conversation of this length: it copies and sorts them all.
   */
  storeMerge(rounds: number) {
    const message = generate(next, next + 1)[0]!;
    const start = performance.now();
    for (let i = 0; i < rounds; i++) {
      mergeMessages(shown.messages, [message]);
    }
    return { mergeMs: (performance.now() - start) / rounds };
  },

  memory() {
    const heap = (performance as Performance & { memory?: { usedJSHeapSize: number } }).memory?.usedJSHeapSize;
    return { nodes: document.getElementsByTagName("*").length, heapMiB: heap === undefined ? null : heap / 2 ** 20 };
  },
};

declare global {
  interface Window {
    bench?: typeof bench;
  }
}
window.bench = bench;
