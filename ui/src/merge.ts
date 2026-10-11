// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

// Merging conversations into a person from the interface (E2.5,
// docs/ADR/0024): which conversations to offer, and which persons.* calls
// a merge takes (docs/ADR/0017 for the model).

import type { Account, Conversation, Network, Person } from "./core-api/types.gen";

/** A conversation the user can merge with the one they read. */
export interface MergeCandidate {
  conversation: Conversation;
  /** Its name looks like the name of the conversation (or person) read: offered first. */
  likely: boolean;
}

/** How many candidates the panel lists at once: the search narrows them. */
export const MAX_CANDIDATES = 30;

/**
 * Lower case, without accents, punctuation or extra spaces: "Åsa (Echo)" and
 * "asa echo" are the same.
 */
export function normalizeName(name: string): string {
  return name
    .normalize("NFKD")
    .replace(/\p{M}/gu, "")
    .toLowerCase()
    .replace(/[^\p{L}\p{N}]+/gu, " ")
    .trim();
}

/**
 * Whether two names look like the same person's: every word of the shorter
 * name is a word of the longer one ("Alice" and "Alice T.", not "Al" and
 * "Alice").
 */
export function similarNames(a: string, b: string): boolean {
  const wordsA = normalizeName(a).split(" ").filter(Boolean);
  const wordsB = normalizeName(b).split(" ").filter(Boolean);
  if (wordsA.length === 0 || wordsB.length === 0) {
    return false;
  }
  const [shorter, longer] = wordsA.length <= wordsB.length ? [wordsA, wordsB] : [wordsB, wordsA];
  const words = new Set(longer);
  return shorter.every((word) => words.has(word));
}

/**
 * The conversations that can be merged with `current`: the other direct
 * conversations read so far, not already with the same person, whose name
 * holds every word of `query`. Those whose name looks like `current`'s (or
 * its person's) come first; otherwise the order of `conversations` (the most
 * recently active first) is kept. At most MAX_CANDIDATES.
 */
export function mergeCandidates(
  conversations: readonly Conversation[],
  persons: readonly Person[],
  current: Conversation,
  query: string,
): MergeCandidate[] {
  if (current.kind !== "direct") {
    return [];
  }
  const person = current.person_id === undefined ? undefined : persons.find((p) => p.person_id === current.person_id);
  const names = [current.name, ...(person ? [person.name] : [])];
  const words = normalizeName(query).split(" ").filter(Boolean);
  const likely: MergeCandidate[] = [];
  const others: MergeCandidate[] = [];
  for (const conversation of conversations) {
    if (
      conversation.conversation_id === current.conversation_id ||
      conversation.kind !== "direct" ||
      (current.person_id !== undefined && conversation.person_id === current.person_id)
    ) {
      continue;
    }
    const name = normalizeName(conversation.name);
    if (!words.every((word) => name.includes(word))) {
      continue;
    }
    if (names.some((n) => similarNames(n, conversation.name))) {
      likely.push({ conversation, likely: true });
    } else {
      others.push({ conversation, likely: false });
    }
  }
  return [...likely, ...others].slice(0, MAX_CANDIDATES);
}

/** The persons.* call that merges two conversations. */
export type MergePlan =
  | { command: "persons.create"; conversation_ids: string[] }
  | { command: "persons.link"; person_id: string; conversation_ids: string[] };

/**
 * How to merge `other` into the thread of `current`: into `current`'s
 * person if it has one, bringing along every conversation of `other`'s
 * person; otherwise into `other`'s person; otherwise into a new person
 * named after `current`. A conversation belongs to one person at most, so
 * linking moves it, and the core deletes a person left without any.
 */
export function mergePlan(current: Conversation, other: Conversation, persons: readonly Person[]): MergePlan {
  const personOf = (c: Conversation) => (c.person_id === undefined ? undefined : persons.find((p) => p.person_id === c.person_id));
  const target = personOf(current);
  const source = personOf(other);
  if (target !== undefined) {
    const moving = source !== undefined && source.person_id !== target.person_id ? source.conversation_ids : [other.conversation_id];
    return {
      command: "persons.link",
      person_id: target.person_id,
      conversation_ids: moving.filter((id) => !target.conversation_ids.includes(id)),
    };
  }
  if (source !== undefined) {
    return { command: "persons.link", person_id: source.person_id, conversation_ids: [current.conversation_id] };
  }
  return { command: "persons.create", conversation_ids: [current.conversation_id, other.conversation_id] };
}

/** "Telegram: Alice's phone", or the network's name alone when the account has none. */
export function accountLabel(conversation: Conversation, networks: readonly Network[], accounts: readonly Account[]): string {
  const network = networks.find((n) => n.network_id === conversation.network_id)?.name ?? conversation.network_id;
  const account = accounts.find((a) => a.account_id === conversation.account_id)?.name;
  return account ? `${network}: ${account}` : network;
}
