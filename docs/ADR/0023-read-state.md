# ADR 0023 — Read state: a read position per conversation in the core, unread counts in the API, read when seen

- **Status**: accepted
- **Date**: 2026-10-11
- **Task**: T2.7 (epic E2.3)
- **Deciders**: Claude Code (design assistant), under the maintainer's standing autonomy instruction; to be reviewed by the maintainer

## Context

T2.5 (ADR 0021) built the unified inbox and left "unread counts (the core has no read state yet)". A messenger without them makes the user open every conversation to find what is new.

Before T2.7:

- The local Matrix side of the core (`core/localmatrix`, ADR 0009) stored read receipts as room account data (`m.read`-like, per user), but only the receipts the **bridges** wrote through an intent: those of other people (ghosts), and those of the device's user when a network reported that they read a conversation in another app (double puppeting). Nothing computed a count, and nothing recorded what the user read **in Musubee**.
- The API (1.3) had no notion of read state; the interface showed every conversation alike.
- bridgev2 already knows how to pass a read receipt to a network: a Matrix `m.receipt` ephemeral event for a portal room goes to `Portal.handleMatrixReceipts`, which updates the user's `UserPortal.LastRead` and calls the connector's `HandleMatrixReadReceipt` when it implements `ReadReceiptHandlingNetworkAPI` (mautrix-telegram does; read in mautrix-go v0.31.0, `bridgev2/portal.go`, and mautrix-telegram v0.2609.0, `pkg/connector/handlematrix.go`, 2026-10-11).
- In the other direction, mautrix-telegram reports the user's own reads as `RemoteEventReadReceipt` events from the user (on `updateReadHistoryInbox`, and from each dialog's `read_inbox_max_id` when it syncs the chats); bridgev2 applies them through the user's double puppet intent, which our `localmatrix` connector provides for the device's user (`NewUserIntent`, ADR 0009).

Requirements:

1. Each conversation carries an unread count, cheap to compute for a page of the inbox, and bounded so that a conversation with years of unread history does not cost more than any other.
2. "Read" must mean read: reading on the phone's official app (where the network reports it) or in Musubee clears the count; writing in a conversation does too.
3. Reading in Musubee tells the network, when the network supports it, so that the official apps and the other person's "read" ticks agree.
4. A conversation can be "marked as unread" (Matrix's `m.marked_unread`, MSC2867), which reading clears.
5. No Matrix type nor identifier reaches the interface (`CLAUDE.md` §2).
6. The interface marks a conversation read only when the user can see its newest message: not when the window is hidden, nor when they scrolled up into the history.

## Options considered

### Where the read position lives

- **A. Room account data only**, as before: the user's receipt is an event ID and a time. Counting then means finding that event's position on every count, and a receipt that names an event the core never stored (a network's message ID mapped to nothing, or a time only) has no position at all.
- **B. A read marker table** (chosen): `local_read_marker(room_id, stream_order, updated_at)`, one row per room, holding the **position** (the event's `stream_order` in `local_event`) up to which the device's user has read. A receipt is resolved to a position once, when it arrives: the event's own position if it is stored, otherwise the last event of the room sent at or before the receipt's time. The marker only moves forward, so a late or replayed receipt never makes messages unread again.

The receipts of other people stay in room account data as before: they are not the user's read state.

### What counts as unread

- **Every event after the marker**: state events (names, members) and the user's own messages would count.
- **Messages of others after the read position** (chosen): `m.room.message` events not sent by the device's user, after the later of the marker and the user's own last message. Writing in a conversation implies having read it, including from another device of the network (the user's messages carry the user's ID through double puppeting). The count stops at `MaxUnread` (1000): a `LIMIT` inside the `COUNT`, so the cost is bounded per conversation; the interface shows "99+" from 100 anyway.

### Who tells the network

- **The core's API calls the connector directly**: it would bypass bridgev2's own handling (`UserPortal.LastRead`, the message lookup from event ID to network message ID) and duplicate it.
- **The local Matrix side queues an `m.receipt` ephemeral event** (chosen), exactly what a Matrix homeserver would send to an appservice: bridgev2 does the rest, and every connector that handles receipts (Telegram today, Signal later) gets them for free. A connector that does not is left unaware, and bridgev2 logs it; the read state in the core is already stored.

### When the interface marks a conversation read

- **On opening**: simple, but a conversation opened in a background window, or scrolled up to read old messages while new ones arrive, would be marked read unseen.
- **When the newest message is in view in a visible window** (chosen): the thread reports when its list reaches or leaves the bottom (`MessageList`'s `onBottomChange`; the list opens at the bottom), and `document.visibilityState` tells whether the window can be seen. The store clears the count at once (optimistic), then calls the core; a failure shows an error, and the next `conversation.updated` brings the core's truth back.

## Decision

### Core API 1.4

- `Conversation.unread_count` (0 to 1000, omitted when 0) and `Conversation.marked_unread` (omitted when false). Both optional: 1.3 interfaces still read 1.4 responses.
- Command `conversations.markRead {conversation_id, message_id?}` → `{}`: the user read the conversation up to that message, by default its last one. A message of another conversation, or an unknown one, is `not_found`. A conversation with no message only loses its "marked unread" flag.
- `message.added` is now followed by `conversation.updated` for the same conversation (its last message and unread count changed), and any change of read state (read here, read on another device, marked unread) sends `conversation.updated`.

### Core

- Schema v3 of the local Matrix storage (`02-read-marker.sql`, compatible with v1 and later): the `local_read_marker` table, deleted with its room.
- `Server.MarkRead` stores the marker, clears the "marked unread" flag, publishes a `ReadState` update, then queues the receipt to the room's bridge. `Intent.MarkRead` and `Intent.MarkUnread` for the device's user (double puppet) move the marker or set the flag and publish the same update; for anyone else they keep the account data as before.
- The echo test network handles read receipts (it accepts them), so that bridgev2's path to a connector is exercised in the core's tests.

### Interface

- `InboxEntry.unread` and `markedUnread`: a person's entry sums the counts of their conversations and is marked if any is (`ui/src/inbox.ts`).
- The row shows a badge (the count, "99+" above 99, or a dot when only marked unread) next to the preview, with the preview and time in a stronger color. The badge's text is hidden from assistive technologies; a visually hidden "N unread messages" (or "Marked as unread") describes the row's button, before the network and the preview (`aria-describedby`).
- `Store.markRead` (optimistic), called by the thread when it shows a conversation with unread messages or marked unread, its newest message in view, in a visible window.

Not decided here, and not built: marking a conversation unread from the interface, and passing it to networks (mautrix-telegram does not bridge Telegram's "mark as unread"; read in `pkg/connector`, 2026-10-11); read receipts for native Matrix accounts (ADR 0018's connector does not implement `ReadReceiptHandlingNetworkAPI`, and does not report the user's reads from their other Matrix clients); showing other people's read receipts ("seen") in the thread (E2.4); syncing the read state between Musubee devices (it follows the networks for now).

## State of knowledge

| Claim | Status | Source |
|---|---|---|
| The unread count follows the marker, the user's own messages and ghosts' receipts; the marker only moves forward; an unknown event is `ErrNotFound`; a receipt for an event the core does not have is placed by its time; "marked unread" is set and cleared; updates are published | **verified** (run) | `core/localmatrix/localmatrix_test.go` `TestReadState`, Windows, Go 1.27.1, 2026-10-11 |
| A v2 database (T2.5) upgrades to v3 and keeps its rooms | **verified** (run) | `TestUpgradeFromV2`, same run |
| Through the API: an echo answer makes the count 1, `conversations.markRead` makes it 0 and sends `conversation.updated`, bridgev2 records the receipt in `UserPortal.LastRead` (so the connector was called), a wrong conversation or message ID is `not_found`, an empty conversation is accepted | **verified** (run) | `core/embedded/read_test.go`, same run; `go test -race ./core/...` all green with MSYS2 UCRT64 |
| The interface's sums, badges, labels and optimistic `markRead` (including a failed call) | **verified** (run) | `ui/src/inbox.test.ts`, `ui/src/store.test.ts`, Vitest, 2026-10-11 |
| In the desktop app: an answer in a conversation that is not open shows "1 unread message" in the row's description; opening it clears the badge and the core's count | **verified** (run) | `apps/desktop/e2e/app.spec.ts`, "the inbox shows … unread count", Electron on Windows, 2026-10-11 |
| Reading in Musubee marks the conversation read on Telegram (`HandleMatrixReadReceipt` → `messages.readHistory`) | **assumed**: bridgev2's path is verified with echo; Telegram's handler is read in the code, not run | mautrix-telegram v0.2609.0 `pkg/connector/handlematrix.go:943`, 2026-10-11 |
| Reading on the phone's Telegram app clears the count in Musubee, and the counts after a first sync match Telegram's | **assumed**: mautrix-telegram queues own `RemoteEventReadReceipt`s (`handletelegram.go` `onOwnReadReceipt`, `chatsync.go`), bridgev2 applies them with the double puppet (`User.DoublePuppet` calls our `NewUserIntent`), and `Intent.MarkRead` of the device user is tested; the Telegram journey does not check it | mautrix-go v0.31.0 `bridgev2/portal.go` `handleRemoteReadReceipt`, `bridgev2/user.go`, 2026-10-11 |
| The cost of the counts on a page of 50 to 200 conversations (one bounded `COUNT` and one account data read per conversation) is acceptable on a low-end phone | **unknown**: not measured; to watch with the inbox benchmark when the low-end phone is available (T2.4) | — |
| Screen readers announce "N unread messages" with the row | **unknown**: the accessible description is checked by Playwright; NVDA and TalkBack were not run (as in T2.5) | — |

## Consequences

- Every connector that handles read receipts gets them without code in the core; Signal (T2.6) will too.
- One `conversation.updated` per received message doubles the events of a busy conversation; it is the price of keeping the interface's list right without asking the core again. Batching is possible later if measurements call for it.
- The read position is per device: two Musubee devices agree only through the networks. An encrypted sync of the read state belongs with the persons' sync (ADR 0017).
- Revisit if counting shows up in measurements (a counter column updated on each message is the alternative), or if Matrix-native accounts need the full receipt model (threads, private receipts).

## Sources

- mautrix-go v0.31.0, `bridgev2/portal.go` (`handleMatrixReceipts`, `handleRemoteReadReceipt`, `GetIntentFor`), `bridgev2/user.go` (`DoublePuppet`), `bridgev2/networkinterface.go` (`ReadReceiptHandlingNetworkAPI`), read 2026-10-11.
- mautrix-telegram v0.2609.0, `pkg/connector/handlematrix.go`, `handletelegram.go`, `chatsync.go`, read 2026-10-11.
- MSC2867 (marked unread), as implemented by mautrix-go's `event.MarkedUnreadEventContent`, read 2026-10-11.
