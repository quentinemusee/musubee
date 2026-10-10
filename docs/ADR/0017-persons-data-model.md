# ADR 0017 — Persons: merged conversations keyed by network identity, stored next to the bridges

- **Status**: accepted
- **Date**: 2026-10-10
- **Task**: T2.1
- **Deciders**: Claude Code (design assistant), under the maintainer's standing autonomy instruction; to be reviewed by the maintainer

## Context

Merging is a core promise of the product (`CLAUDE.md` §1 and §2): the conversations with one person, coming from several networks or accounts, are shown as one thread. `CLAUDE.md` §2 already decided where it lives: a local link table `Person ↔ [Conversation]` in the domain layer, not in the bridges, stored on the device and later synced across the user's devices in encrypted form. Open question 5 of phase 1 (`CLAUDE.md` §4) asked for the data model and its sync. Phase 1 ended without a spike on it (ADR 0016), so it is decided here, with the first implementation.

Constraints:

- **No Matrix type in the API** (`CLAUDE.md` §2). The core's conversation IDs are opaque, but they encode the local Matrix room ID (ADR 0012), an identifier that exists only on one device.
- **Several devices.** The same person must be the same on the user's phone and computer, once the links sync. A link must therefore name a conversation by something every device knows.
- **Accounts come and go.** An account can be logged out and added again, or added on one device only. A link must not be lost because its conversation is absent for a while.
- **Two connection modes** (`CLAUDE.md` §2): on device, bridgev2 runs in the core; with a hosted bridge, the core is a Matrix client (T2.2) and the bridge's portal key is not directly at hand. The model must work for both.

## Options considered

### What a link names

#### Option A: the local conversation (room) ID
- Pros: trivial; what the API already exposes.
- Cons: different on every device, so nothing to sync; lost when an account is logged out and its rooms are deleted.

#### Option B: the conversation's identity on its network (bridge ID, network chat ID, receiving account)
This is bridgev2's portal key, plus the bridge: `(network_id, chat_id, receiver)`.
- Pros: the same on every device that has the account. Example: for a Telegram direct chat, the chat ID is built from the other user's Telegram ID and the receiver is the account's own Telegram user ID (`ids.InternalMakePortalKey`, `ids.MakeUserLoginID`). Also stable when an account is logged out and added again, so a dormant link wakes up by itself.
- Cons: the core must map key ↔ local conversation on every read. With a hosted bridge (T2.2), the key has to come from the room's bridge info (the `m.bridge` / `uk.half-shot.bridge` state event names the network and the channel), which is not built yet.

#### Option C: the remote contact (the other user's network ID, or a phone number)
- Pros: allows automatic merging ("same phone number on Signal and WhatsApp").
- Cons: a person is the user's own grouping, not a network fact: two contacts with the same name are often different people, and one person has different IDs on every network, so automatic merging needs a suggestion step anyway. Contacts also do not identify group conversations, which a later version may want to attach to a person.

### Where the links live

#### Option D: in the core's SQLite database, in tables of our own
- Pros: one file, one transaction model, the core's existing upgrade mechanism (dbutil upgrade tables, like `localmatrix`), backed up with the rest.
- Cons: none found; the tables must not collide with bridgev2's, hence the `musubee_` prefix.

#### Option E: in the interface (local storage)
- Pros: none that matters.
- Cons: every interface would reimplement it, and the core could not sync it. Rejected: merging is a domain feature (`CLAUDE.md` §2).

#### Option F: in Matrix account data
- Pros: Matrix would sync it for free.
- Cons: on device there is no homeserver (ADR 0009); in hosted mode the homeserver operator would see the user's social graph unless it is encrypted. Kept as a possible *transport* for the future encrypted sync, not as the store.

## Decision

**B + D.** A person is a user-made grouping of conversations. The core stores, in its database:

- `musubee_person(person_id, name, created_at, updated_at)`: person IDs are `h.` followed by 128 random bits in hex, so that devices can create persons independently without collisions.
- `musubee_person_link(network_id, chat_id, receiver, person_id, linked_at)`, keyed by the conversation's network identity. A conversation belongs to **at most one** person: linking it elsewhere moves it. A person left without any link is deleted.

The core maps keys to the API's opaque conversation IDs when it answers. A link whose conversation is not on this device stays **dormant**: it is not shown, and it comes back with its conversation (the account added again, or added on another device once sync exists). A person whose links are all dormant is not listed.

**Only direct conversations can be linked** for now (`invalid_params` otherwise). Attaching a group to a person is a different feature (a "related group"), left to E2.5.

**API 1.2** (minor, backward compatible): `Conversation.person_id` (optional); `Person {person_id, name, conversation_ids}`; commands `persons.list`, `persons.create` (name defaults to the first conversation's), `persons.rename`, `persons.link`, `persons.unlink`, `persons.delete`; events `person.updated`, `person.deleted`, and `conversation.updated` for each conversation whose person changed. A person with no conversation left on this device is reported as `person.deleted` to the interface, though its dormant links stay stored. The commands are serialized in the core so that the events of two changes never interleave.

**Sync across devices (not built; constraints recorded now).** The tables were shaped for it:

1. Rows are keyed by values every device agrees on (random person IDs, network identities), so a sync is a merge of rows, not a translation.
2. Merge rule: last writer wins, per row: a link by `linked_at`, a person's name by `updated_at`. Timestamps are milliseconds of the device's clock; clock skew between the user's own devices is accepted for this kind of data (an error only reverts a rename or a link, never a message).
3. **Deletions need tombstones**, which are not stored today (deleting a person or unlinking deletes the row). They will be added with the sync, as a schema upgrade: a deleted link must win over an older link from another device.
4. The sync payload will be end-to-end encrypted between the user's devices (`CLAUDE.md` §1); its transport (Matrix account data in an encrypted room, or our own relay) is decided with the sync, after T2.2 and T2.3.

## State of knowledge

| Claim | Status | Source |
|---|---|---|
| bridgev2 identifies a conversation by its portal key (portal ID + receiver) and the bridge by its ID | **verified** (read in the code) | `bridgev2/networkid`, `Bridge.GetExistingPortalByKey`, mautrix-go v0.31.0, 2026-10-10 |
| A Telegram direct chat's key is the other user's ID and the account's Telegram user ID: the same on every device | **verified** (read in the code) | `pkg/connector/ids/ids.go` (`InternalMakePortalKey`, `MakeUserLoginID`), mautrix-telegram v0.2609.0, 2026-10-10 |
| Links survive a restart of the core, and a logout followed by a new login of the same account | **verified** (run) | `core/embedded/persons_test.go` (`TestPersonsOutliveRoomsAndRestarts`), echo network |
| Linking, moving, unlinking, renaming and deleting persons behave as specified, with the events | **verified** (run) | `core/persons/persons_test.go`, `core/embedded/persons_test.go` (`TestPersonsJourney`), both SQLite drivers |
| No Matrix identifier reaches the API in the echo journeys | **verified** (run) | every response and event of `core/embedded` tests goes through `apitest.MatrixIDs` |
| No Matrix identifier reaches the API in the Telegram journey | **assumed** until the CI run of this change | `infra/telegramtest/ondevice` now applies the same check |
| A group conversation is refused by `persons.create` and `persons.link` | **assumed** (code written, not run: the echo network has only direct conversations) | to test in T2.5, when the echo network gets a group for the inbox |
| The local room ID of a portal is the same after a logout and a new login | **verified** (run), and not relied upon | `localmatrix` derives it from the portal key (`GenerateDeterministicRoomID`) |
| A hosted bridge's room carries enough to rebuild the key | **unknown** | T2.2 (the core as a Matrix client) |
| Last-writer-wins with device clocks is acceptable to users | **assumed** | to revisit with the sync |

## Consequences

- The interface gets everything it needs for the merged inbox (T2.5): persons, their conversations, and each conversation's person.
- No automatic merging. Suggestions (same name, same phone number in the contact's identifiers) can come later on top of this model, as suggestions the user accepts.
- In hosted-bridge mode (T2.2 and later), the core must derive the same key from the room's bridge info; if a network's hosted bridge cannot provide it, links in that mode fall back to being local only. To check in T2.2.
- The interface must read the persons again after `accounts.logout`, as it already reads the conversations again: the core sends no event for a person that loses its last visible conversation through a logout. Acceptable for now; revisit if logouts happen from elsewhere (another device).
- Revisit this ADR if a network gives the same conversation different keys on different devices, or when a group must belong to a person.

## Sources

- mautrix-go v0.31.0, `bridgev2/portal.go`, `bridgev2/networkid` (read 2026-10-10).
- mautrix-telegram v0.2609.0, `pkg/connector/ids/ids.go` (read 2026-10-10).
- `CLAUDE.md` §1, §2, §4 question 5; ADR 0009 (no homeserver on device), ADR 0012 (opaque IDs, versioning), ADR 0016 (phase 2 order).
