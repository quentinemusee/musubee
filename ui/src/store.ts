// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

// The interface's state, fed by the core's responses and events.
//
// Chosen in docs/ADR/0020: our own store over React's
// useSyncExternalStore, with no state library. One immutable snapshot,
// replaced on every change; components select the part they show.

import { CoreClient, parseEvent } from "./core-api/client";
import type { Account, Conversation, Event, Message, Network, Person } from "./core-api/types.gen";
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
  /**
   * The conversations read so far, the most recently active first
   * (byActivity): the first pages of the core's list, kept up to date by its
   * events.
   */
  conversations: readonly Conversation[];
  /**
   * Where the conversation list read so far stops: a cursor for the next
   * page, "loading" while it is read, or "end" when every conversation was
   * read.
   */
  inbox: InboxState;
  /** The persons the user merged conversations into, oldest first. */
  persons: readonly Person[];
  /**
   * Messages read or received so far, by conversation ID, oldest first: only
   * for the conversations whose latest messages were read (select).
   */
  messages: Readonly<Record<string, readonly Message[]>>;
  /**
   * Where the history of each conversation read so far stops: a cursor for
   * the older messages, "loading" while they are read, or "start" when the
   * first message was read.
   */
  history: Readonly<Record<string, HistoryState>>;
  selected?: string;
}

export type HistoryState = { cursor: string } | "loading" | "start";
export type InboxState = { cursor: string } | "loading" | "end";

/** How many older messages loadOlder() reads at once. */
export const OLDER_PAGE = 100;
/** How many conversations a page of the conversation list holds. */
export const INBOX_PAGE = 50;

const initialState: State = {
  status: { state: "starting" },
  loaded: false,
  networks: [],
  accounts: [],
  conversations: [],
  inbox: "end",
  persons: [],
  messages: {},
  history: {},
};

export class Store {
  readonly client: CoreClient;
  readonly #core: ShellCore;
  #state = initialState;
  readonly #listeners = new Set<() => void>();
  /** Increases on each reload, so that a stale reload does not overwrite a newer one. */
  #generation = 0;
  /**
   * The events received while a reload reads the state: applied again on top
   * of it, since it may have been read before them.
   */
  #replay: Event[] | undefined;
  /** The conversations whose latest messages select() is reading. */
  readonly #reading = new Set<string>();

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
    this.#replay = [];
    try {
      const hello = await this.client.hello();
      const [networks, accounts, conversations, persons] = await Promise.all([
        this.client.call("networks.list"),
        this.client.call("accounts.list"),
        // As many conversations as were read before, so that the list does
        // not shrink under the user's eyes.
        this.#readConversations(this.#state.conversations.length),
        this.client.call("persons.list"),
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
        conversations: conversations.conversations.toSorted(byActivity),
        inbox: inboxAfter(conversations.before),
        persons: persons.persons,
        // Only the shown conversation is read again; the others will be when
        // shown. Messages received while reading are kept.
        messages: keep && latest ? { [selected]: mergeMessages(this.#state.messages[selected] ?? [], latest.messages) } : {},
        history: keep && latest ? { [selected]: historyAfter(latest.before) } : {},
        ...(keep ? { selected } : {}),
      });
      const replay = this.#replay ?? [];
      this.#replay = undefined;
      for (const event of replay) {
        this.#apply(event);
      }
    } catch (error) {
      if (generation === this.#generation) {
        this.#replay = undefined;
        this.#set({ error: `Could not read your conversations: ${describe(error)}` });
      }
    }
  }

  /**
   * Reads at least count conversations (one page if fewer), page by page,
   * the most recent first.
   */
  async #readConversations(count: number): Promise<{ conversations: Conversation[]; before?: string }> {
    const conversations: Conversation[] = [];
    let before: string | undefined;
    do {
      const page = await this.client.call("conversations.list", { limit: INBOX_PAGE, ...(before === undefined ? {} : { before }) });
      conversations.push(...page.conversations);
      before = page.before;
    } while (before !== undefined && conversations.length < count);
    return { conversations, ...(before === undefined ? {} : { before }) };
  }

  /**
   * Reads the next page of the conversation list; does nothing while one is
   * read or when every conversation was.
   */
  async loadMoreConversations(): Promise<void> {
    const inbox = this.#state.inbox;
    if (typeof inbox === "string") {
      return;
    }
    const generation = this.#generation;
    this.#set({ inbox: "loading" });
    try {
      const page = await this.client.call("conversations.list", { limit: INBOX_PAGE, before: inbox.cursor });
      if (generation !== this.#generation) {
        return; // A reload replaced the list.
      }
      this.#set({ conversations: mergeConversations(this.#state.conversations, page.conversations), inbox: inboxAfter(page.before) });
    } catch (error) {
      if (generation === this.#generation) {
        this.#set({ inbox, error: `Could not read your conversations: ${describe(error)}` });
      }
    }
  }

  /**
   * Reads the first page of the conversation list again, for a message in a
   * conversation that was not read yet: it is now the most recent one.
   */
  async #refreshFirstPage(): Promise<void> {
    const generation = this.#generation;
    try {
      const page = await this.client.call("conversations.list", { limit: INBOX_PAGE });
      if (generation === this.#generation) {
        this.#set({ conversations: mergeConversations(this.#state.conversations, page.conversations) });
      }
    } catch (error) {
      if (generation === this.#generation) {
        this.#set({ error: `Could not read your conversations: ${describe(error)}` });
      }
    }
  }

  /** Shows a conversation, reading its latest messages the first time. */
  async select(conversationId: string): Promise<void> {
    this.#set({ selected: conversationId });
    if (this.#state.messages[conversationId] !== undefined || this.#reading.has(conversationId)) {
      return;
    }
    this.#reading.add(conversationId);
    try {
      const result = await this.client.call("messages.list", { conversation_id: conversationId });
      // Events may have brought messages in the meantime.
      this.#set({
        messages: { ...this.#state.messages, [conversationId]: mergeMessages(result.messages, this.#state.messages[conversationId] ?? []) },
        history: { ...this.#state.history, [conversationId]: historyAfter(result.before) },
      });
    } catch (error) {
      this.#set({ error: `Could not read the messages: ${describe(error)}` });
    } finally {
      this.#reading.delete(conversationId);
    }
  }

  /** Shows no conversation; their messages stay. */
  deselect(): void {
    const { selected: _, ...rest } = this.#state;
    this.#replace(rest);
  }

  /**
   * Reads the messages before the oldest one shown, in the selected
   * conversation; does nothing while they are read or when there are none.
   */
  async loadOlder(): Promise<void> {
    const conversationId = this.#state.selected;
    const history = conversationId === undefined ? undefined : this.#state.history[conversationId];
    if (conversationId === undefined || history === undefined || typeof history === "string") {
      return;
    }
    const generation = this.#generation;
    this.#set({ history: { ...this.#state.history, [conversationId]: "loading" } });
    try {
      const result = await this.client.call("messages.list", { conversation_id: conversationId, before: history.cursor, limit: OLDER_PAGE });
      if (generation !== this.#generation) {
        return; // A reload replaced the history.
      }
      this.#set({
        messages: { ...this.#state.messages, [conversationId]: mergeMessages(this.#state.messages[conversationId] ?? [], result.messages) },
        history: { ...this.#state.history, [conversationId]: historyAfter(result.before) },
      });
    } catch (error) {
      if (generation === this.#generation) {
        this.#set({ history: { ...this.#state.history, [conversationId]: history }, error: `Could not read older messages: ${describe(error)}` });
      }
    }
  }

  /**
   * Marks a conversation read up to its last message (API 1.4). The badge
   * goes at once; the core then sends the conversation again. A
   * conversation.updated event already on its way still carries the old
   * count: the thread calls this again while it is shown, and the core
   * changes nothing the second time.
   */
  async markRead(conversationId: string): Promise<void> {
    const conversations = this.#state.conversations;
    const index = conversations.findIndex((c) => c.conversation_id === conversationId);
    const conversation = conversations[index];
    if (conversation === undefined || (!conversation.unread_count && !conversation.marked_unread)) {
      return;
    }
    const { unread_count: _, marked_unread: __, ...read } = conversation;
    this.#set({ conversations: conversations.with(index, read) });
    try {
      await this.client.call("conversations.markRead", {
        conversation_id: conversationId,
        ...(conversation.last_message ? { message_id: conversation.last_message.message_id } : {}),
      });
    } catch (error) {
      this.#set({ error: `Could not mark the conversation as read: ${describe(error)}` });
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

  /**
   * Logs an account out: the core removes it, with its conversations. The
   * persons are read again too: those left without a conversation here are
   * no longer listed.
   */
  async logout(accountId: string): Promise<void> {
    try {
      await this.client.call("accounts.logout", { account_id: accountId });
    } catch (error) {
      this.#set({ error: `Could not log out: ${describe(error)}` });
    }
    await this.reload();
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
    if (event === null) {
      return;
    }
    if (event.type === "resync.required") {
      void this.reload();
      return;
    }
    this.#replay?.push(event);
    this.#apply(event);
  }

  #apply(event: Event): void {
    switch (event.type) {
      case "account.updated": {
        const { account } = event.data;
        // A logged-out account no longer exists in the core.
        this.#set({
          accounts:
            account.state === "logged_out"
              ? this.#state.accounts.filter((a) => a.account_id !== account.account_id)
              : upsert(this.#state.accounts, account, (a) => a.account_id),
        });
        break;
      }
      case "conversation.updated":
        this.#set({ conversations: mergeConversations(this.#state.conversations, [event.data.conversation]) });
        break;
      case "person.updated":
        this.#set({ persons: upsert(this.#state.persons, event.data.person, (p) => p.person_id) });
        break;
      case "person.deleted": {
        const { person_id } = event.data;
        this.#set({ persons: this.#state.persons.filter((p) => p.person_id !== person_id) });
        break;
      }
      case "message.added":
        this.#upsertMessage(event.data.message);
        this.#lastMessage(event.data.message, true);
        break;
      case "message.updated":
        this.#upsertMessage(event.data.message);
        this.#lastMessage(event.data.message, false);
        break;
      default:
        // core.closed: the shell reports the status; unknown types are ignored.
        break;
    }
  }

  /**
   * Keeps the conversation list up to date with a message: a new message is
   * its conversation's last one (API 1.3), which moves it first; an updated
   * one replaces the last message it was.
   */
  #lastMessage(message: Message, added: boolean): void {
    const conversations = this.#state.conversations;
    const index = conversations.findIndex((c) => c.conversation_id === message.conversation_id);
    const conversation = conversations[index];
    if (conversation === undefined) {
      if (added && this.#state.loaded) {
        void this.#refreshFirstPage();
      }
      return;
    }
    if (added) {
      const moved = { ...conversation, last_message: message, activity_ms: message.timestamp_ms };
      this.#set({ conversations: placeConversation(conversations.toSpliced(index, 1), moved) });
    } else if (conversation.last_message?.message_id === message.message_id) {
      this.#set({ conversations: conversations.with(index, { ...conversation, last_message: message }) });
    }
  }

  /**
   * Keeps a message, in a conversation whose latest messages were read or are
   * being read: the others will be read when shown, with their history.
   */
  #upsertMessage(message: Message): void {
    const id = message.conversation_id;
    if (this.#state.messages[id] === undefined && !this.#reading.has(id)) {
      return;
    }
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

/**
 * The order of the conversation list: the most recently active first. The
 * sort is stable, so conversations as active keep the core's order.
 */
export function byActivity(a: Conversation, b: Conversation): number {
  return (b.activity_ms ?? 0) - (a.activity_ms ?? 0);
}

/**
 * Inserts a conversation into a list in activity order, before the ones as
 * active: the common case of a new message, at the top, costs a copy.
 */
function placeConversation(conversations: readonly Conversation[], conversation: Conversation): Conversation[] {
  const index = conversations.findIndex((c) => byActivity(conversation, c) <= 0);
  return index < 0 ? [...conversations, conversation] : conversations.toSpliced(index, 0, conversation);
}

/**
 * Merges conversations read from the core into a list: a conversation
 * already listed keeps the version with the latest activity (an event may
 * have overtaken the page being read), then the list is sorted.
 */
export function mergeConversations(base: readonly Conversation[], newer: readonly Conversation[]): Conversation[] {
  const byId = new Map(base.map((c) => [c.conversation_id, c]));
  for (const conversation of newer) {
    const known = byId.get(conversation.conversation_id);
    if (known === undefined || (conversation.activity_ms ?? 0) >= (known.activity_ms ?? 0)) {
      byId.set(conversation.conversation_id, conversation);
    }
  }
  return [...byId.values()].toSorted(byActivity);
}

function inboxAfter(cursor: string | undefined): InboxState {
  return cursor === undefined ? "end" : { cursor };
}

function historyAfter(cursor: string | undefined): HistoryState {
  return cursor === undefined ? "start" : { cursor };
}

/** How far from the newest message mergeMessages looks for a message to replace. */
const RECENT = 64;

/**
 * Merges two lists of messages: the later version of a message wins; oldest
 * first. One new or updated message near the end, the common case of an
 * event, costs a copy of the list instead of a sort of it (ADR 0020).
 */
export function mergeMessages(base: readonly Message[], newer: readonly Message[]): Message[] {
  const only = newer.length === 1 ? newer[0]! : undefined;
  const last = base.at(-1);
  if (only !== undefined) {
    const from = Math.max(0, base.length - RECENT);
    for (let i = base.length - 1; i >= from; i--) {
      if (base[i]!.message_id === only.message_id) {
        // In place if its time keeps the order.
        if (base[i]!.timestamp_ms === only.timestamp_ms) {
          return base.with(i, only);
        }
        break;
      }
    }
    if (last === undefined || (only.timestamp_ms >= last.timestamp_ms && !base.some((m) => m.message_id === only.message_id))) {
      return [...base, only];
    }
  }
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
