// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

import { useEffect, useRef, useState } from "react";
import { formatDuration } from "../media";
import { MAX_RECORDING_MS, recordingName, type Recording, type VoiceRecorder } from "../recorder";

export interface VoiceRecordingProps {
  recorder: VoiceRecorder;
  /** Called with the recording once the user sends it. */
  onSend: (recording: Recording, name: string) => void;
  /** Called once the user threw the recording away. */
  onCancel: () => void;
}

/**
 * The composer while a voice message is recorded: how long it has been
 * recording, Cancel and Send. Leaving it (another conversation, a closed
 * window) throws the recording away and closes the microphone.
 */
export function VoiceRecording({ recorder, onSend, onCancel }: VoiceRecordingProps) {
  const [elapsed, setElapsed] = useState(0);
  const [sending, setSending] = useState(false);
  const send = useRef<HTMLButtonElement>(null);

  useEffect(() => {
    send.current?.focus();
    const timer = setInterval(() => setElapsed(Math.min(recorder.elapsedMs, MAX_RECORDING_MS)), 250);
    return () => {
      clearInterval(timer);
      // Unmounted without being sent: nothing is kept.
      recorder.cancel();
    };
  }, [recorder]);

  function finish() {
    setSending(true);
    recorder.stop().then(
      (recording) => onSend(recording, recordingName(recording.type, new Date())),
      () => onCancel(),
    );
  }

  const stopped = elapsed >= MAX_RECORDING_MS;
  return (
    <div className="composer-form composer-recording">
      <span className="recording-dot" aria-hidden="true" />
      <span className="recording-time" role="timer" aria-label={stopped ? "Recording stopped at the limit" : "Recording"}>
        {formatDuration(elapsed)}
      </span>
      <output className="visually-hidden">{stopped ? "The recording reached 15 minutes and stopped." : "Recording a voice message."}</output>
      <button
        type="button"
        className="secondary"
        disabled={sending}
        onClick={() => {
          recorder.cancel();
          onCancel();
        }}
      >
        Cancel
      </button>
      <button ref={send} type="button" disabled={sending} onClick={finish}>
        Send voice message
      </button>
    </div>
  );
}
