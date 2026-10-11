// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package signaltest holds the end-to-end test of Signal "on device" (build
// tags "signal" and "musubee_signal"): the core talks to Signal's staging
// service itself, with mautrix-signal inside, linked to a test account that
// signal-cli drives (docs/ADR/0025-signal-on-device.md, README.md here).
package signaltest
