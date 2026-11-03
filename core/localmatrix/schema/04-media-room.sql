-- v4 -> v5 (compatible with v1+): The room of each media, deleted with it
-- SPDX-FileCopyrightText: 2026 Quentin Raimbaud
-- SPDX-License-Identifier: AGPL-3.0-or-later
ALTER TABLE local_media ADD COLUMN room_id TEXT REFERENCES local_room (room_id) ON DELETE CASCADE;
CREATE INDEX local_media_room_idx ON local_media (room_id);

-- Media stored before v5 belongs to the room whose events name it. Media
-- that no event names (avatars, or media of a deleted room) stays without
-- a room.
UPDATE local_media
SET room_id=(SELECT e.room_id FROM local_event e
             WHERE e.state_key IS NULL AND e.event_type='m.room.message'
               AND instr(e.content, '/' || local_media.media_id || '"') > 0
             LIMIT 1);
