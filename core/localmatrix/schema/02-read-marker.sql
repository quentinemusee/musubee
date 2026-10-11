-- v2 -> v3 (compatible with v1+): The device user's read position in each room
-- SPDX-FileCopyrightText: 2026 Quentin Raimbaud
-- SPDX-License-Identifier: AGPL-3.0-or-later
CREATE TABLE local_read_marker (
    room_id      TEXT   NOT NULL PRIMARY KEY REFERENCES local_room (room_id) ON DELETE CASCADE,
    stream_order BIGINT NOT NULL,
    updated_at   BIGINT NOT NULL
);
