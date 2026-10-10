# ADR 0020 — UI state and the thread list: the app's own store, and a list virtualized with virtua

- **Status**: accepted (measured on Electron and a throttled Android emulator; the measurement on a low-end phone waits for one)
- **Date**: 2026-10-10
- **Task**: T2.4
- **Deciders**: Claude Code (design assistant), under the maintainer's standing autonomy instruction; to be reviewed by the maintainer

## Context

`CLAUDE.md` §4, question 6: choose the interface's state management and a virtualized message list, with performance measurements on low-end Android.

Until T2.4:

- **State**: the store of T1.4 (`ui/src/store.ts`) is a small class of our own: an immutable `State`, a set of listeners, and React's `useSyncExternalStore` through `useAppState(selector)` (`ui/src/app/state.ts`). Core events update it (`message.added`, `message.updated`…).
- **Thread**: `Thread.tsx` rendered every message of the conversation in an `<ol role="log" aria-live="polite">` and scrolled to the end on each new message. Only the latest page of messages was ever read (`messages.list` without `before`); older messages could not be shown.
- **Merging a message**: `mergeMessages` copied the conversation's messages into a `Map`, then sorted them all, for each event.

A Telegram group or a long personal conversation has thousands of messages. The interface runs in Chromium everywhere: Electron on the desktop, the system WebView on Android (Capacitor), WKWebView on iOS later. Low-end Android phones are the constraint (`CLAUDE.md` §4).

Requirements for the thread:

1. Open at the newest message.
2. Follow new messages while the user is at the bottom, and only then.
3. Read older messages as the user nears the top (reverse infinite scroll), **without moving the message being read**.
4. Rows of variable height (text wraps; media later, E2.4), measured rather than fixed.
5. Status changes of a message (sending → sent → failed) cheap.
6. Accessible: a keyboard can scroll it, screen readers can read it, and new messages are announced.
7. Small, maintained, AGPL-compatible dependency; works under the app's Content Security Policy (`style-src 'self'`, no inline `<style>`).

## Options considered

### State management

- **A — Keep the app's own store** (`useSyncExternalStore`, selectors returning parts of the state).
- **B — Zustand** (MIT, about 1 KB): the same model (an external store and selectors), with middleware.
- **C — Redux Toolkit** (MIT): reducers and actions, DevTools; more code for the same result.
- **D — Atom libraries** (Jotai, MIT; Recoil, archived): state split into atoms.

The store of T1.4 already does what B would: components subscribe to a selected part and render again only when it changes. The one measured problem was algorithmic (`mergeMessages` sorting everything for each event, see Measurements), and a library would not change it. The store's own tests (`ui/src/store.test.ts`, 12 tests) drive it with a scripted core.

### Thread list

1. **Plain DOM**: every message rendered. Chromium's scroll anchoring (`overflow-anchor`) keeps the visible messages in place when older ones are added above.
2. **CSS `content-visibility: auto`**: every message in the DOM, but the browser skips the layout and paint of the rows off screen (`contain-intrinsic-size` as the placeholder).
3. **react-virtuoso** 4.18.16 (MIT): built for chats (`firstItemIndex` for prepending, `followOutput`, `initialTopMostItemIndex`).
4. **@tanstack/react-virtual** 3.14.14 (MIT, with `@tanstack/virtual-core` 3.18.0): a headless virtualizer; no built-in support for keeping the position when items are added above.
5. **virtua** 0.53.3 (MIT): a small virtualizer with a `shift` option for items added at the start ("reverse infinite scrolling").

Bundle size of each library alone (esbuild 0.28.2, minified / gzip, bytes): react-virtuoso 60 356 / 20 122; @tanstack/react-virtual 26 847 / 8 090; virtua 8 108 / 4 206.

## Method

A benchmark page (`ui/bench/`, built with the app's Vite settings and its Content Security Policy) shows one list of `n` synthetic messages (deterministic: 70 % short, 25 % medium, 5 % long), with the app's message component (`MessageRow`). `ui/bench/run.mjs` drives it with Playwright and throttles the CPU through the DevTools protocol (`Emulation.setCPUThrottlingRate`, ×1 and ×4). Each list does the same scenarios, three runs each, and the medians are kept:

| Measure | Scenario |
|---|---|
| mount | time until the newest message is entirely on screen |
| nodes, heap | DOM elements and JS heap after mounting |
| update | a status change of the newest message, to the next painted frame |
| append | a new message while at the bottom, until it is on screen (and whether it was followed) |
| scroll p50 / p95, > 33 ms | frame intervals while scrolling up 400 px per frame (a fast fling), and the share of frames over 33 ms |
| blank | share of probes (3 per frame, at 20 %, 50 % and 80 % of the height) that found no message during the fling |
| prepend, drift | 200 older messages added while the user reads near the top: time to the next frame, and how far the message at the middle moved (lost: it left the DOM) |
| merge | the store's `mergeMessages` for one new message in a conversation of `n` messages |
| older (app only) | the user scrolls up into the last screen: the list must ask for older messages by itself; the simulated core answers once the message being read is still, and that message must not move |

Environments:

- **Electron** 44.7.0 (the desktop app's), Windows 11, Intel Core i7-9750H, 16 GB; a 412 × 860 window shown inactive (a hidden window gets one frame per second), on a 240 Hz display (frames of 4.2 ms).
- **Android emulator**, API 30 (Android 11) x86 image, 4 virtual CPUs on the same host, 60 Hz; the page served by the debug app (Capacitor). Its **System WebView is 83** (2020), the version shipped in that image: the page had to be built for `chrome83` with four small polyfills (`Array.prototype.at`, `with`, `toSorted`, `Object.hasOwn`, `ui/bench/legacy.ts`). An old engine on an emulated CPU makes these numbers pessimistic. WebView 83 rounds `performance.memory`, so the heap is not reported there.

The full results are in `ui/bench/results/` (one JSON line per run, then the medians).

## Measurements

### Electron, 20 000 messages, CPU ×4 (medians)

| List | mount | nodes | heap | update | append | scroll p95 | > 33 ms | blank | prepend | drift |
|---|---|---|---|---|---|---|---|---|---|---|
| plain | 12 730 ms | 120 014 | 33.7 MiB | 108 ms | 446 ms | 121 ms | 50 % | 0 | 1 245 ms | 0.4 px |
| content-visibility | 6 356 ms | 120 014 | 35.4 MiB | 84 ms | 1 040 ms | 763 ms | 99 % | 0 | 1 143 ms | 0 |
| react-virtuoso | 227 ms | 50 | 7.2 MiB | 8 ms | 34 ms | 29 ms | 2 % | 0 | 1 395 ms | 0 |
| tanstack-virtual | 303 ms | 92 | 7.3 MiB | 21 ms | 59 ms | 54 ms | 34 % | 0 | 86 ms | lost |
| virtua | 190 ms | 56 | 7.1 MiB | 7 ms | 14 ms | 25 ms | 1 % | 12 % | 32 ms | 0.1 px |

### Electron, 5 000 messages, CPU ×4

| List | mount | nodes | update | append | scroll p95 | > 33 ms | blank | prepend | drift |
|---|---|---|---|---|---|---|---|---|---|
| plain | 3 648 ms | 30 014 | 40 ms | 141 ms | 46 ms | 17 % | 0 | 475 ms | 0.4 px |
| content-visibility | 1 637 ms | 30 014 | 60 ms | 260 ms | 233 ms | 99 % | 0 | 325 ms | 0 |
| react-virtuoso | 232 ms | 71 | 7 ms | 22 ms | 34 ms | 8 % | 0 | 197 ms | 0 |
| tanstack-virtual | 263 ms | 113 | 12 ms | 27 ms | 38 ms | 13 % | 0 | 86 ms | lost |
| virtua | 230 ms | 77 | 5 ms | 21 ms | 29 ms | 1 % | 11 % | 22 ms | 0.1 px |

### Android emulator (WebView 83), 5 000 messages

| List | CPU | mount | nodes | update | append | scroll p95 | > 33 ms | blank | prepend | drift |
|---|---|---|---|---|---|---|---|---|---|---|
| plain | ×1 | 2 206 ms | 30 015 | 65 ms | 46 ms | 33 ms | 1 % | 0 | 238 ms | 0 |
| content-visibility | ×1 | 2 227 ms | 30 015 | 79 ms | 52 ms | 50 ms | 5 % | 0 | 221 ms | 0 |
| react-virtuoso | ×1 | **never at the bottom** | 72 | 8 ms | 38 ms | 50 ms | 18 % | 0 | 41 ms | 0 |
| tanstack-virtual | ×1 | 136 ms | 114 | 17 ms | 29 ms | 50 ms | 15 % | 0 | 28 ms | lost |
| virtua | ×1 | 87 ms | 78 | 14 ms | 19 ms | 50 ms | 5 % | 21 % | 21 ms | 0.4 px |
| plain | ×4 | 9 000 ms | 30 015 | 154 ms | 236 ms | 67 ms | 16 % | 0 | 1 211 ms | 0 |
| content-visibility | ×4 | 9 563 ms | 30 015 | 253 ms | 181 ms | 67 ms | 28 % | 0 | 1 126 ms | 0 |
| react-virtuoso | ×4 | **never at the bottom** | 72 | 16 ms | 46 ms | 83 ms | 74 % | 0 | 99 ms | 0 |
| tanstack-virtual | ×4 | 563 ms | 114 | 22 ms | 56 ms | 83 ms | 37 % | 0 | 106 ms | lost |
| virtua | ×4 | 284 ms | 78 | 24 ms | 30 ms | 67 ms | 35 % | **100 %** (2 runs of 3) | 20 ms | 0.4 px |

At 1 000 messages on the emulator, react-virtuoso also failed to follow a new message (append: never on screen).

### The app's list (`MessageList`: virtua with `bufferSize` 800)

| Environment | n | CPU | mount | nodes | update | append | scroll p95 | > 33 ms | blank | prepend, drift | older: loaded, drift |
|---|---|---|---|---|---|---|---|---|---|---|---|
| Android emulator | 5 000 | ×1 | 65 ms | 129 | 12 ms | 17 ms | 33 ms | 3 % | 0 | 19 ms, 0.4 px | 100, 0.6 px |
| Android emulator | 5 000 | ×4 | 221 ms | 129 | 19 ms | 31 ms | 67 ms | 41 % | 0 | 28 ms, 0.4 px | 100, 0.6 px |
| Electron | 5 000 | ×1 | 34 ms | 135 | 1 ms | 5 ms | 4 ms | 0 | 0 | 4 ms, 0.1 px | 100, 0.1 px |
| Electron | 5 000 | ×4 | 391 ms | 135 | 13 ms | 25 ms | 54 ms | 25 % | 0 | 27 ms, 0.1 px | 100, 0.1 px |
| Electron | 20 000 | ×1 | 41 ms | 93 | 2 ms | 5 ms | 8 ms | 0 | 0 | 6 ms, 0.1 px | 100, 0.1 px |
| Electron | 20 000 | ×4 | 319 ms | 93 | 11 ms | 28 ms | 50 ms | 20 % | 0 | 28 ms, 0.1 px | 100, 0.1 px |

The Electron rows come from a later session than the comparison tables, on a busier machine: in that session virtua alone gave a fling p95 of 46 ms at 5 000 messages ×4, against 29 ms in the first. Compare rows of one session only.

virtua's default `bufferSize` (200 px beyond the viewport) left blank areas during the fling on the emulator (21 % at ×1, up to 100 % at ×4). On the emulator at 5 000 messages, 200 / 800 / 1 600 px gave blank areas of 21 % / 0 / 0 with 80 / 129 / 192 nodes; the frames over 33 ms at ×4 stayed between 29 % and 48 % in every case, which is the noise between runs. On Electron, in two interleaved rounds (200, 800, 200, 800) at ×4, 800 px removed the blank areas (11 to 12 % with 200 px) but raised the fling's p95 from 33–37 ms to 42–50 ms and its frames over 33 ms from 4–7 % to 9–15 %. The app keeps 800 px: blank areas are visible on every device, while the extra frame time appears only with the CPU slowed 4 times (at ×1 the fling stays at 4 to 8 ms).

After a jump (dragging the scroll bar) into rows never measured, virtua shows no message for about 10 frames, whatever the buffer, then renders them. This is visible only on a slow device and only after a jump.

### The store

`mergeMessages` for one new message, before T2.4 (copy into a `Map` and sort everything): 11 to 20 ms per event at 20 000 messages on Electron at ×4 (medians of the five lists' sessions), so a busy group would spend most of each frame merging. T2.4 adds a path for the common case (one message: the newest, or an update of one of the last 64, replaced in place): 0.7 ms at 20 000 messages on Electron at ×1 and 4 to 6 ms at ×4 (before: 2.7 and 11 to 20 ms); 0.2 ms at 5 000 messages on the emulator at ×1 and 1.1 ms at ×4. What remains is a scan to make sure the message is new and the copy of the array that an immutable state requires; a chunked structure would remove it if conversations that long become common. The general path (pages of older messages, out-of-order updates) still sorts, once per page.

## Decision

1. **State: keep the app's own store** (option A), with two changes: the **history cursor** of each conversation (`history`: a cursor, `"loading"` or `"start"`) and **`loadOlder()`**, which reads pages of 100 older messages through `messages.list` with `before`; and the **fast path of `mergeMessages`** for single messages. No state library.
2. **Thread list: virtua**, wrapped in `ui/src/app/MessageList.tsx`, with `bufferSize` 800. Each row is `MessageRow` (a memoized component). The list:
   - opens at the newest message and follows new ones while the user is at the bottom;
   - calls `onOlder` (the store's `loadOlder`) when the user is within one viewport of the top, or when a short conversation does not fill the viewport;
   - sets virtua's `shift` when messages were added above (the first message changed, the last did not), so the message being read stays in place;
   - is rendered once per conversation (`key`), so that its scroll state does not leak from one conversation to another.

Why virtua: the fastest mount, update, append and prepend on both targets, the smallest frame times on Electron, a stable position when older messages arrive, and the smallest bundle (4 KB gzip, no dependency). Its one weakness, blank areas in very fast flings, is fixed by the buffer, at the cost of some frame time on a slowed desktop (see above). react-virtuoso is close on Electron but failed to open at the newest message and to follow new messages on the Android WebView, its prepend is slow at 20 000 messages (1.4 s), and it is five times larger. @tanstack/react-virtual loses the message being read when older ones are added (no built-in support; our correction by estimated size was not enough). Keeping every message in the DOM, with or without `content-visibility`, takes seconds to open a long conversation and makes every frame slow; `content-visibility` was worse than plain DOM for scrolling.

### Accessibility

- The list keeps **`role="log"`**, named after the conversation (`aria-labelledby` its heading), but **`aria-live="off"`**: rows mount as the user scrolls, and in a live region a screen reader would announce each of them as news. New messages from others are announced by a separate, visually hidden `<output>` ("sender: text"). Since the page now holds more than one status, the app's status line is named ("App status").
- The scroll container takes the keyboard focus (`tabIndex=0`), so arrow keys and Page Up / Page Down scroll it.
- A screen reader only sees the rows in the DOM (about 15 to 30 around the viewport): reading the whole history means scrolling. Positions in the set (`aria-setsize`, `aria-posinset`) are not given yet, because the full history is not known; to revisit with E2.4.
- **Find in page** (Ctrl+F) only finds the rendered messages. Searching a conversation belongs to the core (its database has every message), with a search field in the thread: a follow-up for E2.4.

### Old WebViews

The interface is built for Vite's default target, the "Baseline widely available" set: Chrome and Edge 111, Firefox 114, Safari and iOS 16.4 (`ESBUILD_BASELINE_WIDELY_AVAILABLE_TARGET` in Vite 8.3.4). It **does not load on WebView 83** (a syntax error: private class methods, `??=`). The Android app does not load the interface yet (its `www/` is still the test page of T1.3); when it does, set Capacitor's `android.minWebViewVersion` to match the build target, so that a phone with an outdated WebView gets Capacitor's message rather than a blank screen; iOS 16.4 is the matching floor for WKWebView (E3.3). The System WebView updates from the Play Store on Android 7 and later; phones without the Play Store may keep an old one.

## State of knowledge

| Claim | Status | Source |
|---|---|---|
| virtua renders only the rows near the viewport and keeps the position from the end with `shift` | **verified** (measured: nodes, drift) | `ui/bench/results/`, virtua 0.53.3 `VirtualizerProps.shift` |
| `MessageList` opens at the bottom, follows new messages, reads older ones by itself and keeps the message being read in place | **verified** in Electron (Windows) and the Android emulator (API 30, WebView 83) | `npm run bench:check`, `ui/bench/results/` |
| The list works under the app's Content Security Policy | **verified** (the benchmark page uses the same policy; React sets inline styles through the CSSOM, which `style-src 'self'` allows) | `ui/bench/index.html` |
| react-virtuoso does not open at the newest message on WebView 83 with 5 000 messages | **verified** on the emulator; whether the cause is the old engine or the emulator is **unknown** | `ui/bench/results/android-emulator-2026-10-10.txt` |
| The numbers hold on a low-end phone | **unknown**: no low-end phone yet; the emulator with an old engine and ×4 throttling is meant to be pessimistic | T2.4 deliverable 📞 |
| Screen readers announce rows mounted inside a live region, hence `aria-live="off"` | **assumed** (WAI-ARIA: a polite live region announces added nodes); not tested with NVDA or TalkBack | WAI-ARIA 1.2, `log` role, `aria-live` |
| The Electron and Linux CI runners pass the regression test's limits | **verified** locally; CI to be confirmed by the pull request | `.github/workflows/checks.yml` |

## Consequences

- Long conversations open in about 0.1 to 0.3 s and stay smooth; older messages load as the user scrolls up.
- A regression test runs on every pull request in the desktop job (Linux, Windows, macOS): `npm run bench:check` measures `MessageList` in Electron at 5 000 messages with the CPU throttled 4 times, and fails if the DOM holds more than 500 nodes, if the message being read moves by more than 2 px when older messages arrive (added by the test, or read by the list itself), if the newest message is not shown or followed, if the list reads older messages while opening at the newest one (it must wait until the user scrolls up), or if a time is about five times above the desktop medians (limits in `ui/bench/run.mjs`, `CHECKS`).
- virtua is at version 0.x: its API may change. `MessageList` is the only file that uses it, and the regression test checks the behaviour at each update. The comparison benchmark stays (`npm run bench`), with react-virtuoso and @tanstack/react-virtual as development dependencies only, to measure again on a low-end phone or after a major update.
- Browser find-in-page no longer finds messages off screen; conversation search moves to the core (E2.4).
- Follow-ups: the measurement on a low-end phone 📞; `android.minWebViewVersion` when the Android app loads the interface; search in a conversation and `aria-setsize` (E2.4).

## Sources

- virtua 0.53.3: `lib/react/Virtualizer.d.ts` (`shift`, `bufferSize`, `onResize`), https://github.com/inokawa/virtua (2026-10-10).
- react-virtuoso 4.18.16: https://virtuoso.dev/ (`firstItemIndex`, `followOutput`), 2026-10-10.
- @tanstack/react-virtual 3.14.14: https://tanstack.com/virtual/latest (2026-10-10).
- Chrome DevTools Protocol, `Emulation.setCPUThrottlingRate`: https://chromedevtools.github.io/devtools-protocol/tot/Emulation/ (2026-10-10).
- Capacitor 8.5.3, `Bridge.java` and `CapConfig.java` (`minWebViewVersion`, default 60), in `apps/mobile/node_modules/@capacitor/android` (2026-10-10).
- Vite 8.3.4 `build.target` default ("baseline-widely-available"), `dist/node/chunks/node.js`, and https://vite.dev/config/build-options (2026-10-10).
- WAI-ARIA 1.2, `log` role and live regions: https://www.w3.org/TR/wai-aria-1.2/#log (2026-10-10).
- MDN, `content-visibility`: https://developer.mozilla.org/en-US/docs/Web/CSS/content-visibility (2026-10-10).
