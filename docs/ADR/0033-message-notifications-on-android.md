# ADR 0033 — Message notifications on Android: the service decides from the core's events, without push

- **Status**: accepted
- **Date**: 2026-10-20
- **Task**: T2.17
- **Deciders**: Claude Code (design assistant), under the maintainer's standing autonomy instruction; to be reviewed by the maintainer

## Context

Until T2.17, the Android app said nothing when a message arrived while it was not on screen: the core received it (the foreground service `CoreService` keeps it running, ADR 0011), and the inbox showed it the next time the user opened the app. E2.6 asks for notifications. Its harder part is push: T1.6 found that the core's Telegram connection drops in deep Doze (ADR 0014, Battery), so an idle phone needs something that wakes the app. This ADR covers the first step, which push will build on: deciding which messages to announce, showing them, and opening the conversation the user taps, while the core is running.

What the core already gives (API 1.4, ADR 0023): `message.added` for each message, then `conversation.updated` with the conversation's `unread_count` and `marked_unread`. Reading a conversation, on any device or in the network's own app, sends a `conversation.updated` with no unread messages.

Two traps:
- After a login, the networks send history: dozens of old messages arrive as `message.added`, and their conversations are unread. Announcing them would fill the shade with old news.
- Android 13 (API 33) and later need the runtime permission `POST_NOTIFICATIONS`. The manifest declared it since T1.3, but nothing asked for it.

## Options considered

### Where to decide

#### Option A — The Android service, from the events the interface also reads (chosen)
`CoreService` hands every event to a `MessageNotifier` as it does to the interface. Pure rules (`MessageNotifications.kt`, no Android type) decide; the notifier shows.
- Pros: works while the web view is closed or paused, which is exactly when notifications matter; no change to the core or its API; the rules are tested alone.
- Cons: the rules are Kotlin; the iOS app will need its own (its Notification Service Extension will not run the same code anyway, `CLAUDE.md` §3).

#### Option B — The interface posts notifications
- Pros: one implementation in TypeScript.
- Cons: Android pauses and may destroy the web view in the background; notifications would stop exactly when needed.

#### Option C — The core decides and sends a `notification` event
- Pros: shared by every shell, desktop included.
- Cons: the core would have to know whether the app is on screen, which only the shell knows; a new API for a decision that is mostly presentation. It can still come later, when the desktop wants notifications and the rules have settled.

### Which messages

The rule chosen (in `MessageNotifications`): a message is announced when **all** of these hold:
1. it is from someone else (`from_me` false);
2. its conversation's next `conversation.updated` says it is unread (`unread_count > 0` or `marked_unread`): a message the user read elsewhere in the meantime is not announced;
3. its time is at or after its account's **watermark**: the moment the service first saw the account (an `account.updated` event), kept in the app's preferences between runs. The history sent after a login is older than that, so it is not announced;
4. it is less than **24 hours** old: after days offline, the inbox says enough;
5. the app's activity is not on screen (`onResume` to `onPause`): the inbox shows it already.

One notification per conversation (tag: the conversation ID), in `MessagingStyle` with its last 5 lines. It goes away when its conversation is read again, wherever that happens. Messages without text (a kind this version cannot show) say "New message".

## Decision

**Option A**, with the rule above.

- `core/MessageNotifications.kt`: the rules; `core/MessageNotifier.kt`: the "Messages" channel (high importance, separate from the low-importance "core" channel of the service's own notification), the notifications, and the watermarks in `SharedPreferences`.
- **Privacy on the lock screen**: the notifications are `VISIBILITY_PRIVATE`, with a public version that says "Musubee / New message", no sender and no text. The user's system settings can hide more, never show more. Nothing logs a message's content (`CLAUDE.md` §6.8).
- **Opening**: a tap starts `MainActivity` with the conversation's ID; the plugin passes it to the interface as an `openConversation` event that Capacitor keeps until a listener takes it (a tap can start the app), and the interface opens the conversation as a tap in the inbox does. A recreated activity, or one opened from the recent apps, does not open it again.
- **Permission**: the plugin's `askToNotify()` asks for `POST_NOTIFICATIONS` (Android 13 and later; earlier versions need nothing). The interface calls it once per launch, the first time it sees an account: a notification means nothing before. A refusal is the user's answer; Android itself stops showing the question after it was refused twice.
- **Interface**: `Shell.notifications` (`ask`, `onOpen`) exists only in the mobile shell; the desktop has no notifications yet.

## Consequences

- **No push yet.** When Android kills the process, or Doze cuts the network connection (ADR 0014), messages are not announced until the app runs and reconnects. The next E2.6 step adds push (FCM or UnifiedPush) to wake the service; these rules stay, fed by the same events.
- The watermark is per account and per install: a message that arrives while the core is stopped and is less than 24 hours old is announced when the core receives it, which is what the user wants after a restart, but also means a burst after a long time offline (up to one notification per conversation, never more).
- On screen, no notification at all, even for another conversation than the one shown: the inbox's unread counts tell.
- iOS will need its own rules in its extension, and the desktop its own notifications (Option C can then move the rules into the core).
- The rules know the API's event names and fields by hand (org.json), as the instrumented tests do; a renamed field breaks `NotificationTest` on a device, not the build.

## Verification

| Claim | Status | Evidence |
|---|---|---|
| The rules: a new message is announced once its conversation is unread; history before the watermark, messages older than a day, own messages and messages while the app is on screen are not; the last 5 lines are kept; reading removes the notification; "New message" without text | **verified** on a Pixel 8 Pro (Android 17), 2026-10-20 | `MessageNotificationsTest` (7 tests) |
| Through the real service and core: an echo shows a notification in the "Messages" channel with its text, private on the lock screen with a public version without the text, and `conversations.markRead` removes it | **verified** on a Pixel 8 Pro (Android 17), 2026-10-20 | `NotificationTest` |
| The mobile shell passes the user's answer on, and opens tapped notifications, the one that started the app included; the interface asks once, after the first account | **verified** (Windows, Vitest) | `ui/src/capacitor.test.ts`, `ui/src/notifications.test.ts` |
| The system's permission question appears after the first account, and a tap on a notification opens its conversation in the app | **unknown**: the phone was locked with a code during the session; to check by hand | |
| Behaviour on Android 7 to 12 (no runtime permission) | **assumed** from Android's documentation; CI's emulators run the rules' and service's tests on API 30 and 35 | |
