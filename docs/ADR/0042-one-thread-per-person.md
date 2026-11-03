# ADR 0042 — One thread per person: the interface interleaves a person's conversations, and never skips a message

- **Status**: accepted
- **Date**: 2026-11-03
- **Task**: T2.26
- **Deciders**: Claude Code (design assistant), under the maintainer's standing autonomy instruction; to be reviewed by the maintainer

## Context

The product vision (`CLAUDE.md` §1) lets users "merge conversations with the same person coming from several networks into a single thread". T2.1 (ADR 0017) links conversations to persons in the core; T2.5 (ADR 0021) gives a person one entry in the inbox and a switcher between their conversations; T2.8 (ADR 0024) merges from the thread. The thread itself still shows one conversation at a time.

Two questions:

1. Where are the conversations interleaved: in the core (a `persons.messages` command) or in the interface?
2. How does a thread made of several histories, each read in pages, stay correct while the user scrolls up?

## Options considered

### Where to interleave

#### Option A — In the interface, from each conversation's pages (chosen)
The store already reads, caches and updates each conversation's messages (events, edits, reactions, history from the network, ADR 0041). The person's thread is a view over them.
- Pros: no new command; every change to a conversation shows in the person's thread at once; the API stays per conversation, which the actions, read state and sending need anyway.
- Cons: the paging rule below lives in the interface.

#### Option B — A core command that pages through a person's messages
- Cons: a second history cursor per person, events to map to it, and the same rule in Go; nothing the interface could not do.

### Paging several histories

#### Option C — Show only what cannot be preceded by unread history (chosen)
While a conversation has older messages not read yet (on the device or on the network), the others' messages older than its oldest read one are hidden; scrolling up reads that conversation next (the one whose oldest read message is the newest). A conversation with history but nothing read yet is read first, and hides nothing. The thread so grows only at the top, as one conversation's does, and the virtualized list keeps the reader's place (ADR 0020).

#### Option D — Show everything read, sorted
- Cons: a page read later can put messages in the middle of what the user has seen: the list jumps and messages appear between others.

## Decision

**Options A and C.**

- `ui/src/together.ts`: `togetherView` interleaves by time (then by the conversations' order, then their own), applies the rule above, and says which conversation to read next; `togetherSources` takes them from the store's state.
- Store: `together` (the person whose thread is shown), `showTogether()` (reads every conversation of the person not read yet), `showSeparately()`; `loadOlder()` reads the conversation the rule names; selecting another person's conversation leaves the person's thread.
- Thread:
  - "All together" (a toggle, `aria-pressed`) above the switcher; in a person's thread, the switcher's buttons choose the conversation the user writes in ("Write to … with", for screen readers), and the composer says "Write with Telegram…";
  - each message says the network it came through ("via Telegram"; the account too when two of the person's conversations are on one network);
  - each message offers what its own network allows (`ConversationActions` per conversation);
  - answering or editing a message moves the writing to its conversation;
  - every conversation shown is marked read, under the same conditions as one conversation (ADR 0023).
- The privacy note (ADR 0039) is the conversation the user writes in.

## Consequences

- Since T2.31, the choice is remembered per device: the persons read together are kept in the page's storage (opaque person IDs only, like the appearance, ADR 0040), and selecting one of their conversations opens their thread together again; "All together" pressed off forgets it.
- Groups are not merged (ADR 0024); a person's thread holds direct conversations only.
- Sending in a person's thread goes to the conversation chosen, never to "all": a message goes once, through one network.
- A person with many conversations reads more pages to reach the same date.

## Verification

| Claim | Status | Evidence |
|---|---|---|
| Interleaving, the paging rule, the conversation read next, waiting while one is read | **implemented, unverified** | `ui/src/together.test.ts` |
| The store shows a person's conversations together, reads further back where the thread stops, and leaves the person's thread for another person's conversation | **implemented, unverified** | `ui/src/store.test.ts` |
| The desktop app shows two merged conversations in one thread, says where each message came through, writes with the chosen network, answers through the message's own, and separates them again | **implemented, unverified** | `apps/desktop/e2e/app.spec.ts`, "a person's conversations read as one thread" |
