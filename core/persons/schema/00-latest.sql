-- v0 -> v1: Latest revision
-- SPDX-FileCopyrightText: 2026 Quentin Raimbaud
-- SPDX-License-Identifier: AGPL-3.0-or-later
--
-- The persons the user merged conversations into (docs/ADR/0017). The
-- tables are prefixed with "musubee_" because they share the database file
-- with the bridgev2 tables and the local Matrix storage.

CREATE TABLE musubee_person (
    person_id  TEXT   NOT NULL PRIMARY KEY,
    name       TEXT   NOT NULL,
    created_at BIGINT NOT NULL,
    updated_at BIGINT NOT NULL
);

-- A conversation is named by its identity on its network, not by the local
-- room that shows it: rooms are created by each device, while the network's
-- chat, and the account that reaches it, are the same on every device of
-- the user. A link whose conversation is not on this device (an account not
-- added here, or logged out) is kept, and shows again when it comes back.
CREATE TABLE musubee_person_link (
    network_id TEXT   NOT NULL,
    chat_id    TEXT   NOT NULL,
    -- The account that reaches the chat; empty for a chat shared by every
    -- account of the user on that network.
    receiver   TEXT   NOT NULL,
    person_id  TEXT   NOT NULL REFERENCES musubee_person (person_id) ON DELETE CASCADE,
    linked_at  BIGINT NOT NULL,
    PRIMARY KEY (network_id, chat_id, receiver)
);
CREATE INDEX musubee_person_link_person_idx ON musubee_person_link (person_id);
