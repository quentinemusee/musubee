# ADR 0063 — A conversation's or a person's own color: the app's accents, per thread, kept per device

- **Status**: accepted
- **Date**: 2026-11-03
- **Task**: T2.61
- **Deciders**: Claude Code (design assistant), under the maintainer's standing autonomy instruction; to be reviewed by the maintainer

## Context

E2.7 promises themes per app, per conversation and per person. ADR 0040 built the app's: data attributes on the root element select design tokens, and five accents are measured for contrast in both themes.

## Options considered

### What a thread can change

#### Option A — The accent only, among the app's five (chosen)
The pairs are already measured (ADR 0040: white text on the light half, the dark background on the dark half, at least 4.5:1). The thread's own bubbles and buttons take its accent. The rest of the app keeps the app's.

#### Option B — Free colors or the whole theme per thread
- Cons: unmeasured contrasts; a light thread inside a dark app.

### Whose color

A person's, when the conversation belongs to one (ADR 0017), so that all their conversations and their merged thread (ADR 0042) share it; otherwise the conversation's.

### Where it is kept

The page's storage, per device, by opaque IDs ("c." and the conversation's ID, "p." and the person's), as ADR 0040 and 0042 do. It holds nothing private.

## Decision

**Option A**:
- `ui/src/conversation-accent.ts` holds the keys, parsing (unknown keys and colors are dropped), loading and saving;
- the store keeps `accents` and `setAccent`;
- the thread's section carries `data-accent`, which redefines the accent tokens for its subtree; the theme then picks the light or dark half, as at the root;
- "Color…" in the thread's header offers "Same as the app" and the five accents.

## Consequences

- Not synced between devices, like the appearance.
- The inbox's entries keep the app's accent.

## Verification

| Claim | Status | Evidence |
|---|---|---|
| Keys, parsing, saving, and a storage that throws | **implemented, unverified** | `ui/src/conversation-accent.test.ts` |
| The desktop app keeps a conversation's color after a restart, and only for it | **implemented, unverified** | `apps/desktop/e2e/app.spec.ts`, "a conversation keeps its own color" |
| The accents' contrasts | **verified** (ADR 0040's figures, the same pairs) | |
