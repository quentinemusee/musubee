// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

// The interface's state, fed by the core's responses and events.
//
// Provisional: state management is still to be chosen by an ADR with
// measurements on low-end Android (CLAUDE.md §4, question 6). This store is
// the smallest thing that works with React's useSyncExternalStore: one
// immutable snapshot, replaced on every change.

import { CoreClient, parseEvent } from "./core-api/client";
import type { Account, Conversation, Event, Message, Network } from "./core-api/types.gen";
import type { CoreStatus, ShellCore } from "./shell";

export interface State {
  status: CoreStatus;
  /** Set once the state was read from the core since it last started. */
  loaded: boolean;
  /** A problem to show to the user, in their words. */
  error?: string;
  coreVersion?: string;
  networks: readonly Network[];
  accounts: readonly Account[];
  /** Sorted by name. */
  conversations: readonly Conversation[];
  /** Messages read or received so far, by conversation ID, oldest first. */
  messages: Readonly<Record<string, readonly Message[]>>;
  selected?: string;
}

const initialState: State = {
  status: { state: "starting" },
  loaded: false,
  networks: [],
  accounts: [],
  conversations: [],
  messages: {},
};

export class Store {
  readonly client: CoreClient;
  readonly #core: ShellCore;
  #state = initialState;
  readonly #listeners = new Set<() => void>();
  /** Increases on each reload, so that a stale reload does not overwrite a newer one. */
  #generation = 0;

  constructor(core: ShellCore) {
    this.#core = core;
    this.client = new CoreClient({ call: (request) => core.call(request) });
  }

  /** Listens to the shell; returns a function that stops listening. */
  start(): () => void {
    const stopEvents = this.#core.onEvent((json) => this.#onEvent(json));
    const stopStatus = this.#core.onStatus((status) => {
      this.#set({ status });
      if (status.state === "ready") {
        void this.reload();
      }
    });
    return () => {
      stopEvents();
      stopStatus();
    };
  }

  readonly subscribe = (listener: () => void): (() => void) => {
    this.#listeners.add(listener);
    return () => this.#listeners.delete(listener);
  };

  readonly getState = (): State => this.#state;

  /**
   * Reads the whole state from the core again: after it started, after it
   * restarted, or when it says events were dropped.
   */
  async reload(): Promise<void> {
    const generation = ++this.#generation;
    try {
      const hello = await this.client.hello();
      const [networks, accounts, conversations] = await Promise.all([
        this.client.call("networks.list"),
        this.client.call("accounts.list"),
        this.client.call("conversations.list"),
      ]);
      const selected = this.#state.selected;
      const keep = selected !== undefined && conversations.conversations.some((c) => c.conversation_id === selected);
      const latest = keep ? await this.client.call("messages.list", { conversation_id: selected }) : undefined;
      if (generation !== this.#generation) {
        return;
      }
      const { error: _, ...rest } = this.#state;
      this.#replace({
        ...rest,
        loaded: true,
        coreVersion: hello.core_version,
        networks: networks.networks,
        accounts: accounts.accounts,
        conversations: sortConversations(conversations.conversations),
        // Only the shown conversation is read again; the others will be when
        // shown. Messages received while reading are kept.
        messages: keep && latest ? { [selected]: mergeMessages(this.#state.messages[selected] ?? [], latest.messages) } : {},
        ...(keep ? { selected } : {}),
      });
    } catch (error) {
      if (generation === this.#generation) {
        this.#set({ error: `Could not read your conversations: ${describe(error)}` });
      }
    }
  }

  /** Shows a conversation, reading its latest messages the first time. */
  async select(conversationId: string): Promise<void> {
    this.#set({ selected: conversationId });
    if (this.#state.messages[conversationId] !== undefined) {
      return;
    }
    try {
      const result = await this.client.call("messages.list", { conversation_id: conversationId });
      // Events may have brought messages in the meantime.
      this.#setMessages(conversationId, mergeMessages(result.messages, this.#state.messages[conversationId] ?? []));
    } catch (error) {
      this.#set({ error: `Could not read the messages: ${describe(error)}` });
    }
  }

  /** Sends a text message to the selected conversation. */
  async send(text: string): Promise<void> {
    const conversationId = this.#state.selected;
    if (conversationId === undefined) {
      return;
    }
    try {
      const { message } = await this.client.call("messages.send", { conversation_id: conversationId, text });
      this.#upsertMessage(message);
    } catch (error) {
      this.#set({ error: `Could not send the message: ${describe(error)}` });
    }
  }

  dismissError(): void {
    const { error: _, ...rest } = this.#state;
    this.#replace(rest);
  }

  #onEvent(json: string): void {
    let event: Event | null;
    try {
      event = parseEvent(json);
    } catch {
      return;
    }
    switch (event?.type) {
      case "account.updated":
        this.#set({ accounts: upsert(this.#state.accounts, event.data.account, (a) => a.account_id) });
        break;
      case "conversation.updated":
        this.#set({
          conversations: sortConversations(upsert(this.#state.conversations, event.data.conversation, (c) => c.conversation_id)),
        });
        break;
      case "message.added":
      case "message.updated":
        this.#upsertMessage(event.data.message);
        break;
      case "resync.required":
        void this.reload();
        break;
      default:
        // core.closed: the shell reports the status; unknown types are ignored.
        break;
    }
  }

  #upsertMessage(message: Message): void {
    const id = message.conversation_id;
    this.#setMessages(id, mergeMessages(this.#state.messages[id] ?? [], [message]));
  }

  #setMessages(conversationId: string, messages: readonly Message[]): void {
    this.#set({ messages: { ...this.#state.messages, [conversationId]: messages } });
  }

  #set(patch: Partial<State>): void {
    this.#replace({ ...this.#state, ...patch });
  }

  #replace(state: State): void {
    this.#state = state;
    for (const listener of this.#listeners) {
      listener();
    }
  }
}

function upsert<T>(items: readonly T[], item: T, key: (item: T) => string): T[] {
  const index = items.findIndex((other) => key(other) === key(item));
  return index < 0 ? [...items, item] : items.with(index, item);
}

function sortConversations(conversations: readonly Conversation[]): Conversation[] {
  return conversations.toSorted((a, b) => a.name.localeCompare(b.name));
}

/** Merges two lists of messages: the later version of a message wins; oldest first. */
export function mergeMessages(base: readonly Message[], newer: readonly Message[]): Message[] {
  const byId = new Map(base.map((m) => [m.message_id, m]));
  for (const message of newer) {
    byId.set(message.message_id, message);
  }
  // A stable sort keeps the core's order between messages of the same millisecond.
  return [...byId.values()].toSorted((a, b) => a.timestamp_ms - b.timestamp_ms);
}

function describe(error: unknown): string {
  return error instanceof Error ? error.message : String(error);
}
