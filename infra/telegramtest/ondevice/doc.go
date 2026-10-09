// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package ondevice holds the end-to-end test of Telegram "on device" (build
// tag "telegram"): the core of the apps talks to production Telegram
// itself, with mautrix-telegram inside and no homeserver
// (docs/ADR/0014-telegram-on-device.md).
package ondevice
