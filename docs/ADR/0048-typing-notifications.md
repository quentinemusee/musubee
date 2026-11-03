# ADR 0048 — Typing notifications: in memory, both ways, renewed by the interface

- **Status**: accepted
- **Date**: 2026-11-03
- **Task**: T2.37
- **Deciders**: Claude Code (design assistant), under the maintainer's standing autonomy instruction; to be reviewed by the maintainer

## Context

The local server ignored typing (`Intent.MarkTyping` did nothing, waiting for "ephemeral updates" in the API, T1.4), and the user's typing never reached a network. Every messaging app shows "… is typing".

Sources: mautrix-go v0.31.0 `bridgev2/portal.go` (`handleMatrixTyping`: hands only changes of who types to `TypingHandlingNetworkAPI.HandleMatrixTyping`; a ghost's message stops its typing with `MarkTyping(…, 0)`), `bridgev2/queue.go` (`QueueMatrixEvent` takes an ephemeral event without a sender), `bridgev2/simplevent.Typing`.

## Decision

- **Local server** (`core/localmatrix/typing.go`): who types, per room, until when (at most 30 s without a renewal), in memory only; a change publishes `Update.Typing`, and so does an expiry. The device's user typing in another app of the network is not shown. `SendTyping` queues a Matrix typing event (`m.typing`) for the room's bridge; since bridgev2 hands only changes, a renewal while typing is sent as a stop, then a start.
- **API 1.20**: the event `conversation.typing` (`conversation_id`, `names`: display names, `""` for someone unnamed, none when nobody types) and the command `conversations.typing` (`typing`).
- **Echo network**: ContactInstant types back for three seconds while the user types.
- **Interface**: the composer says "typing" when the text becomes non-empty and again every five seconds while it changes, and "stopped" when it becomes empty, when a message is sent and when the conversation is left; a line above the composer says "Alice is typing…", "Alice and Bob are typing…", "Someone is typing…" or "Several people are typing…", read out politely.

## Consequences

- Networks that show typing get it as long as the user types; a stop then start each five seconds may show as a flicker on some networks (to check by hand on Telegram).
- Typing is never stored: after a restart nobody types.
- Typing is sent to the network the conversation is on, including from a person's thread (ADR 0042), where the composer writes.

## Verification

| Claim | Status | Evidence |
|---|---|---|
| The echo contact types back, announced by name, then stops by itself | **implemented, unverified** | `core/embedded/typing_test.go` `TestTyping` |
| The wording | **implemented, unverified** | `ui/src/message.test.ts` |
| The desktop app shows the contact typing, then not | **implemented, unverified** | `apps/desktop/e2e/app.spec.ts`, "typing shows the echo contact typing back" |
| Telegram, Signal and Matrix show the user typing | **assumed** (their connectors implement `TypingHandlingNetworkAPI`) | |
