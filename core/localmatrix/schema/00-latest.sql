-- v0 -> v1: Latest revision
-- SPDX-FileCopyrightText: 2026 Quentin Raimbaud
-- SPDX-License-Identifier: AGPL-3.0-or-later
--
-- Local storage of the in-process Matrix implementation. The tables are
-- prefixed with "local_" because they share the database file with the
-- bridgev2 tables (portal, message, ghost, ...).

CREATE TABLE local_room (
    room_id    TEXT    NOT NULL PRIMARY KEY,
    -- The bridgev2 bridge whose connector created the room. Messages that
    -- the user sends in the room are handed to that bridge.
    bridge_id  TEXT    NOT NULL,
    creator    TEXT    NOT NULL,
    is_direct  BOOLEAN NOT NULL,
    created_at BIGINT  NOT NULL
);

-- Every event of every room, in the order they were stored.
CREATE TABLE local_event (
    stream_order INTEGER NOT NULL PRIMARY KEY AUTOINCREMENT,
    event_id     TEXT    NOT NULL UNIQUE,
    room_id      TEXT    NOT NULL REFERENCES local_room (room_id) ON DELETE CASCADE,
    sender       TEXT    NOT NULL,
    event_type   TEXT    NOT NULL,
    state_key    TEXT,
    content      TEXT    NOT NULL,
    timestamp    BIGINT  NOT NULL
);
CREATE INDEX local_event_room_idx ON local_event (room_id, stream_order);

-- Current state of every room (latest state event per type and state key).
CREATE TABLE local_state (
    room_id    TEXT NOT NULL REFERENCES local_room (room_id) ON DELETE CASCADE,
    event_type TEXT NOT NULL,
    state_key  TEXT NOT NULL,
    event_id   TEXT NOT NULL,
    PRIMARY KEY (room_id, event_type, state_key)
);

-- Delivery status of the messages the user sent, reported by the bridge.
CREATE TABLE local_message_status (
    event_id     TEXT   NOT NULL PRIMARY KEY,
    room_id      TEXT   NOT NULL REFERENCES local_room (room_id) ON DELETE CASCADE,
    status       TEXT   NOT NULL,
    reason       TEXT   NOT NULL,
    message      TEXT   NOT NULL,
    new_event_id TEXT   NOT NULL,
    updated_at   BIGINT NOT NULL
);

-- Connection state of the bridges and of each network login.
CREATE TABLE local_bridge_state (
    bridge_id  TEXT   NOT NULL,
    login_id   TEXT   NOT NULL,
    content    TEXT   NOT NULL,
    updated_at BIGINT NOT NULL,
    PRIMARY KEY (bridge_id, login_id)
);

CREATE TABLE local_profile (
    user_id     TEXT NOT NULL PRIMARY KEY,
    displayname TEXT NOT NULL,
    avatar_url  TEXT NOT NULL,
    extra       TEXT NOT NULL
);

-- Per-user room data: tags, mute, unread marker, read receipt.
CREATE TABLE local_room_account_data (
    user_id    TEXT NOT NULL,
    room_id    TEXT NOT NULL REFERENCES local_room (room_id) ON DELETE CASCADE,
    data_type  TEXT NOT NULL,
    content    TEXT NOT NULL,
    PRIMARY KEY (user_id, room_id, data_type)
);

CREATE TABLE local_media (
    media_id   TEXT   NOT NULL PRIMARY KEY,
    mime_type  TEXT   NOT NULL,
    file_name  TEXT   NOT NULL,
    data       BLOB   NOT NULL,
    created_at BIGINT NOT NULL
);
