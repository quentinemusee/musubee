# ADR 0024 — Merging conversations in the interface: from the thread, with likely matches first

- **Status**: accepted
- **Date**: 2026-10-11
- **Task**: T2.8 (epic E2.5)
- **Deciders**: Claude Code (design assistant), under the maintainer's standing autonomy instruction; to be reviewed by the maintainer

## Context

Merging the conversations with one person into one thread is the product's distinctive feature (`CLAUDE.md` §1). The core has stored persons since T2.1 (ADR 0017: `persons.create`, `link`, `unlink`, `rename`, `delete`), and the inbox and thread show them since T2.5 (ADR 0021): one entry per person, a switcher between their conversations. But nothing in the interface **made** a person: only the tests did, through the API.

Requirements:

1. The user merges from where they notice the need: reading a conversation and knowing the same person writes elsewhere.
2. Finding the other conversation is quick among hundreds: likely matches (the same name on another network) come first, and a search narrows the rest.
3. Every merge can be undone: a conversation taken out, or all of them.
4. No Matrix notion reaches the interface (`CLAUDE.md` §2): it speaks of conversations, networks and persons.

## Options considered

- **Automatic merging by name**: wrong too often (two "Alex"), and silently mixing two people's messages is worse than not merging. Names only **suggest**.
- **A separate "people" screen** listing persons and their conversations: useful later for bulk management, but far from the moment of need.
- **From the thread** (chosen): a "Same person…" button in a direct conversation's header (or "Person…" once merged) opens a panel. It lists the other direct conversations, likely ones first, with a search; "Merge" links one. The same panel renames the person and separates conversations.

## Decision

- `ui/src/merge.ts`, pure and unit-tested:
  - `mergeCandidates`: the direct conversations read so far, except the conversation itself and those already with its person, whose name holds every word searched (ignoring case, accents and punctuation). Those whose name looks like the conversation's or its person's name come first ("likely": every word of the shorter name is in the longer one, so "Alice" matches "Alice T." but "Al" does not match "Alice"). At most 30 are listed; the search narrows them.
  - `mergePlan`: into the thread's person if it has one, bringing **all** of the other conversation's person along (two persons become one); otherwise into the other conversation's person; otherwise a new person named after the conversation read. Groups are not offered (ADR 0017 links direct conversations only).
- `Store.mergeWith`, `renamePerson`, `separateConversation`, `unmergePerson` call the core and apply the person it answers with at once; the core's events (`person.updated`, `person.deleted`, `conversation.updated`) bring the rest. Errors go to the error banner.
- `PersonPanel`: the panel, closed after a merge (the switcher then shows the merged thread). "Separate all…" asks for a confirmation and says that the messages stay.
- Only the conversations read so far are searched: the panel offers "Search more conversations", which reads the next page of the inbox. A search in the core (E2.4's conversation search) will replace it.

## State of knowledge

| Claim | Status | Source |
|---|---|---|
| Candidates, likely matches, the search, and each merge plan (new person, link into either person, two persons into one) | **verified** (run) | `ui/src/merge.test.ts`, Vitest, 2026-10-11 |
| The store's calls, its immediate update, and its errors | **verified** (run) | `ui/src/store.test.ts` "Store's persons", same run |
| In the desktop app, with the real core: merge two accounts' conversations from the thread, rename the person, separate them all | **verified** (run) | `apps/desktop/e2e/app.spec.ts` "merge two conversations from the thread…", Electron on Windows, 2026-10-11 |
| The panel on Android (narrow screen, touch) | **assumed**: same interface, narrow layout covered by CSS; not run on the emulator in this task | — |
| Whether names suggest the right person often enough to be useful with real contacts | **unknown**: to watch with the first users | — |

## Consequences

- The feature is usable end to end on the desktop; persons stay per device until their sync (ADR 0017).
- A separate people screen, merging groups ("related groups"), and suggestions from phone numbers or usernames (when networks give them) remain for later in E2.5.

## Sources

- ADR 0017 (persons data model), ADR 0021 (inbox and switcher).
