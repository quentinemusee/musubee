// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

import { use, useEffect, useRef, useState } from "react";
import type { Message } from "../core-api/types.gen";
import { gallery, type Gallery } from "../gallery";
import { inlineType, mediaLabel } from "../media";
import { useMediaURL } from "./MessageMedia";
import { StoreContext, useStore } from "./state";

/** A picture's small view, read through the media cache; a label until then. */
function Thumbnail({ message }: { message: Message }) {
  const cache = use(StoreContext)?.media;
  const media = message.media!;
  const type = message.kind === "image" ? inlineType(message.kind, media) : null;
  const thumbnail = media.thumbnail_id ?? (type === null ? undefined : media.media_id);
  const loaded = useMediaURL(cache, thumbnail, media.thumbnail_id === undefined ? type : "image/jpeg", thumbnail !== undefined);
  return loaded.state === "ready" ? <img src={loaded.url} alt="" /> : <span>{mediaLabel(message.kind, media.animated === true)}</span>;
}

/**
 * A conversation's media and files (T2.77), in a modal dialog: what the
 * device stores. A click opens the message in the thread.
 */
export function MediaGallery({ conversationId, name, onClose }: { conversationId: string; name: string; onClose: () => void }) {
  const store = useStore();
  const dialog = useRef<HTMLDialogElement>(null);
  const [found, setFound] = useState<Gallery | "failed" | undefined>(undefined);

  useEffect(() => {
    const element = dialog.current;
    // As AppearanceSettings: no close() on cleanup (StrictMode).
    if (element !== null && !element.open) {
      element.showModal();
    }
    let current = true;
    store.allStoredMessages(conversationId).then(
      (messages) => current && setFound(gallery(messages)),
      () => current && setFound("failed"),
    );
    return () => {
      current = false;
    };
  }, [store, conversationId]);

  function open(message: Message) {
    dialog.current?.close();
    void store.reveal(message);
  }

  return (
    <dialog ref={dialog} className="settings gallery" aria-labelledby="gallery-title" onClose={onClose}>
      <h2 id="gallery-title">Media and files of {name}</h2>
      {found === undefined ? (
        <p className="settings-note">Reading…</p>
      ) : found === "failed" ? (
        <p role="alert">Musubee could not read the conversation.</p>
      ) : found.pictures.length === 0 && found.others.length === 0 ? (
        <p className="settings-note">No media or files on this device.</p>
      ) : (
        <>
          {found.pictures.length === 0 ? null : (
            <ul className="gallery-pictures" aria-label="Photos and videos">
              {found.pictures.map((message) => (
                <li key={message.message_id}>
                  <button
                    type="button"
                    className="secondary"
                    aria-label={`${mediaLabel(message.kind, message.media?.animated === true)}${message.text === "" ? "" : `: ${message.text}`}`}
                    onClick={() => open(message)}
                  >
                    <Thumbnail message={message} />
                  </button>
                </li>
              ))}
            </ul>
          )}
          {found.others.length === 0 ? null : (
            <ul className="gallery-files" aria-label="Files and audio">
              {found.others.map((message) => (
                <li key={message.message_id}>
                  <button type="button" className="secondary" onClick={() => open(message)}>
                    {message.media?.file_name || mediaLabel(message.kind)}
                  </button>
                </li>
              ))}
            </ul>
          )}
        </>
      )}
      <div className="settings-buttons">
        <button type="button" onClick={() => dialog.current?.close()}>
          Done
        </button>
      </div>
    </dialog>
  );
}
