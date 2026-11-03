// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

// The interface's state, fed by the core's responses and events.
//
// Chosen in docs/ADR/0020: our own store over React's
// useSyncExternalStore, with no state library. One immutable snapshot,
// replaced on every change; components select the part they show.

import { CoreApiError, CoreClient, parseEvent } from "./core-api/client";
import { accepts, MediaCache, sendChoice, sendProblem, uploadBlob } from "./media";
import { pageStorage } from "./appearance";
import { loadTogether, saveTogether, togetherSources, togetherView, type TogetherStorage } from "./together";
import type { Account, Conversation, Event, Message, Network, Person } from "./core-api/types.gen";
import { mergePlan } from "./merge";
import { replyTo } from "./message";
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
   * the older messages on the device, "network" once they are all read and
   * the network may have older ones (API 1.13), "loading" while they are
   * read, or "start" when the first message was read.
   */
  history: Readonly<Record<string, HistoryState>>;
  selected?: string;
  /**
   * The person whose conversations the thread shows as one (docs/ADR/0042),
   * the selected conversation being where the user writes; none: the
   * selected conversation alone.
   */
  together?: string;
  /** A search of the messages (API 1.17), while the user searches. */
  search?: SearchState;
  /** A media the user sends, while its bytes go to the core (API 1.10). */
  upload?: UploadState;
}

export interface SearchState {
  query: string;
  /** The newest first. */
  results: readonly Message[];
  /** A cursor for more results, "loading" while they are read, or "end". */
  more: { cursor: string } | "loading" | "end";
}

/** How sendMedia sends a file. */
export interface SendMediaOptions {
  /** A voice message the user recorded. */
  voice?: boolean;
  /** Its duration, when known. */
  durationMs?: number;
}

export interface UploadState {
  conversationId: string;
  name: string;
  /** Bytes given to the core so far. */
  sent: number;
  size: number;
}

export type HistoryState = { cursor: string } | "loading" | "network" | "start";
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
  /** The media of messages, read from the core (API 1.9). */
  readonly media: MediaCache;
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

  /** The persons the user reads together (docs/ADR/0042), remembered per device. */
  readonly #togetherPersons: Set<string>;
  readonly #togetherStorage: TogetherStorage | undefined;

  constructor(core: ShellCore, togetherStorage: TogetherStorage | undefined = pageStorage()) {
    this.#core = core;
    this.client = new CoreClient({ call: (request) => core.call(request) });
    this.media = new MediaCache((params) => this.client.call("media.read", params));
    this.#togetherStorage = togetherStorage;
    this.#togetherPersons = loadTogether(togetherStorage);
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
    const conversation = this.#state.conversations.find((c) => c.conversation_id === conversationId);
    if (this.#state.together !== undefined && conversation?.person_id !== this.#state.together) {
      // Another person's conversation: shown alone.
      const { together: _, ...rest } = this.#state;
      this.#replace({ ...rest, selected: conversationId });
    } else {
      this.#set({ selected: conversationId });
    }
    // A person the user reads together opens together again.
    if (this.#state.together === undefined && conversation?.person_id !== undefined && this.#togetherPersons.has(conversation.person_id)) {
      await this.#readLatest(conversationId);
      return this.showTogether();
    }
    return this.#readLatest(conversationId);
  }

  /**
   * Shows the selected conversation's person's conversations as one thread
   * (docs/ADR/0042), reading the latest messages of those not read yet.
   */
  async showTogether(): Promise<void> {
    const selected = this.#state.conversations.find((c) => c.conversation_id === this.#state.selected);
    const personId = selected?.person_id;
    if (personId === undefined) {
      return;
    }
    this.#set({ together: personId });
    this.#togetherPersons.add(personId);
    saveTogether(this.#togetherStorage, this.#togetherPersons);
    const merged = this.#state.conversations.filter((c) => c.person_id === personId);
    await Promise.all(merged.map((c) => this.#readLatest(c.conversation_id)));
  }

  /** Shows the selected conversation alone again. */
  showSeparately(): void {
    const { together, ...rest } = this.#state;
    if (together !== undefined) {
      this.#togetherPersons.delete(together);
      saveTogether(this.#togetherStorage, this.#togetherPersons);
    }
    this.#replace(rest);
  }

  /** Reads the latest messages of a conversation, once. */
  async #readLatest(conversationId: string): Promise<void> {
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
    if (this.#state.together !== undefined) {
      // The conversation that hides the most of the thread (togetherView).
      const next = togetherView(togetherSources(this.#state, this.#state.together)).next;
      return next === undefined ? undefined : this.#loadOlderOf(next);
    }
    if (this.#state.selected !== undefined) {
      return this.#loadOlderOf(this.#state.selected);
    }
  }

  async #loadOlderOf(conversationId: string): Promise<void> {
    const history = this.#state.history[conversationId];
    if (history === undefined || history === "loading" || history === "start") {
      return;
    }
    if (history === "network") {
      return this.#fetchOlder(conversationId);
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
   * Asks the network for the history before the oldest message on the
   * device (API 1.13). Once the network has no more, or gives nothing, or
   * the core cannot ask (an older core), the history starts there.
   */
  async #fetchOlder(conversationId: string): Promise<void> {
    const generation = this.#generation;
    this.#set({ history: { ...this.#state.history, [conversationId]: "loading" } });
    try {
      const result = await this.client.call("messages.fetchOlder", { conversation_id: conversationId, limit: OLDER_PAGE });
      if (generation !== this.#generation) {
        return;
      }
      let next: HistoryState = "network";
      if (result.before !== undefined) {
        next = { cursor: result.before };
      } else if (result.end === true || result.messages.length === 0) {
        next = "start";
      }
      this.#set({
        messages: { ...this.#state.messages, [conversationId]: mergeMessages(this.#state.messages[conversationId] ?? [], result.messages) },
        history: { ...this.#state.history, [conversationId]: next },
      });
    } catch (error) {
      if (generation !== this.#generation) {
        return;
      }
      if (error instanceof CoreApiError && error.code === "unknown_command") {
        this.#set({ history: { ...this.#state.history, [conversationId]: "start" } });
        return;
      }
      this.#set({ history: { ...this.#state.history, [conversationId]: "network" }, error: `Could not read older messages: ${describe(error)}` });
    }
  }

  /**
   * Marks a conversation read up to its last message (API 1.4). The badge
   * goes at once; the core then sends the conversation again. A
   * conversation.updated event already on its way still carries the old
   * count: the thread calls this again while it is shown, and the core
   * changes nothing the second time.
   */
  /** Marks a conversation as unread (API 1.18), at once in the store. */
  async markUnread(conversationId: string): Promise<void> {
    const conversations = this.#state.conversations;
    const index = conversations.findIndex((c) => c.conversation_id === conversationId);
    const conversation = conversations[index];
    if (conversation === undefined) {
      return;
    }
    this.#set({ conversations: conversations.with(index, { ...conversation, marked_unread: true }) });
    try {
      await this.client.call("conversations.markUnread", { conversation_id: conversationId });
    } catch (error) {
      this.#set({ error: `Could not mark the conversation as unread: ${describe(error)}` });
    }
  }

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

  /** Sends a text message to the selected conversation, as an answer to the message answered if given. */
  async send(text: string, answered?: string): Promise<void> {
    const conversationId = this.#state.selected;
    if (conversationId === undefined) {
      return;
    }
    try {
      const params = answered === undefined ? { conversation_id: conversationId, text } : { conversation_id: conversationId, text, reply_to: answered };
      const { message } = await this.client.call("messages.send", params);
      // The core's events about the message may have arrived first, and are
      // newer than this answer: a send that fails at once (an account
      // without network) would otherwise stay "sending".
      const known = this.#state.messages[message.conversation_id]?.some((m) => m.message_id === message.message_id);
      if (!known) {
        this.#upsertMessage(message);
      }
    } catch (error) {
      this.#set({ error: `Could not send the message: ${describe(error)}` });
    }
  }

  /**
   * Sends a file to the selected conversation (API 1.10), with a caption if
   * not empty, as an answer if given: as a photo, video or audio when the
   * network takes that kind, as a file otherwise. Resolves with whether it
   * was sent; a problem shows as the app's error.
   */
  async sendMedia(file: File, caption: string, answered?: string, options: SendMediaOptions = {}): Promise<boolean> {
    const conversationId = this.#state.selected;
    if (conversationId === undefined || this.#state.upload !== undefined) {
      return false;
    }
    const conversation = this.#state.conversations.find((c) => c.conversation_id === conversationId);
    const media = conversation?.actions?.media;
    // A recorded voice message goes as one where the network takes them,
    // as audio (or a file) otherwise.
    const choice =
      options.voice === true && media?.voice !== undefined && accepts(media.voice, file.type)
        ? { kind: "voice" as const, action: media.voice }
        : sendChoice(file.type, media);
    const problem = sendProblem(file.size, choice, caption !== "");
    if (choice === null || problem !== null) {
      this.#set({ error: problem ?? "This conversation does not take this kind of file." });
      return false;
    }
    const progress = (sent: number) => this.#set({ upload: { conversationId, name: file.name, sent, size: file.size } });
    progress(0);
    let uploadId: string | undefined;
    try {
      const size = choice.kind === "image" ? await imageSize(file) : undefined;
      uploadId = await uploadBlob((params) => this.client.call("uploads.append", params), file, progress);
      const { message } = await this.client.call("messages.sendMedia", {
        conversation_id: conversationId,
        upload_id: uploadId,
        kind: choice.kind,
        mime_type: file.type,
        file_name: file.name || "file",
        ...(caption === "" ? {} : { caption }),
        ...(size === undefined ? {} : size),
        ...(options.durationMs === undefined ? {} : { duration_ms: Math.round(options.durationMs) }),
        ...(answered === undefined ? {} : { reply_to: answered }),
      });
      uploadId = undefined;
      const known = this.#state.messages[message.conversation_id]?.some((m) => m.message_id === message.message_id);
      if (!known) {
        this.#upsertMessage(message);
      }
      return true;
    } catch (error) {
      if (uploadId !== undefined) {
        this.client.call("uploads.cancel", { upload_id: uploadId }).catch(() => undefined);
      }
      this.#set({ error: `Could not send ${file.name || "the file"}: ${describe(error)}` });
      return false;
    } finally {
      const { upload: _, ...rest } = this.#state;
      this.#replace(rest);
    }
  }

  /*
   * The user's changes to messages (API 1.8). The core changes the message
   * at once and tells it (message.updated), then answers once the network
   * did: a refusal undoes the change there, and shows here as an error.
   */

  /** Adds the user's reaction to a message. */
  async react(message: Message, key: string): Promise<void> {
    await this.#change("Could not add the reaction", () =>
      this.client.call("messages.react", { conversation_id: message.conversation_id, message_id: message.message_id, key }),
    );
  }

  /** Takes back the user's reaction with this key (as the core keyed it). */
  async unreact(message: Message, key: string): Promise<void> {
    await this.#change("Could not take the reaction back", () =>
      this.client.call("messages.unreact", { conversation_id: message.conversation_id, message_id: message.message_id, key }),
    );
  }

  /** Replaces the text of one of the user's messages. */
  async edit(message: Message, text: string): Promise<void> {
    await this.#change("Could not edit the message", () =>
      this.client.call("messages.edit", { conversation_id: message.conversation_id, message_id: message.message_id, text }),
    );
  }

  /** Deletes one of the user's messages for everyone. */
  async deleteMessage(message: Message): Promise<void> {
    await this.#change("Could not delete the message for everyone", () =>
      this.client.call("messages.delete", { conversation_id: message.conversation_id, message_id: message.message_id }),
    );
  }

  async #change(failure: string, call: () => Promise<unknown>): Promise<void> {
    try {
      await call();
    } catch (error) {
      this.#set({ error: `${failure}: ${describe(error)}` });
    }
  }

  /**
   * Logs an account out: the core removes it, with its conversations. The
   * persons are read again too: those left without a conversation here are
   * no longer listed.
   */
  /**
   * Verifies this device for a Matrix account with its recovery key (API
   * 1.15, docs/ADR/0044). Resolves with what to tell the user; the key is
   * kept nowhere.
   */
  async verifyAccount(accountId: string, recoveryKey: string): Promise<{ ok: true; backupRestored: boolean } | { ok: false; reason: string }> {
    try {
      const result = await this.client.call("accounts.verify", { account_id: accountId, recovery_key: recoveryKey });
      const accounts = this.#state.accounts;
      const index = accounts.findIndex((a) => a.account_id === accountId);
      const account = accounts[index];
      if (account !== undefined) {
        this.#set({ accounts: accounts.with(index, { ...account, verification: "verified" }) });
      }
      return { ok: true, backupRestored: result.backup_restored };
    } catch (error) {
      if (error instanceof CoreApiError && error.code === "invalid_params") {
        return { ok: false, reason: "This is not the recovery key of this account. Check it and try again." };
      }
      if (error instanceof CoreApiError && error.code === "unsupported") {
        return { ok: false, reason: "This account has no secure backup yet. Set it up in another Matrix app, such as Element, then come back." };
      }
      return { ok: false, reason: `Could not verify this device: ${describe(error)}` };
    }
  }

  async logout(accountId: string): Promise<void> {
    try {
      await this.client.call("accounts.logout", { account_id: accountId });
    } catch (error) {
      this.#set({ error: `Could not log out: ${describe(error)}` });
    }
    await this.reload();
  }

  /** Whether the shell can start over once the core's key is lost. */
  get canStartOver(): boolean {
    return this.#core.startOver !== undefined;
  }

  /**
   * Starts over once the core's key is lost (docs/ADR/0031): the shell sets
   * the unreadable data aside and starts a new, empty core. Whatever the
   * interface still shows of the old data is forgotten first. Returns
   * whether the shell did it.
   */
  async startOver(): Promise<boolean> {
    if (this.#core.startOver === undefined) {
      return false;
    }
    this.#generation++;
    this.#replay = undefined;
    this.#replace({ ...initialState, status: this.#state.status });
    this.media.clear();
    try {
      await this.#core.startOver();
      return true;
    } catch (error) {
      this.#set({ error: `Could not start over: ${describe(error)}` });
      return false;
    }
  }

  /**
   * Merges `otherId` into the thread of `currentId` (mergePlan). The core's
   * events update the persons and conversations; the person it answers with
   * is applied at once. Returns whether the merge was made.
   */
  async mergeWith(currentId: string, otherId: string): Promise<boolean> {
    const current = this.#state.conversations.find((c) => c.conversation_id === currentId);
    const other = this.#state.conversations.find((c) => c.conversation_id === otherId);
    if (current === undefined || other === undefined) {
      return false;
    }
    const plan = mergePlan(current, other, this.#state.persons);
    try {
      if (plan.command === "persons.create") {
        const { person } = await this.client.call("persons.create", { conversation_ids: plan.conversation_ids });
        this.#applyPerson(person);
      } else {
        // One link per conversation: a person's conversations follow it.
        for (const conversationId of plan.conversation_ids) {
          const { person } = await this.client.call("persons.link", { person_id: plan.person_id, conversation_id: conversationId });
          this.#applyPerson(person);
        }
      }
      return true;
    } catch (error) {
      this.#set({ error: `Could not merge the conversations: ${describe(error)}` });
      return false;
    }
  }

  /** Takes a conversation out of its person's thread. */
  async separateConversation(conversationId: string): Promise<void> {
    try {
      await this.client.call("persons.unlink", { conversation_id: conversationId });
    } catch (error) {
      this.#set({ error: `Could not separate the conversation: ${describe(error)}` });
    }
  }

  /** Renames a person; returns whether it was renamed. */
  async renamePerson(personId: string, name: string): Promise<boolean> {
    try {
      const { person } = await this.client.call("persons.rename", { person_id: personId, name });
      this.#applyPerson(person);
      return true;
    } catch (error) {
      this.#set({ error: `Could not rename: ${describe(error)}` });
      return false;
    }
  }

  /** Separates all of a person's conversations: the person is gone, its conversations stay. */
  async unmergePerson(personId: string): Promise<void> {
    try {
      await this.client.call("persons.delete", { person_id: personId });
    } catch (error) {
      this.#set({ error: `Could not separate the conversations: ${describe(error)}` });
    }
  }

  /**
   * Applies a person the core answered with, and the person ID of its
   * conversations, before the events that say the same arrive.
   */
  #applyPerson(person: Person): void {
    const ids = new Set(person.conversation_ids);
    this.#set({
      persons: upsert(this.#state.persons, person, (p) => p.person_id),
      conversations: this.#state.conversations.map((c) =>
        ids.has(c.conversation_id) && c.person_id !== person.person_id ? { ...c, person_id: person.person_id } : c,
      ),
    });
  }

  /**
   * Searches the messages on the device (API 1.17). A newer search replaces
   * an older one's results, even if the older one answers last; an empty
   * query ends the search.
   */
  async searchMessages(query: string): Promise<void> {
    const text = query.trim();
    if (text === "") {
      this.clearSearch();
      return;
    }
    this.#set({ search: { query: text, results: [], more: "loading" } });
    try {
      const result = await this.client.call("messages.search", { query: text });
      if (this.#state.search?.query !== text) {
        return;
      }
      this.#set({ search: { query: text, results: result.messages, more: result.before === undefined ? "end" : { cursor: result.before } } });
    } catch (error) {
      if (this.#state.search?.query === text) {
        this.#set({ search: { query: text, results: [], more: "end" }, error: `Could not search: ${describe(error)}` });
      }
    }
  }

  /** Reads the next page of the current search's results. */
  async searchMore(): Promise<void> {
    const search = this.#state.search;
    if (search === undefined || typeof search.more !== "object") {
      return;
    }
    const { query } = search;
    this.#set({ search: { ...search, more: "loading" } });
    try {
      const result = await this.client.call("messages.search", { query, before: search.more.cursor });
      const current = this.#state.search;
      if (current?.query !== query) {
        return;
      }
      this.#set({
        search: { query, results: [...current.results, ...result.messages], more: result.before === undefined ? "end" : { cursor: result.before } },
      });
    } catch (error) {
      if (this.#state.search?.query === query) {
        this.#set({ search: { ...search }, error: `Could not search: ${describe(error)}` });
      }
    }
  }

  /** Ends the search: the inbox shows again. */
  clearSearch(): void {
    const { search: _, ...rest } = this.#state;
    this.#replace(rest);
  }

  /** Shows a problem to the user, in their words. */
  showError(error: string): void {
    this.#set({ error });
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
        this.#refreshReplies(event.data.message);
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

  /**
   * Quotes a changed message (edited, deleted: API 1.7) as it now is in the
   * messages that answer it: the core only sends the changed one again.
   */
  #refreshReplies(message: Message): void {
    const messages = this.#state.messages[message.conversation_id];
    if (messages === undefined || !messages.some((m) => m.reply_to?.message_id === message.message_id)) {
      return;
    }
    const quote = replyTo(message);
    this.#setMessages(
      message.conversation_id,
      messages.map((m) => (m.reply_to?.message_id === message.message_id ? { ...m, reply_to: quote } : m)),
    );
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
  return cursor === undefined ? "network" : { cursor };
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

/** The size of an image, when the browser can decode it; the core takes the image without. */
async function imageSize(file: Blob): Promise<{ width: number; height: number } | undefined> {
  if (typeof createImageBitmap !== "function") {
    return undefined;
  }
  try {
    const bitmap = await createImageBitmap(file);
    const size = { width: bitmap.width, height: bitmap.height };
    bitmap.close();
    return size.width > 0 && size.height > 0 ? size : undefined;
  } catch {
    return undefined;
  }
}
