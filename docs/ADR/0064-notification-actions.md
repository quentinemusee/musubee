# ADR 0064 — Notification actions on Android: reply and mark read through the core service, after unlocking

- **Status**: accepted
- **Date**: 2026-11-03
- **Task**: T2.64
- **Deciders**: Claude Code (design assistant), under the maintainer's standing autonomy instruction; to be reviewed by the maintainer

## Context

Android notifications of new messages (ADR 0033) open the conversation when tapped. Every messenger also lets the user answer from the notification, or dismiss it as read, without opening the app.

Sources: Android's developer guide, "Create a notification" ("Add a direct reply action", `RemoteInput`, `PendingIntent.FLAG_MUTABLE` for a reply's intent on Android 12 and later); AndroidX Core 1.17, `NotificationCompat.Action.Builder` (`addRemoteInput`, `setSemanticAction`, `setAuthenticationRequired`, added in 1.9.0).

## Options considered

### Who handles the action

#### Option A — CoreService, through an intent with an action (chosen)
The service owns the core (ADR 0011) and is running whenever a notification exists. The action's `PendingIntent` starts it with `app.musubee.REPLY` or `app.musubee.MARK_READ` and the conversation's ID. `onStartCommand` calls the core on a thread of its own: `messages.send` with the typed text, or `conversations.markRead`.

#### Option B — A BroadcastReceiver
- Cons: it would need to reach the service anyway, and a receiver has seconds to finish.

### When the notification goes

When the core says the conversation is read, as for a conversation read elsewhere (ADR 0033). The user's own message makes a conversation read (ADR 0023), so a reply clears it too. If the call fails, the notification is removed by hand: Android shows a spinner on a reply until the notification changes.

### Locked devices

Both actions require the device to be unlocked (`setAuthenticationRequired`). Otherwise anyone holding the phone could reply in the user's name. The lock screen's notification shows no content anyway (ADR 0033).

## Decision

**Option A**, with the two actions on every message notification, after unlocking. The typed text and the commands' parameters are never logged.

## Consequences

- The reply goes to the conversation of the notification, never to "all" of a person's conversations (ADR 0042).
- A reply sent while the account is offline waits like any message (ADR 0043).

## Verification

| Claim | Status | Evidence |
|---|---|---|
| The Kotlin compiles | **unknown** (no Android SDK in the cloud session) | `./gradlew :app:assembleDebug` |
| Reply and Mark as read work from a notification, and ask to unlock first | **unknown** (to try by hand on the Pixel 8 Pro) | |
