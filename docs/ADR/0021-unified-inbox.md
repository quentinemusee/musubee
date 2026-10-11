# ADR 0021 — The unified inbox: conversations by activity in pages, merged per person in the interface, and the interface in the Android app

- **Status**: accepted
- **Date**: 2026-10-11
- **Task**: T2.5 (epic E2.3)
- **Deciders**: Claude Code (design assistant), under the maintainer's standing autonomy instruction; to be reviewed by the maintainer

## Context

T2.5 asks for the conversation list of every account, merged per person (T2.1, ADR 0017), with pagination and offline display, tested with Playwright on the desktop and Appium on Android.

Before T2.5:

- `conversations.list` (API 1.2) returned every conversation of every account, in the order of their creation, without their last message. The interface showed them as a plain list of buttons.
- Persons existed in the core and the store (`Conversation.person_id`, `persons.*`), but nothing showed them.
- The Android app did not load the interface: its `www/` was the test page of T1.3 (ADR 0011), and the interface does not run on WebView 83, the version of the API 30 emulator images (ADR 0020, "Old WebViews").
- The echo test network had three direct conversations per login and no group, so the core's refusal to link a group to a person was not tested (TASKS.md, T2.1 outcome), and nothing could show a long inbox or an account without network.

Requirements:

1. Every account's conversations in one list, the most recently active first, each with a preview of its last message.
2. The conversations linked to one person shown as **one** entry, with a way to switch between them in the thread.
3. Thousands of conversations must not be read at once nor rendered at once.
4. An account without network still shows its conversations and says it has no network; sending fails visibly.
5. The same interface on the desktop and in the Android app, usable on a phone screen, with Android's back gesture.
6. No Matrix type nor identifier reaches the interface (`CLAUDE.md` §2).

## Options considered

### Where conversations are merged per person

- **A. In the core**: `conversations.list` returns inbox entries (a person or a conversation). The core knows the links, but an entry is not a conversation: every event (`message.added`, `conversation.updated`, `person.*`) would need an entry form too, and pagination would have to count entries, not conversations. A second list model in the API, for one view.
- **B. In the interface** (chosen): the core keeps returning conversations, each with its `person_id`, ordered by activity; a pure function of the interface (`inboxEntries`, `ui/src/inbox.ts`) folds a person's conversations into the entry of the most recently active one. The API stays a list of domain objects; the merging is a view, testable without a core.

The cost of B: a person whose conversations are on different pages shows up once their first conversation is read, and gains the others as pages arrive. The order stays right, since a person's entry is placed by its most recent conversation, which is always read first.

### Pagination

- **Offset and limit**: simple, but a message arriving between two pages moves a conversation to the top and shifts every offset: one conversation would be read twice and another skipped.
- **A cursor on the activity** (chosen): `before` is an opaque cursor naming the last conversation returned (its activity and room); the next page holds the conversations strictly less recently active, ties broken by room ID. A conversation that becomes active moves above the cursor and is not read again in the later pages; the interface learns of it through `message.added`.

### Rendering a long inbox

The thread is virtualized with virtua since T2.4 (ADR 0020, with its measurements). The inbox uses the same library, without a second comparison: the rows are of almost fixed height, a simpler case than the thread's.

### The interface in the Android app

- **Keep the test page** and build the interface for an older engine (`chrome83`, plus the few functions it lacks, as `ui/bench/legacy.ts` does for the benchmark): every Android phone would run it, but the build target would be tied to the oldest emulator image, and the polyfills would be shipped to everyone.
- **Ship the interface as built** (chosen) and require a recent system web view: Capacitor's `android.minWebViewVersion` (default 60) makes the bridge load `server.errorPath` instead of the app when the web view is older. The System WebView updates from the Play Store on Android 7 and later.

## Decision

### Core API 1.3

- `Conversation.activity_ms`: when the last message was sent, or when the conversation was created if it has none. `Conversation.last_message`: that message, the one its thread shows last. Both optional, so that 1.2 interfaces still read 1.3 responses.
- `conversations.list` orders by activity (descending, then room ID descending), takes `limit` (1 to 200) and `before`, and returns `before` while more conversations may follow. Without `limit` it still returns them all.
- A `message.added` event always makes its message the conversation's last one (the last one stored), so the interface keeps its list in order without asking the core again.
- The local Matrix storage (`core/localmatrix`) ranks the rooms in one query, `COALESCE(latest message timestamp, created_at)`, with a new index on events by room and type (schema v2, compatible with v1) so that finding a room's latest message does not scan its other events.
- The cursor is opaque like every API identifier (`k.` and base64, `core/embedded/ids.go`); a malformed one is `invalid_params`.

### The interface

- **Store** (`ui/src/store.ts`): reads conversations in pages of 50 (`INBOX_PAGE`); `inbox` is `{cursor}`, `"loading"` or `"end"`; `loadMoreConversations()` reads the next page. A reload reads as many conversations as the store held. A new message moves its conversation first (`placeConversation`); a message in a conversation the store has not read yet makes it read the first page again.
- **Inbox** (`ui/src/app/Inbox.tsx`): `inboxEntries` merges per person; the list is a virtua `Virtualizer` rendered as `ul`/`li`, which reads the next page when the user is within two viewports of the end (or when the list does not fill the viewport). A row is a button **named by the entry's name alone** (`aria-labelledby`) and **described by its networks and preview** (`aria-describedby`), so that two conversations with the same last message are not confused; each `li` carries `aria-setsize` and `aria-posinset`, since only the rows near the viewport are in the DOM. The preview says "You: " for the user's messages and the sender's name in groups; the time is today's hour or the date, updated every minute.
- **Thread** (`ui/src/app/Thread.tsx`): a person's thread is headed by their name, with a **switcher** (a `fieldset` of buttons "Network: account" with `aria-pressed`) when two or more of their conversations are loaded.
- **Narrow screens** (`ui/src/app/navigation.ts`, `styles.css`, at 720 px and below, so a phone and a narrow desktop window): the inbox and the thread take the whole width in turn. Opening a conversation pushes a history entry; the history's Back (the `popstate` event) returns to the inbox, and so does a back button in the thread header. On Android, `MainActivity` hands the system's back action to the web view while it can go back, then lets Android close the app.
- **Mobile shell** (`ui/src/capacitor.ts`): when Capacitor's native platform is present, the interface builds the same `Shell` as the desktop's preload over the `MusubeeCore` plugin: requests and events are JSON, the core is "starting" until it answers a first `core.hello`, then "ready" or "failed" (it lives in the app's process and does not restart on its own). The module is a separate 8 KB chunk, loaded only in the app. `@capacitor/core` 8.5.3 is MIT (its dependency tslib is 0BSD).

### The Android app

- `apps/mobile/www/` is now generated: `npm run web` (`copy-ui.mjs`) copies `ui/dist` and `apps/mobile/web/` into it; `npm run sync` does that and `cap sync android`. Git ignores it; the test page of T1.3 is gone.
- `capacitor.config.json`: `android.minWebViewVersion` **111**, matching the interface's build target (Chrome 111, ADR 0020), and `server.errorPath` `webview-too-old.html`, a static page (no script, `default-src 'none'`) that asks the user to update Android System WebView.
- Capacitor 8.5.3 injects its bridge with `addDocumentStartJavaScript`, not with an inline script, so the interface's Content Security Policy needs no change.

### Test support in the echo network

The echo network (a test network inside the core) gains:

- a **group** conversation, "Echo Group", where a member named Instant Echo answers: groups appear in the inbox, and linking a group to a person is refused (tested). Its members are other remote users than the direct contacts of the same names: in mautrix-go v0.31.0, `Ghost.UpdateInfo` writes the ghost's fields before taking its lock, and a login's direct and group conversations are created at once, so a ghost shared by both raced (found by CI's race detector, reproduced locally). A real network (Telegram: a contact in a direct conversation and a group) can meet the same race. The locking of `Ghost.UpdateInfo` is fixed upstream after v0.31.0 (mautrix/go#571, found in T1.1); after the upgrade, the group may share its members with the direct conversations again;
- **"crowd" logins** (usernames starting with "crowd"): 120 more contacts with one message each, for pagination;
- **"offline" logins** (usernames starting with "offline"): connected right after the login only; afterwards the account reports `transient_disconnect` with "No network: the echo network simulates a device without connection."

### A fix in the store found by these tests

A reload reads the state, then replaces the store's. An event received meanwhile was overwritten by the older state: an account that went offline right after start stayed "Connecting…". The store now applies again the events received during a reload on top of what it read (they are idempotent). It also keeps messages only for the conversations read or being read, so that a message received in a conversation never shown no longer passes for its whole history.

## Tests

- **Core** (`core/embedded`, `core/localmatrix`, `core/bridgehost`): ordering, pages and cursors, the last message, the refusal to link a group, the crowd and offline logins.
- **Interface** (`npm test` in `ui`, 153 tests): `inboxEntries`, `preview`, `activityLabel`, the store's pages, reload replay and kept histories, the mobile shell over a scripted plugin, the 1.3 contract.
- **Desktop, Playwright** (`apps/desktop/e2e/app.spec.ts`, the real app and core): the order and previews of the inbox (direct and group); two accounts' conversations merged into one person, with the switcher and sending through the second account; a crowd account read page by page as the user scrolls (124 entries, fewer than 60 rows in the DOM); an offline account that keeps its conversations after a restart, says it has no network and fails to send; a narrow window that shows the inbox or the thread, where Back and the back button return to the inbox.
- **Android, Appium** (`apps/mobile/e2e/android.app.test.mjs`, the APK on an emulator): reads the device's web view version (`dumpsys webviewupdate`). From 111: adds an echo account through the interface, opens Instant Echo from the inbox, sends a message, waits for its echo, goes back with Android's back action and finds the echo previewed in the inbox. Below 111: checks the update page instead. CI runs both: API 30 (WebView 83) and API 35.

## State of knowledge

| Claim | Status | Source |
|---|---|---|
| The inbox orders, previews, merges, pages and shows an offline account in the desktop app | **verified** (Windows; 3 repeats of each new test) | `apps/desktop/e2e/app.spec.ts` |
| The narrow layout and Back work in the desktop app | **verified** (Windows) | same, "a narrow window…" |
| The Android app shows the update page on WebView 83 | **verified** on the local API 30 emulator | `android.app.test.mjs` |
| The Android app runs the whole journey (login, inbox, thread, echo, back) | **verified** on the API 30 emulator with an exploration build for WebView 83 (a `chrome83` build and `minWebViewVersion` lowered for the run); with the real build on WebView 111 or later: to be **verified** by CI's API 35 job | exploration run, `.github/workflows/checks.yml` |
| Capacitor loads `errorPath` below `minWebViewVersion` and injects its bridge without an inline script | **verified** in its sources and on the emulator | Capacitor 8.5.3 `Bridge.java`, `CapConfig.java` |
| The interface's benchmark on Android still loads under the update page's threshold (it navigates to `bench/index.html` itself) | **verified** on the API 30 emulator | `ui/README.md`, `bench/run.mjs --target android` |
| Screen readers announce the inbox rows with their position and description | **assumed** from the accessibility tree (UiAutomator2 sees the name as text and the description as hint); not tested with TalkBack or NVDA | WAI-ARIA 1.2 |
| A person whose conversations span pages gains them as pages load | **verified** by unit tests of `inboxEntries`; not exercised end to end | `ui/src/inbox.test.ts` |

## Consequences

- An Android phone needs Android System WebView 111 or later (released in March 2023); one without the Play Store may keep an older one and sees the update page. The API 30 emulator images run only the update-page test; the journey runs on API 35.
- Building the Android app now needs the interface built first (`cd ui && npm ci && npm run build`, then `npm run sync` in `apps/mobile`); CI does it in the Android jobs of `checks.yml` and `telegram.yml`.
- The test page's Telegram login is gone: Telegram on a phone goes through the interface's "Add an account" (the QR code flow shows its data as text until the interface draws codes, E2.2).
- Merging stays a view of the interface: a second interface (the web app of a later phase) reuses `inboxEntries` with the rest of `ui/`.
- Follow-ups: unread counts and mentions (the core does not track read state yet, E2.3/E2.4); search in the inbox; muting and archiving; drawing QR codes (E2.2); TalkBack and NVDA checks of the inbox and thread; iOS (E3.3), where WKWebView 16.4 is the matching floor.

## Sources

- Capacitor 8.5.3, `android/capacitor/src/main/java/com/getcapacitor/Bridge.java` (`isMinimumWebViewInstalled`, `getErrorUrl`, `addDocumentStartJavaScript`) and `CapConfig.java` (`minWebViewVersion`, default 60), in `apps/mobile/node_modules/@capacitor/android` (2026-10-10).
- `@capacitor/core` 8.5.3, `registerPlugin` and `Capacitor.isNativePlatform`, in `ui/node_modules/@capacitor/core` (2026-10-10).
- virtua 0.53.3, `Virtualizer` props `as` and `item` (`CustomItemComponentProps`), `lib/react/Virtualizer.d.ts` (2026-10-10).
- Android, `OnBackPressedDispatcher`: https://developer.android.com/guide/navigation/custom-back (2026-10-10).
- WAI-ARIA 1.2, `aria-setsize`, `aria-posinset`, `aria-describedby`: https://www.w3.org/TR/wai-aria-1.2/ (2026-10-10).
- Chrome 111 release (March 2023): https://developer.chrome.com/blog/new-in-chrome-111 (2026-10-10).
