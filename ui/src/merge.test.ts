// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

import { describe, expect, test } from "vitest";
import type { Conversation, Person } from "./core-api/types.gen";
import { accountLabel, MAX_CANDIDATES, mergeCandidates, mergePlan, normalizeName, similarNames } from "./merge";

function conversation(id: string, name: string, patch: Partial<Conversation> = {}): Conversation {
  return { conversation_id: id, account_id: "a1", network_id: "echo", name, kind: "direct", ...patch };
}

describe("names", () => {
  test("ignore case, accents, punctuation and extra spaces", () => {
    expect(normalizeName("  Åsa   (Echo) ")).toBe("asa echo");
    expect(normalizeName("ZO\u{CB}-Anne")).toBe("zoe anne");
    expect(normalizeName("")).toBe("");
  });

  test("are similar when the shorter one's words are all in the longer one", () => {
    expect(similarNames("Alice", "Alice T.")).toBe(true);
    expect(similarNames("alice (Echo)", "ALICE")).toBe(true);
    expect(similarNames("Alice Martin", "Martin, Alice")).toBe(true);
    expect(similarNames("Al", "Alice")).toBe(false);
    expect(similarNames("Alice", "Bob")).toBe(false);
    expect(similarNames("", "Alice")).toBe(false);
    expect(similarNames("!!", "Alice")).toBe(false);
  });
});

describe("mergeCandidates", () => {
  const alice = conversation("c1", "Alice", { person_id: "h1" });
  const persons: Person[] = [{ person_id: "h1", name: "Alice Martin", conversation_ids: ["c1", "c2"] }];
  const conversations = [
    conversation("c0", "Bob"),
    alice,
    conversation("c2", "Alice (Telegram)", { person_id: "h1" }),
    conversation("c3", "Family", { kind: "group" }),
    conversation("c4", "Bobby"),
    conversation("c5", "Martin", { network_id: "telegram" }),
    conversation("c6", "Ålice", { person_id: "h2" }),
  ];

  test("offers the other direct conversations, likely ones first", () => {
    const candidates = mergeCandidates(conversations, persons, alice, "");
    // Not itself, not the person's own, not a group; "Martin" is like the
    // person's name, "Ålice" like the conversation's, though it has a person.
    expect(candidates.map((c) => `${c.conversation.conversation_id}:${c.likely}`)).toEqual(["c5:true", "c6:true", "c0:false", "c4:false"]);
  });

  test("keeps those whose name holds every word searched", () => {
    expect(mergeCandidates(conversations, persons, alice, "bo").map((c) => c.conversation.conversation_id)).toEqual(["c0", "c4"]);
    expect(mergeCandidates(conversations, persons, alice, "  BOBBY ").map((c) => c.conversation.conversation_id)).toEqual(["c4"]);
    expect(mergeCandidates(conversations, persons, alice, "bob martin")).toEqual([]);
  });

  test("offers nothing for a group, and at most MAX_CANDIDATES", () => {
    expect(mergeCandidates(conversations, persons, conversations[3]!, "")).toEqual([]);
    const many = Array.from({ length: MAX_CANDIDATES + 5 }, (_, i) => conversation(`x${i}`, `Contact ${i}`));
    const lone = conversation("me", "Someone");
    expect(mergeCandidates([lone, ...many], [], lone, "")).toHaveLength(MAX_CANDIDATES);
  });
});

describe("mergePlan", () => {
  const persons: Person[] = [
    { person_id: "h1", name: "Alice", conversation_ids: ["c1", "c2"] },
    { person_id: "h2", name: "Alice T.", conversation_ids: ["c3", "c4"] },
  ];

  test("creates a person for two conversations of no one", () => {
    expect(mergePlan(conversation("c5", "A"), conversation("c6", "B"), persons)).toEqual({ command: "persons.create", conversation_ids: ["c5", "c6"] });
  });

  test("links into the person of the conversation read", () => {
    expect(mergePlan(conversation("c1", "A", { person_id: "h1" }), conversation("c6", "B"), persons)).toEqual({
      command: "persons.link",
      person_id: "h1",
      conversation_ids: ["c6"],
    });
  });

  test("links into the other conversation's person when the one read has none", () => {
    expect(mergePlan(conversation("c6", "B"), conversation("c3", "A", { person_id: "h2" }), persons)).toEqual({
      command: "persons.link",
      person_id: "h2",
      conversation_ids: ["c6"],
    });
  });

  test("brings every conversation of the other person along", () => {
    expect(mergePlan(conversation("c1", "A", { person_id: "h1" }), conversation("c4", "B", { person_id: "h2" }), persons)).toEqual({
      command: "persons.link",
      person_id: "h1",
      conversation_ids: ["c3", "c4"],
    });
  });

  test("treats a person not read yet as none", () => {
    expect(mergePlan(conversation("c7", "A", { person_id: "h9" }), conversation("c8", "B"), persons)).toEqual({
      command: "persons.create",
      conversation_ids: ["c7", "c8"],
    });
  });
});

test("accountLabel names the network, and the account when it has a name", () => {
  const networks = [{ network_id: "telegram", name: "Telegram", login_flows: [] }];
  const accounts = [{ account_id: "a1", network_id: "telegram", name: "+33 6", state: "connected" as const }];
  expect(accountLabel(conversation("c1", "A", { network_id: "telegram" }), networks, accounts)).toBe("Telegram: +33 6");
  expect(accountLabel(conversation("c1", "A", { network_id: "telegram", account_id: "a2" }), networks, accounts)).toBe("Telegram");
  expect(accountLabel(conversation("c1", "A", { network_id: "signal", account_id: "a3" }), networks, accounts)).toBe("signal");
});
