// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

import { afterEach, describe, expect, test, vi } from "vitest";
import { MAX_RECORDING_MS, recordingName, recordingType, VoiceRecorder, type RecorderDeps } from "./recorder";

/** A fake microphone and MediaRecorder: records what the recorder does. */
function fakeBrowser(options: { refuse?: boolean; supported?: readonly string[] } = {}) {
  let now = 1000;
  const stoppedTracks: string[] = [];
  const recorders: FakeRecorder[] = [];
  class FakeRecorder extends EventTarget {
    state: "inactive" | "recording" = "inactive";
    timeslice: number | undefined;
    constructor(readonly mimeType: string) {
      super();
    }
    start(timeslice?: number) {
      this.state = "recording";
      this.timeslice = timeslice;
    }
    stop() {
      this.state = "inactive";
      this.emit(new Blob(["last"], { type: this.mimeType }));
      this.dispatchEvent(new Event("stop"));
    }
    emit(data: Blob) {
      const event = new Event("dataavailable") as Event & { data: Blob };
      event.data = data;
      this.dispatchEvent(event);
    }
  }
  const stream = { getTracks: () => [{ stop: () => stoppedTracks.push("audio") }] } as unknown as MediaStream;
  const deps: RecorderDeps = {
    getUserMedia: async (constraints) => {
      expect(constraints.video).toBe(false);
      if (options.refuse === true) {
        throw new DOMException("denied", "NotAllowedError");
      }
      return stream;
    },
    createRecorder: (_, type) => {
      const recorder = new FakeRecorder(type ?? "audio/webm");
      recorders.push(recorder);
      return recorder as unknown as MediaRecorder;
    },
    isTypeSupported: (type) => (options.supported ?? ["audio/webm;codecs=opus"]).includes(type),
    now: () => now,
  };
  return { deps, recorders, stoppedTracks, advance: (ms: number) => (now += ms) };
}

/** What Chromium's MediaRecorder records. */
function chromium(type: string): boolean {
  return type.startsWith("audio/webm") || type.startsWith("audio/mp4");
}

afterEach(() => {
  vi.useRealTimers();
});

describe("recording types and names", () => {
  test("asks for Ogg Opus first, then MP4, then WebM, among what the network takes", () => {
    expect(recordingType(chromium)).toBe("audio/mp4;codecs=opus");
    expect(recordingType((t) => t.startsWith("audio/ogg"))).toBe("audio/ogg;codecs=opus");
    expect(recordingType(() => false)).toBeUndefined();
    // Telegram's voice messages: Ogg, MPEG or MP4.
    expect(recordingType(chromium, (t) => t.startsWith("audio/mp4"))).toBe("audio/mp4;codecs=opus");
    // Nothing the network takes: the browser's first, sent as audio or a file.
    expect(
      recordingType(
        (t) => t.startsWith("audio/webm"),
        (t) => t.startsWith("audio/aac"),
      ),
    ).toBe("audio/webm;codecs=opus");
  });

  test("names a recording by its time and container", () => {
    const at = new Date(2026, 10, 3, 9, 5, 7);
    expect(recordingName("audio/webm;codecs=opus", at)).toBe("voice-20261103-090507.webm");
    expect(recordingName("audio/ogg;codecs=opus", at)).toBe("voice-20261103-090507.ogg");
    expect(recordingName("audio/mp4", at)).toBe("voice-20261103-090507.m4a");
  });
});

describe("VoiceRecorder", () => {
  test("records in chunks, gives the recording and its duration, and closes the microphone", async () => {
    const browser = fakeBrowser();
    const recorder = await VoiceRecorder.start(undefined, browser.deps);
    const fake = browser.recorders[0]!;
    expect(fake.mimeType).toBe("audio/webm;codecs=opus");
    expect(fake.timeslice).toBe(1000);
    fake.emit(new Blob(["first "], { type: fake.mimeType }));
    browser.advance(4200);
    expect(recorder.elapsedMs).toBe(4200);
    const recording = await recorder.stop();
    expect(recording.type).toBe("audio/webm;codecs=opus");
    expect(recording.durationMs).toBe(4200);
    expect(await recording.blob.text()).toBe("first last");
    expect(browser.stoppedTracks).toEqual(["audio"]);
    expect(recorder.closed).toBe(true);
  });

  test("throws a cancelled recording away and closes the microphone", async () => {
    const browser = fakeBrowser();
    const recorder = await VoiceRecorder.start(undefined, browser.deps);
    browser.recorders[0]!.emit(new Blob(["secret"]));
    recorder.cancel();
    recorder.cancel();
    expect(browser.stoppedTracks).toEqual(["audio"]);
    expect(browser.recorders[0]!.state).toBe("inactive");
  });

  test("stops by itself at the limit", async () => {
    vi.useFakeTimers();
    const browser = fakeBrowser();
    const recorder = await VoiceRecorder.start(undefined, browser.deps);
    vi.advanceTimersByTime(MAX_RECORDING_MS);
    expect(recorder.closed).toBe(true);
    expect(browser.stoppedTracks).toEqual(["audio"]);
    expect((await recorder.stop()).blob.size).toBeGreaterThan(0);
  });

  test("passes on a refused microphone", async () => {
    const browser = fakeBrowser({ refuse: true });
    await expect(VoiceRecorder.start(undefined, browser.deps)).rejects.toMatchObject({ name: "NotAllowedError" });
    expect(browser.recorders).toHaveLength(0);
  });
});
