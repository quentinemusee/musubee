// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

import { use, useEffect, useRef, useState, type CSSProperties } from "react";
import type { MessageKind, MessageMedia as Media } from "../core-api/types.gen";
import { autoload, formatDuration, formatSize, inlineType, mediaLabel, SAVE_TYPE, type MediaCache } from "../media";
import { saveBlob } from "../save";
import { StoreContext } from "./state";

type Loaded = { state: "idle" } | { state: "loading" } | { state: "ready"; url: string } | { state: "failed" };

/**
 * Reads a media as a Blob of the given type while enabled, and gives an
 * object URL of it, revoked when the component lets it go.
 */
export function useMediaURL(cache: MediaCache | undefined, mediaId: string | undefined, type: string | null, enabled: boolean): Loaded {
  const key = cache !== undefined && mediaId !== undefined && type !== null && enabled ? `${type}\n${mediaId}` : undefined;
  const [result, setResult] = useState<{ key: string; url?: string }>();
  useEffect(() => {
    if (key === undefined || cache === undefined || mediaId === undefined || type === null) {
      return;
    }
    let url: string | undefined;
    let current = true;
    cache.get(mediaId, type).then(
      (blob) => {
        if (current) {
          url = URL.createObjectURL(blob);
          setResult({ key, url });
        }
      },
      () => {
        if (current) {
          setResult({ key });
        }
      },
    );
    return () => {
      current = false;
      if (url !== undefined) {
        URL.revokeObjectURL(url);
      }
    };
  }, [cache, key, mediaId, type]);
  if (key === undefined) {
    return { state: "idle" };
  }
  if (result?.key !== key) {
    return { state: "loading" };
  }
  return result.url === undefined ? { state: "failed" } : { state: "ready", url: result.url };
}

/** The space an image or video takes before it is read: its own shape, within the bubble. */
function frameStyle(media: Media): CSSProperties | undefined {
  return media.width !== undefined && media.height !== undefined && media.width > 0 && media.height > 0
    ? { aspectRatio: `${media.width} / ${media.height}`, maxWidth: `min(100%, ${media.width}px)` }
    : undefined;
}

export interface MessageMediaProps {
  kind: MessageKind;
  media: Media;
  /** The caption, if any: it describes the image better than its kind. */
  caption: string;
}

/**
 * The media of a message: a photo or GIF shown in the bubble, a video or
 * audio player, or a file to save. Media up to AUTOLOAD_BYTES is read as
 * soon as its message shows (rows outside the viewport are not rendered);
 * larger media waits for the user to ask.
 */
export function MessageMedia({ kind, media, caption }: MessageMediaProps) {
  const cache = use(StoreContext)?.media;
  const type = inlineType(kind, media);
  const [asked, setAsked] = useState(false);
  const enabled = type !== null && (asked || autoload(media));
  const loaded = useMediaURL(cache, media.media_id, type, enabled);
  // The thumbnail stands in until the media is read.
  const thumbnail = useMediaURL(cache, media.thumbnail_id, "image/jpeg", media.thumbnail_id !== undefined && loaded.state !== "ready");
  const label = mediaLabel(kind, media.animated === true);
  const size = media.size === undefined ? "" : formatSize(media.size);
  // A voice message's playback speed (T2.70); the audio element keeps it while it plays.
  const audio = useRef<HTMLAudioElement>(null);
  const [rate, setRate] = useState<number>(1);

  if (type === null || loaded.state === "failed") {
    return <SaveFile kind={kind} media={media} cache={cache} unreadable={loaded.state === "failed"} />;
  }
  if (loaded.state !== "ready") {
    const poster = thumbnail.state === "ready" ? thumbnail.url : undefined;
    return (
      <div className="media media-pending" data-kind={kind} style={kind === "image" || kind === "video" ? frameStyle(media) : undefined}>
        {poster !== undefined ? <img className="media-poster" src={poster} alt="" /> : null}
        {enabled ? (
          <span className="media-loading">Loading {label.toLowerCase()}…</span>
        ) : (
          <button type="button" className="secondary media-show" onClick={() => setAsked(true)}>
            {kind === "image" ? "Show" : "Load"} {label.toLowerCase()}
            {size !== "" ? ` (${size})` : ""}
          </button>
        )}
      </div>
    );
  }
  switch (kind) {
    case "image":
      return (
        <div className="media" data-kind="image" style={frameStyle(media)}>
          <img src={loaded.url} alt={caption !== "" ? caption : label} />
        </div>
      );
    case "video":
      return (
        <div className="media" data-kind="video" style={frameStyle(media)}>
          {/* Networks send no captions with videos and voice messages: there is no track to give. */}
          {media.animated === true ? (
            // A GIF: no sound, no controls, on a loop, as networks show them.
            // oxlint-disable-next-line jsx-a11y/media-has-caption
            <video src={loaded.url} autoPlay loop muted playsInline aria-label={caption !== "" ? caption : label} />
          ) : (
            // oxlint-disable-next-line jsx-a11y/media-has-caption
            <video src={loaded.url} controls playsInline preload="metadata" aria-label={caption !== "" ? caption : label} />
          )}
        </div>
      );
    default:
      return (
        <div className="media" data-kind={kind}>
          {/* oxlint-disable-next-line jsx-a11y/media-has-caption -- no captions come with audio */}
          <audio ref={audio} src={loaded.url} controls preload="metadata" aria-label={describeAudio(kind, media)} />
          {media.duration_ms !== undefined ? <span className="media-duration">{formatDuration(media.duration_ms)}</span> : null}
          {kind === "voice" ? (
            // Voice messages are often long: they play faster on request (T2.70).
            <button
              type="button"
              className="secondary playback-rate"
              aria-label={`Playback speed: ${rateLabel(rate)}. Change`}
              onClick={() => {
                const next = nextRate(rate);
                setRate(next);
                if (audio.current !== null) {
                  // The default too: loading the media again resets the rate to it.
                  audio.current.defaultPlaybackRate = next;
                  audio.current.playbackRate = next;
                }
              }}
            >
              {rateLabel(rate)}
            </button>
          ) : null}
        </div>
      );
  }
}

/** The playback speeds of voice messages, in turn. */
export const PLAYBACK_RATES = [1, 1.5, 2] as const;

export function nextRate(rate: number): number {
  const index = PLAYBACK_RATES.indexOf(rate as (typeof PLAYBACK_RATES)[number]);
  return PLAYBACK_RATES[(index + 1) % PLAYBACK_RATES.length]!;
}

export function rateLabel(rate: number): string {
  return `${rate}×`;
}

function describeAudio(kind: MessageKind, media: Media): string {
  const label = mediaLabel(kind);
  return media.duration_ms === undefined ? label : `${label}, ${formatDuration(media.duration_ms)}`;
}

/**
 * A file, or media this interface does not show: its name and size, and a
 * button that reads it and hands it to the browser as a download, never as
 * a page (SAVE_TYPE).
 */
function SaveFile({ kind, media, cache, unreadable }: { kind: MessageKind; media: Media; cache: MediaCache | undefined; unreadable: boolean }) {
  const [state, setState] = useState<"idle" | "saving" | "failed">("idle");
  const name = media.file_name ?? mediaLabel(kind).toLowerCase();

  async function save() {
    if (cache === undefined) {
      return;
    }
    setState("saving");
    try {
      const blob = await cache.get(media.media_id, SAVE_TYPE);
      // Through the shell on Android, as a download elsewhere (docs/ADR/0068).
      await saveBlob(blob, name, media.mime_type || SAVE_TYPE);
      setState("idle");
    } catch {
      setState("failed");
    }
  }

  return (
    <div className="media media-file" data-kind={kind}>
      <span className="media-file-name">{name}</span>
      <span className="media-file-size">
        {media.size !== undefined ? formatSize(media.size) : ""}
        {unreadable ? " · could not be shown" : ""}
      </span>
      <button type="button" className="secondary" disabled={state === "saving" || cache === undefined} onClick={() => void save()}>
        {state === "saving" ? "Saving…" : state === "failed" ? "Try again" : "Save"}
        <span className="visually-hidden"> {name}</span>
      </button>
    </div>
  );
}
