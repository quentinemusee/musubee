// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

// A person's conversations shown as one thread (docs/ADR/0042): their
// messages, from every network, interleaved by time.

import type { Message } from "./core-api/types.gen";
import type { HistoryState, State } from "./store";

/** One conversation of the person, as the store has read it. */
export interface TogetherSource {
  conversationId: string;
  /** Read so far, oldest first; undefined while not read yet. */
  messages: readonly Message[] | undefined;
  history: HistoryState | undefined;
}

/** The thread of a person, and how to read further back. */
export interface TogetherView {
  /** Oldest first. */
  messages: readonly Message[];
  /**
   * The conversation to read older messages of next: the one whose oldest
   * read message is the newest among those with more history. Undefined
   * when every conversation is read to its start, or one is being read.
   */
  next: string | undefined;
  /** As the thread's MessageList takes it. */
  history: HistoryState;
}

function hasMore(history: HistoryState | undefined): boolean {
  return history !== undefined && history !== "start" && history !== "loading";
}

/**
 * Interleaves a person's conversations. A conversation that has older
 * messages not read yet hides every other one's messages older than its
 * oldest read one: the thread never skips messages that it would show
 * later, so it only grows at the top, as one conversation's does.
 */
export function togetherView(sources: readonly TogetherSource[]): TogetherView {
  let cutoff = Number.NEGATIVE_INFINITY;
  let next: string | undefined;
  // A conversation with more history but nothing read: where its messages
  // fall is unknown, so it is read first, and hides nothing meanwhile.
  let empty: string | undefined;
  let loading = false;
  for (const source of sources) {
    if (source.messages === undefined || source.history === "loading") {
      loading = true;
      continue;
    }
    if (!hasMore(source.history)) {
      continue;
    }
    const oldest = source.messages[0]?.timestamp_ms;
    if (oldest === undefined) {
      empty ??= source.conversationId;
    } else if (oldest > cutoff) {
      cutoff = oldest;
      next = source.conversationId;
    }
  }
  next = empty ?? next;
  const shown: { message: Message; source: number; index: number }[] = [];
  sources.forEach((source, sourceIndex) => {
    source.messages?.forEach((message, index) => {
      if (message.timestamp_ms >= cutoff) {
        shown.push({ message, source: sourceIndex, index });
      }
    });
  });
  // By time; at the same time, in the order of the sources, then their own.
  shown.sort((a, b) => a.message.timestamp_ms - b.message.timestamp_ms || a.source - b.source || a.index - b.index);
  let history: HistoryState = "start";
  if (loading) {
    history = "loading";
  } else if (next !== undefined) {
    history = { cursor: next };
  }
  return { messages: shown.map((s) => s.message), next: loading ? undefined : next, history };
}

/** The conversations of a person as the store has them, in the order of the conversation list. */
export function togetherSources(state: State, personId: string): TogetherSource[] {
  return state.conversations
    .filter((c) => c.person_id === personId)
    .map((c) => ({ conversationId: c.conversation_id, messages: state.messages[c.conversation_id], history: state.history[c.conversation_id] }));
}
