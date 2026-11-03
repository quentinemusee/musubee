// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

// The media of messages (API 1.9, docs/ADR/0036): what the interface says
// of them, which ones it shows inline, and reading their bytes from the
// core into Blobs, kept in a small cache.

import type { MediaAction, MediaActions, Message, MessageKind, MessageMedia } from "./core-api/types.gen";

/** The most media.read returns at once (API 1.9). */
export const MEDIA_PART = 1_048_576;

/** Media up to this size is read as soon as its message shows; larger media waits for the user. */
export const AUTOLOAD_BYTES = 8 * 1_048_576;

/** How many bytes of media the cache keeps, besides what is on screen. */
export const CACHE_BYTES = 64 * 1_048_576;

const MEDIA_KINDS: ReadonlySet<MessageKind> = new Set(["image", "video", "audio", "voice", "file"]);

/** Whether a message holds media (and media.read can read it). */
export function hasMedia(message: Message): message is Message & { media: MessageMedia } {
  return message.media !== undefined && MEDIA_KINDS.has(message.kind) && message.deleted !== true;
}

/** What a kind of media is called, where its content cannot show. */
export function mediaLabel(kind: MessageKind, animated = false): string {
  switch (kind) {
    case "image":
      return animated ? "GIF" : "Photo";
    case "video":
      return animated ? "GIF" : "Video";
    case "audio":
      return "Audio";
    case "voice":
      return "Voice message";
    case "file":
      return "File";
    default:
      return "";
  }
}

/**
 * A message in one line, for the inbox's preview, a quote or an
 * announcement: its text, or for media its caption after what it is
 * ("Photo: a sunset"), or the file's name.
 */
export function summary(message: Pick<Message, "kind" | "text" | "media">): string {
  const label = mediaLabel(message.kind, message.media?.animated === true);
  if (label === "") {
    return message.text;
  }
  if (message.text !== "") {
    return `${label}: ${message.text}`;
  }
  if (message.kind === "file" && message.media?.file_name !== undefined) {
    return `${label}: ${message.media.file_name}`;
  }
  return label;
}

const INLINE_TYPES: Readonly<Record<string, readonly string[]>> = {
  // No SVG: an image the sender wrote as code.
  image: ["image/png", "image/jpeg", "image/gif", "image/webp", "image/avif", "image/bmp"],
  video: ["video/mp4", "video/webm", "video/ogg", "video/quicktime"],
  audio: ["audio/ogg", "audio/mpeg", "audio/mp4", "audio/aac", "audio/wav", "audio/x-wav", "audio/webm", "audio/flac", "audio/opus"],
};

/**
 * The type to give a media's Blob to show it in the thread, or null to
 * offer it as a file only. The type comes from the sender: only image,
 * video and audio types that browsers decode without running anything
 * qualify, and every other Blob gets application/octet-stream (saveType),
 * so that no media can ever render as a page of the app's origin.
 */
export function inlineType(kind: MessageKind, media: MessageMedia): string | null {
  const family = kind === "voice" ? "audio" : kind;
  const type = media.mime_type.split(";")[0]?.trim().toLowerCase() ?? "";
  return INLINE_TYPES[family]?.includes(type) === true ? type : null;
}

/** The type of a Blob that is only saved, never shown. */
export const SAVE_TYPE = "application/octet-stream";

/** Whether a media is read as soon as its message shows. */
export function autoload(media: MessageMedia): boolean {
  return media.size === undefined || media.size <= AUTOLOAD_BYTES;
}

/** A size for people: "820 bytes", "48 KB", "12.5 MB". */
export function formatSize(bytes: number): string {
  if (bytes < 1000) {
    return `${bytes} bytes`;
  }
  const units = ["KB", "MB", "GB"];
  let value = bytes / 1000;
  let unit = 0;
  while (value >= 1000 && unit < units.length - 1) {
    value /= 1000;
    unit++;
  }
  return `${value >= 10 ? Math.round(value) : Math.round(value * 10) / 10} ${units[unit]}`;
}

/** A duration as a clock: "0:04", "12:30", "1:02:03". */
export function formatDuration(ms: number): string {
  const total = Math.round(ms / 1000);
  const seconds = String(total % 60).padStart(2, "0");
  const minutes = Math.floor(total / 60) % 60;
  const hours = Math.floor(total / 3600);
  return hours > 0 ? `${hours}:${String(minutes).padStart(2, "0")}:${seconds}` : `${minutes}:${seconds}`;
}

/** Decodes base64 (RFC 4648, with padding). */
export function decodeBase64(data: string): Uint8Array<ArrayBuffer> {
  const binary = atob(data);
  const bytes = new Uint8Array(binary.length);
  for (let i = 0; i < binary.length; i++) {
    bytes[i] = binary.charCodeAt(i);
  }
  return bytes;
}

/** Reads one part of a media: the core's media.read. */
export type ReadPart = (params: { media_id: string; offset: number; length: number }) => Promise<{ size: number; data: string }>;

/** Reads a whole media, part after part. */
export async function readMedia(read: ReadPart, mediaId: string): Promise<Uint8Array<ArrayBuffer>[]> {
  const parts: Uint8Array<ArrayBuffer>[] = [];
  let offset = 0;
  for (;;) {
    const result = await read({ media_id: mediaId, offset, length: MEDIA_PART });
    const bytes = decodeBase64(result.data);
    if (bytes.length > 0) {
      parts.push(bytes);
    }
    offset += bytes.length;
    if (bytes.length === 0 || offset >= result.size) {
      return parts;
    }
  }
}

/**
 * Media read from the core, as Blobs of a given type, the most recently
 * used kept up to a number of bytes. A media being read is read once,
 * however many ask for it.
 */
export class MediaCache {
  readonly #read: ReadPart;
  readonly #maxBytes: number;
  /** By media ID and type, the oldest use first. */
  readonly #entries = new Map<string, Promise<Blob>>();
  readonly #sizes = new Map<string, number>();

  constructor(read: ReadPart, maxBytes = CACHE_BYTES) {
    this.#read = read;
    this.#maxBytes = maxBytes;
  }

  /** The media as a Blob of this type. */
  get(mediaId: string, type: string): Promise<Blob> {
    const key = `${type}\n${mediaId}`;
    let entry = this.#entries.get(key);
    if (entry !== undefined) {
      this.#entries.delete(key);
      this.#entries.set(key, entry);
      return entry;
    }
    entry = readMedia(this.#read, mediaId).then((parts) => {
      const blob = new Blob(parts, { type });
      if (this.#entries.get(key) === entry) {
        this.#sizes.set(key, blob.size);
        this.#evict();
      }
      return blob;
    });
    entry.catch(() => {
      if (this.#entries.get(key) === entry) {
        this.#entries.delete(key);
        this.#sizes.delete(key);
      }
    });
    this.#entries.set(key, entry);
    return entry;
  }

  /** Forgets every media, for example once the core starts over. */
  clear(): void {
    this.#entries.clear();
    this.#sizes.clear();
  }

  #evict(): void {
    let total = 0;
    for (const size of this.#sizes.values()) {
      total += size;
    }
    for (const key of this.#entries.keys()) {
      if (total <= this.#maxBytes) {
        return;
      }
      const size = this.#sizes.get(key);
      if (size !== undefined) {
        total -= size;
        this.#entries.delete(key);
        this.#sizes.delete(key);
      }
    }
  }
}

/** Encodes bytes in base64 (RFC 4648, with padding). */
export function encodeBase64(bytes: Uint8Array): string {
  let binary = "";
  // In slices: String.fromCharCode takes its bytes as arguments.
  for (let i = 0; i < bytes.length; i += 0x8000) {
    binary += String.fromCharCode(...bytes.subarray(i, i + 0x8000));
  }
  return btoa(binary);
}

/** How a file the user picked is sent, and what the network takes of it. */
export interface SendChoice {
  kind: "image" | "video" | "audio" | "voice" | "file";
  action: MediaAction;
}

/**
 * How to send a file of this type: as what it is when the network takes
 * that kind (a photo as an image, a video as a video), as a file otherwise;
 * null when the network takes neither. An image is sent as one only in a
 * type shown inline (inlineType): an SVG goes as a file.
 */
export function sendChoice(type: string, media: MediaActions | undefined): SendChoice | null {
  if (media === undefined) {
    return null;
  }
  const plain = type.split(";")[0]?.trim().toLowerCase() ?? "";
  const family = plain.split("/")[0];
  let kind: SendChoice["kind"] = "file";
  if (family === "image" && INLINE_TYPES["image"]?.includes(plain) === true) {
    kind = "image";
  } else if (family === "video" || family === "audio") {
    kind = family;
  }
  const action = media[kind];
  if (action !== undefined) {
    return { kind, action };
  }
  return media.file === undefined ? null : { kind: "file", action: media.file };
}

/** Why a file cannot be sent with this choice, for the user, or null if it can. */
export function sendProblem(size: number, choice: SendChoice | null, hasCaption: boolean): string | null {
  if (choice === null) {
    return "This conversation does not take files.";
  }
  if (size === 0) {
    return "The file is empty.";
  }
  const max = Math.min(choice.action.max_size ?? MAX_UPLOAD_BYTES, MAX_UPLOAD_BYTES);
  if (size > max) {
    return `The file is too large: this conversation takes up to ${formatSize(max)}.`;
  }
  if (hasCaption && choice.action.caption !== true) {
    return "This conversation takes no text with this file: send the text on its own.";
  }
  return null;
}

/** The largest media the core takes (uploads.append, API 1.10). */
export const MAX_UPLOAD_BYTES = 104_857_600;

/** Adds a part to an upload: the core's uploads.append. */
export type AppendPart = (params: { upload_id?: string; data: string }) => Promise<{ upload_id: string; size: number }>;

/**
 * Gives a Blob to the core in parts of MEDIA_PART, reporting the bytes
 * given so far; returns the upload's ID.
 */
export async function uploadBlob(append: AppendPart, blob: Blob, onProgress?: (sent: number) => void): Promise<string> {
  let uploadId: string | undefined;
  for (let offset = 0; offset < blob.size; offset += MEDIA_PART) {
    const bytes = new Uint8Array(await blob.slice(offset, offset + MEDIA_PART).arrayBuffer());
    const result = await append(uploadId === undefined ? { data: encodeBase64(bytes) } : { upload_id: uploadId, data: encodeBase64(bytes) });
    uploadId = result.upload_id;
    onProgress?.(result.size);
  }
  if (uploadId === undefined) {
    throw new Error("the file is empty");
  }
  return uploadId;
}
