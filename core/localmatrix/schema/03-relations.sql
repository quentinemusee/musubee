-- v3 -> v4 (compatible with v1+): Relations between events (edits, reactions) and redactions
-- SPDX-FileCopyrightText: 2026 Quentin Raimbaud
-- SPDX-License-Identifier: AGPL-3.0-or-later
ALTER TABLE local_event ADD COLUMN rel_type TEXT;
ALTER TABLE local_event ADD COLUMN relates_to TEXT;
ALTER TABLE local_event ADD COLUMN redacted_by TEXT;
CREATE INDEX local_event_relation_idx ON local_event (relates_to, rel_type);

UPDATE local_event
SET rel_type=json_extract(content, '$."m.relates_to".rel_type'),
    relates_to=json_extract(content, '$."m.relates_to".event_id')
WHERE state_key IS NULL AND json_extract(content, '$."m.relates_to".rel_type') IS NOT NULL;

UPDATE local_event
SET redacted_by=(SELECT r.event_id FROM local_event r
                 WHERE r.room_id=local_event.room_id AND r.event_type='m.room.redaction'
                   AND json_extract(r.content, '$.redacts')=local_event.event_id
                 ORDER BY r.stream_order LIMIT 1)
WHERE state_key IS NULL AND content='{}';

-- Before v4, a redaction left the edits of the message it deleted. As a
-- redaction does since v4, only the relation is kept.
UPDATE local_event
SET content=json_object('m.relates_to', json(json_extract(content, '$."m.relates_to"'))),
    redacted_by=(SELECT t.redacted_by FROM local_event t WHERE t.event_id=local_event.relates_to)
WHERE rel_type='m.replace'
  AND relates_to IN (SELECT event_id FROM local_event WHERE redacted_by IS NOT NULL);
