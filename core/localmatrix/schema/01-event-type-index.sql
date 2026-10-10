-- v1 -> v2 (compatible with v1+): Index the events by type, to find the latest message of each room
-- SPDX-FileCopyrightText: 2026 Quentin Raimbaud
-- SPDX-License-Identifier: AGPL-3.0-or-later
CREATE INDEX local_event_type_idx ON local_event (room_id, event_type, stream_order);
