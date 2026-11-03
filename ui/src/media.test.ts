// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

import { describe, expect, test } from "vitest";
import type { MessageMedia } from "./core-api/types.gen";
import {
  decodeBase64,
  encodeBase64,
  formatDuration,
  formatSize,
  inlineType,
  MAX_UPLOAD_BYTES,
  MEDIA_PART,
  MediaCache,
  readMedia,
  SAVE_TYPE,
  sendChoice,
  sendProblem,
  summary,
  uploadBlob,
  type AppendPart,
  type ReadPart,
} from "./media";

const media = (mime_type: string, extra: Partial<MessageMedia> = {}): MessageMedia => ({ media_id: "f.1", mime_type, ...extra });

/** A core holding one media of `size` bytes, each byte its offset modulo 256; counts the calls. */
function fakeCore(size: number) {
  const calls: { offset: number; length: number }[] = [];
  const read: ReadPart = async ({ offset, length }) => {
    calls.push({ offset, length });
    const end = Math.min(size, offset + length);
    let binary = "";
    for (let i = offset; i < end; i++) {
      binary += String.fromCharCode(i % 256);
    }
    return { size, data: btoa(binary) };
  };
  return { read, calls };
}

/** A core whose media has no bytes left to give. */
const readNothing: ReadPart = async () => ({ size: 100, data: "" });

describe("summary", () => {
  test("says what media is, with its caption or file name", () => {
    expect(summary({ kind: "text", text: "hi" })).toBe("hi");
    expect(summary({ kind: "image", text: "" })).toBe("Photo");
    expect(summary({ kind: "image", text: "a sunset" })).toBe("Photo: a sunset");
    expect(summary({ kind: "video", text: "", media: media("video/mp4", { animated: true }) })).toBe("GIF");
    expect(summary({ kind: "voice", text: "" })).toBe("Voice message");
    expect(summary({ kind: "file", text: "", media: media("application/pdf", { file_name: "plan.pdf" }) })).toBe("File: plan.pdf");
  });
});

describe("inlineType", () => {
  test("shows only types that browsers decode without running anything", () => {
    expect(inlineType("image", media("image/JPEG"))).toBe("image/jpeg");
    expect(inlineType("image", media("image/svg+xml"))).toBeNull();
    expect(inlineType("image", media("text/html"))).toBeNull();
    expect(inlineType("video", media("video/mp4; codecs=avc1"))).toBe("video/mp4");
    expect(inlineType("voice", media("audio/ogg"))).toBe("audio/ogg");
    expect(inlineType("file", media("image/png"))).toBeNull();
    expect(inlineType("image", media(""))).toBeNull();
  });
});

describe("formats", () => {
  test("sizes and durations", () => {
    expect(formatSize(820)).toBe("820 bytes");
    expect(formatSize(48_213)).toBe("48 KB");
    expect(formatSize(1_250_000)).toBe("1.3 MB");
    expect(formatDuration(4_200)).toBe("0:04");
    expect(formatDuration(750_000)).toBe("12:30");
    expect(formatDuration(3_723_000)).toBe("1:02:03");
  });
});

describe("readMedia", () => {
  test("reads a media in parts of at most 1 MiB", async () => {
    const core = fakeCore(MEDIA_PART * 2 + 10);
    const parts = await readMedia(core.read, "f.1");
    expect(core.calls.map((c) => c.offset)).toEqual([0, MEDIA_PART, MEDIA_PART * 2]);
    expect(parts.reduce((n, p) => n + p.length, 0)).toBe(MEDIA_PART * 2 + 10);
    expect(parts[1]?.[0]).toBe(MEDIA_PART % 256);
  });

  test("stops on an empty part", async () => {
    expect(await readMedia(readNothing, "f.1")).toEqual([]);
  });

  test("decodes base64", () => {
    expect([...decodeBase64("AAEC/w==")]).toEqual([0, 1, 2, 255]);
  });
});

describe("MediaCache", () => {
  test("reads a media once, and keeps the latest within its budget", async () => {
    const core = fakeCore(100);
    const cache = new MediaCache(core.read, 250);
    const [a, b] = await Promise.all([cache.get("f.1", "image/png"), cache.get("f.1", "image/png")]);
    expect(a).toBe(b);
    expect(a.type).toBe("image/png");
    expect(core.calls).toHaveLength(1);
    // Another type is another Blob: what is saved never takes a shown type.
    expect((await cache.get("f.1", SAVE_TYPE)).type).toBe(SAVE_TYPE);
    await cache.get("f.2", "image/png");
    // 300 bytes for 250: the oldest goes.
    await cache.get("f.1", "image/png");
    expect(core.calls).toHaveLength(4);
  });

  test("forgets a failed read", async () => {
    let fail = true;
    const cache = new MediaCache(async () => {
      if (fail) {
        throw new Error("not_found");
      }
      return { size: 1, data: "AA==" };
    });
    await expect(cache.get("f.1", "image/png")).rejects.toThrow("not_found");
    fail = false;
    expect((await cache.get("f.1", "image/png")).size).toBe(1);
  });
});

describe("sending", () => {
  const all = { image: { caption: true, max_size: 1000 }, video: {}, audio: {}, voice: {}, file: { max_size: 500 } };

  test("sends a file as what it is when the network takes that kind, as a file otherwise", () => {
    expect(sendChoice("image/png", all)?.kind).toBe("image");
    expect(sendChoice("image/svg+xml", all)?.kind).toBe("file");
    expect(sendChoice("video/mp4", all)?.kind).toBe("video");
    expect(sendChoice("audio/ogg", all)?.kind).toBe("audio");
    expect(sendChoice("application/pdf", all)?.kind).toBe("file");
    expect(sendChoice("image/png", { file: {} })?.kind).toBe("file");
    expect(sendChoice("image/png", { video: {} })).toBeNull();
    expect(sendChoice("image/png", undefined)).toBeNull();
  });

  test("says why a file cannot go", () => {
    const image = sendChoice("image/png", all);
    expect(sendProblem(10, image, true)).toBeNull();
    expect(sendProblem(1001, image, false)).toBe("The file is too large: this conversation takes up to 1 KB.");
    expect(sendProblem(0, image, false)).toBe("The file is empty.");
    expect(sendProblem(10, sendChoice("application/pdf", all), true)).toContain("no text");
    expect(sendProblem(10, null, false)).toBe("This conversation does not take files.");
    expect(sendProblem(MAX_UPLOAD_BYTES + 1, sendChoice("video/mp4", all), false)).toContain("too large");
  });

  test("encodes base64", () => {
    expect(encodeBase64(new Uint8Array([0, 1, 2, 255]))).toBe("AAEC/w==");
    const large = new Uint8Array(100_000).map((_, i) => i % 256);
    expect([...decodeBase64(encodeBase64(large))]).toEqual([...large]);
  });

  test("gives a Blob to the core in parts of at most 1 MiB", async () => {
    const parts: { upload_id?: string; bytes: number }[] = [];
    let held = 0;
    const append: AppendPart = async (params) => {
      const bytes = decodeBase64(params.data).length;
      parts.push({ ...(params.upload_id === undefined ? {} : { upload_id: params.upload_id }), bytes });
      held += bytes;
      return { upload_id: "u.1", size: held };
    };
    const progress: number[] = [];
    const uploadId = await uploadBlob(append, new Blob([new Uint8Array(MEDIA_PART + 5)]), (sent) => progress.push(sent));
    expect(uploadId).toBe("u.1");
    expect(parts).toEqual([{ bytes: MEDIA_PART }, { upload_id: "u.1", bytes: 5 }]);
    expect(progress).toEqual([MEDIA_PART, MEDIA_PART + 5]);
  });
});
