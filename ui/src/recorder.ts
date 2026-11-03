// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

// Recording voice messages with the browser's MediaRecorder (docs/ADR/0038).
// The microphone is opened when recording starts and closed when it ends,
// whatever ends it; nothing is recorded in the background.

/** The containers asked for, the preferred first: Ogg Opus is what voice messages are on most networks. */
export const RECORDING_TYPES: readonly string[] = ["audio/ogg;codecs=opus", "audio/webm;codecs=opus", "audio/webm", "audio/mp4"];

/** A recording stops by itself after 15 minutes. */
export const MAX_RECORDING_MS = 15 * 60_000;

/** The first recording type the browser takes, or undefined to let it choose. */
export function recordingType(isTypeSupported: (type: string) => boolean): string | undefined {
  return RECORDING_TYPES.find((type) => isTypeSupported(type));
}

function pad(n: number): string {
  return String(n).padStart(2, "0");
}

/** The file name of a recording made at this time, with its container's extension. */
export function recordingName(type: string, at: Date): string {
  const plain = type.split(";")[0]?.trim() ?? "";
  const extension = plain === "audio/ogg" ? "ogg" : plain === "audio/mp4" ? "m4a" : "webm";
  return `voice-${at.getFullYear()}${pad(at.getMonth() + 1)}${pad(at.getDate())}-${pad(at.getHours())}${pad(at.getMinutes())}${pad(at.getSeconds())}.${extension}`;
}

/** What VoiceRecorder needs of the browser: replaced in the tests. */
export interface RecorderDeps {
  getUserMedia(constraints: MediaStreamConstraints): Promise<MediaStream>;
  createRecorder(stream: MediaStream, type: string | undefined): MediaRecorder;
  isTypeSupported(type: string): boolean;
  now(): number;
}

/** Whether this browser can record: it has the microphone API and MediaRecorder. */
export function canRecord(): boolean {
  return typeof navigator !== "undefined" && typeof navigator.mediaDevices?.getUserMedia === "function" && typeof MediaRecorder === "function";
}

function browserDeps(): RecorderDeps {
  return {
    getUserMedia: (constraints) => navigator.mediaDevices.getUserMedia(constraints),
    createRecorder: (stream, type) => new MediaRecorder(stream, type === undefined ? {} : { mimeType: type }),
    isTypeSupported: (type) => MediaRecorder.isTypeSupported(type),
    now: () => Date.now(),
  };
}

/** A recording that ended: its bytes, their type and how long it lasted. */
export interface Recording {
  blob: Blob;
  type: string;
  durationMs: number;
}

/** One voice message being recorded. */
export class VoiceRecorder {
  readonly #stream: MediaStream;
  readonly #recorder: MediaRecorder;
  readonly #deps: RecorderDeps;
  readonly #startedAt: number;
  readonly #chunks: Blob[] = [];
  readonly #limit: ReturnType<typeof setTimeout>;
  #ended: Promise<Recording>;
  #closed = false;
  #cancelled = false;

  private constructor(stream: MediaStream, recorder: MediaRecorder, deps: RecorderDeps) {
    this.#stream = stream;
    this.#recorder = recorder;
    this.#deps = deps;
    this.#startedAt = deps.now();
    recorder.addEventListener("dataavailable", (event) => {
      if (event.data.size > 0 && !this.#cancelled) {
        this.#chunks.push(event.data);
      }
    });
    this.#ended = new Promise((resolve, reject) => {
      recorder.addEventListener("stop", () => {
        const type = recorder.mimeType || this.#chunks[0]?.type || "audio/webm";
        resolve({
          blob: new Blob(this.#chunks, { type }),
          type,
          durationMs: Math.max(0, this.#deps.now() - this.#startedAt),
        });
      });
      recorder.addEventListener("error", () => reject(new Error("the recording failed")));
    });
    // Rejections are read by stop(); a cancelled recording's are not.
    this.#ended.catch(() => undefined);
    this.#limit = setTimeout(() => this.#end(), MAX_RECORDING_MS);
    // Chunks every second, so that a long recording is not one buffer.
    recorder.start(1000);
  }

  /** Opens the microphone and starts recording; rejects when the user or the system refuses it. */
  static async start(deps: RecorderDeps = browserDeps()): Promise<VoiceRecorder> {
    const stream = await deps.getUserMedia({
      audio: { echoCancellation: true, noiseSuppression: true },
      video: false,
    });
    try {
      return new VoiceRecorder(stream, deps.createRecorder(stream, recordingType(deps.isTypeSupported)), deps);
    } catch (error) {
      for (const track of stream.getTracks()) {
        track.stop();
      }
      throw error;
    }
  }

  /** How long it has been recording. */
  get elapsedMs(): number {
    return Math.max(0, this.#deps.now() - this.#startedAt);
  }

  /** Whether it stopped by itself (MAX_RECORDING_MS) or was stopped. */
  get closed(): boolean {
    return this.#closed;
  }

  /** Stops recording and gives the recording. */
  stop(): Promise<Recording> {
    this.#end();
    return this.#ended;
  }

  /** Stops recording and throws it away. */
  cancel(): void {
    this.#cancelled = true;
    this.#chunks.length = 0;
    this.#end();
  }

  #end(): void {
    clearTimeout(this.#limit);
    if (this.#closed) {
      return;
    }
    this.#closed = true;
    if (this.#recorder.state !== "inactive") {
      this.#recorder.stop();
    }
    // The microphone closes now, not when the page goes.
    for (const track of this.#stream.getTracks()) {
      track.stop();
    }
  }
}
