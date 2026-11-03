// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

import { memo, useEffect, useId, useRef, useState, type RefObject } from "react";
import type { ConversationActions, Message, MessageReply } from "../core-api/types.gen";
import { MessageMedia } from "./MessageMedia";
import { hasMedia } from "../media";
import { DELETED_TEXT, hasActions, messageActions, ownReaction, QUICK_REACTIONS, reactionLabel, replyQuote, type MessageActions } from "../message";

const timeFormat = new Intl.DateTimeFormat(undefined, { hour: "2-digit", minute: "2-digit" });

const STATUS_LABELS: Record<string, string> = {
  // API 1.14: it goes once the account connects.
  queued: "Waiting for the network",
  sending: "Sending…",
  sent: "Sent",
  failed: "Not sent",
};

/** What the user does to a message, handled by the thread. */
export interface MessageHandlers {
  react(message: Message, key: string): void;
  /** key is the reaction's, as the core keyed it. */
  unreact(message: Message, key: string): void;
  reply(message: Message): void;
  edit(message: Message): void;
  delete(message: Message): void;
}

/** The answered message, quoted above an answer. */
function ReplyQuote({ reply }: { reply: MessageReply }) {
  const { sender, text } = replyQuote(reply);
  return (
    <blockquote className="reply" data-deleted={reply.deleted === true}>
      <span className="visually-hidden">In reply to </span>
      {sender !== "" ? <span className="reply-sender">{sender}</span> : null}
      <span className="reply-text">{text}</span>
    </blockquote>
  );
}

export interface MessageRowProps {
  message: Message;
  /** What the conversation's network allows (API 1.8); none without. */
  actions?: ConversationActions | undefined;
  handlers?: MessageHandlers | undefined;
  /** In a person's thread: the network the message came through, said on it (docs/ADR/0042). */
  via?: string | undefined;
}

/**
 * One message of a thread: a full-width row holding the bubble. The row has
 * no margin, so that a virtualized list can measure it whole.
 */
export const MessageRow = memo(function MessageRow({ message, actions, handlers, via }: MessageRowProps) {
  const deleted = message.deleted === true;
  const allowed = messageActions(message, handlers === undefined ? undefined : actions);
  const [menu, setMenu] = useState<"closed" | "open" | "confirm-delete">("closed");
  const menuId = useId();
  const toggle = useRef<HTMLButtonElement>(null);
  const showMenu = menu !== "closed" && hasActions(allowed) && handlers !== undefined;

  function close(refocus: boolean) {
    setMenu("closed");
    if (refocus) {
      toggle.current?.focus();
    }
  }

  return (
    <div className="message-row" data-message-id={message.message_id}>
      <div className="message" data-from-me={message.from_me} data-status={message.status} data-deleted={deleted}>
        {!message.from_me && message.sender_name ? <span className="sender">{message.sender_name}</span> : null}
        {message.reply_to && !deleted ? <ReplyQuote reply={message.reply_to} /> : null}
        {deleted ? (
          <span className="text" data-kind="deleted">
            {DELETED_TEXT}
          </span>
        ) : hasMedia(message) ? (
          <>
            <MessageMedia kind={message.kind} media={message.media} caption={message.text} />
            {message.text !== "" ? (
              <span className="text" data-kind="caption">
                {message.text}
              </span>
            ) : null}
          </>
        ) : (
          <span className="text" data-kind={message.kind}>
            {message.text}
          </span>
        )}
        {message.reactions && message.reactions.length > 0 && !deleted ? (
          <ul className="reactions" aria-label="Reactions">
            {message.reactions.map((r) => (
              <li
                key={r.key}
                className="reaction"
                data-from-me={r.from_me === true}
                aria-label={allowed.react && handlers !== undefined ? undefined : reactionLabel(r)}
              >
                {allowed.react && handlers !== undefined ? (
                  <button
                    type="button"
                    aria-pressed={r.from_me === true}
                    aria-label={reactionLabel(r)}
                    onClick={() => (r.from_me === true ? handlers.unreact(message, r.key) : handlers.react(message, r.key))}
                  >
                    <span aria-hidden="true">
                      {r.key} {r.count}
                    </span>
                  </button>
                ) : (
                  <span aria-hidden="true">
                    {r.key} {r.count}
                  </span>
                )}
              </li>
            ))}
          </ul>
        ) : null}
        <span className="meta">
          {via !== undefined ? <span className="via">via {via}</span> : null}
          {message.edited && !deleted ? <span className="edited">Edited</span> : null}
          <time dateTime={new Date(message.timestamp_ms).toISOString()}>{timeFormat.format(message.timestamp_ms)}</time>
          {message.from_me ? <span className="delivery">{message.error || STATUS_LABELS[message.status]}</span> : null}
          {hasActions(allowed) ? (
            <button
              ref={toggle}
              type="button"
              className="message-more"
              aria-label="More actions"
              aria-expanded={showMenu}
              aria-controls={showMenu ? menuId : undefined}
              onClick={() => setMenu(showMenu ? "closed" : "open")}
            >
              <span aria-hidden="true">⋯</span>
            </button>
          ) : null}
        </span>
      </div>
      {showMenu ? (
        <MessageMenu
          id={menuId}
          toggle={toggle}
          message={message}
          allowed={allowed}
          handlers={handlers}
          confirmDelete={menu === "confirm-delete"}
          onConfirmDelete={() => setMenu("confirm-delete")}
          onClose={close}
        />
      ) : null}
    </div>
  );
});

interface MessageMenuProps {
  id: string;
  /** The button that opens and closes the menu. */
  toggle: RefObject<HTMLButtonElement | null>;
  message: Message;
  allowed: MessageActions;
  handlers: MessageHandlers;
  confirmDelete: boolean;
  onConfirmDelete: () => void;
  /** Closes the menu; refocus gives the focus back to its button. */
  onClose: (refocus: boolean) => void;
}

/**
 * The actions of one message, under its bubble: plain buttons in the tab
 * order (not an ARIA menu, which needs arrow keys to work). Escape closes
 * it, and so do moving the focus out and pressing elsewhere. Deleting asks
 * first.
 */
function MessageMenu({ id, toggle, message, allowed, handlers, confirmDelete, onConfirmDelete, onClose }: MessageMenuProps) {
  const ref = useRef<HTMLFieldSetElement>(null);
  const outside = (target: unknown) =>
    target instanceof Node && !(ref.current?.contains(target) ?? false) && !(toggle.current?.contains(target) ?? false);

  const cancel = useRef<HTMLButtonElement>(null);

  // Listened to on the document: a press elsewhere does not always move the
  // focus (a tap on text, or WebKit, which does not focus buttons on click).
  // The toggle closes the menu itself, on its click.
  useEffect(() => {
    function onPointerDown(event: PointerEvent) {
      if (outside(event.target)) {
        onClose(false);
      }
    }
    function onFocusIn(event: FocusEvent) {
      if (outside(event.target)) {
        onClose(false);
      }
    }
    function onKeyDown(event: KeyboardEvent) {
      if (event.key === "Escape" && ref.current?.contains(document.activeElement)) {
        event.stopPropagation();
        onClose(true);
      }
    }
    document.addEventListener("pointerdown", onPointerDown);
    document.addEventListener("focusin", onFocusIn);
    document.addEventListener("keydown", onKeyDown);
    return () => {
      document.removeEventListener("pointerdown", onPointerDown);
      document.removeEventListener("focusin", onFocusIn);
      document.removeEventListener("keydown", onKeyDown);
    };
  });

  // Asked to confirm a deletion, the focus goes to the safe choice.
  useEffect(() => {
    if (confirmDelete) {
      cancel.current?.focus();
    }
  }, [confirmDelete]);

  /** Runs an action, then closes; refocus unless the action moves the focus. */
  function run(action: () => void, refocus = true) {
    action();
    onClose(refocus);
  }

  return (
    <fieldset ref={ref} id={id} className="message-menu" data-from-me={message.from_me}>
      <legend className="visually-hidden">Actions</legend>
      {confirmDelete ? (
        <>
          <p className="message-menu-question">Delete this message for everyone?</p>
          <div className="message-menu-buttons">
            <button ref={cancel} type="button" className="secondary" onClick={() => onClose(true)}>
              Cancel
            </button>
            <button type="button" className="danger" onClick={() => run(() => handlers.delete(message))}>
              Delete
            </button>
          </div>
        </>
      ) : (
        <>
          {allowed.react ? (
            <div className="quick-reactions">
              {QUICK_REACTIONS.map((emoji) => {
                const own = ownReaction(message, emoji);
                return (
                  <button
                    key={emoji}
                    type="button"
                    className="secondary"
                    aria-pressed={own !== undefined}
                    aria-label={`React with ${emoji}`}
                    onClick={() => run(() => (own === undefined ? handlers.react(message, emoji) : handlers.unreact(message, own.key)))}
                  >
                    <span aria-hidden="true">{emoji}</span>
                  </button>
                );
              })}
            </div>
          ) : null}
          <div className="message-menu-buttons">
            {allowed.reply ? (
              <button type="button" className="secondary" onClick={() => run(() => handlers.reply(message), false)}>
                Reply
              </button>
            ) : null}
            {allowed.edit ? (
              <button type="button" className="secondary" onClick={() => run(() => handlers.edit(message), false)}>
                Edit
              </button>
            ) : null}
            {allowed.delete ? (
              <button type="button" className="secondary danger-text" onClick={onConfirmDelete}>
                Delete…
              </button>
            ) : null}
          </div>
        </>
      )}
    </fieldset>
  );
}
