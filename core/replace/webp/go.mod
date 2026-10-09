// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

// Stands in for go.mau.fi/webp, which binds libwebp with cgo, so that the
// core stays pure Go (docs/ADR/0014-telegram-on-device.md). core/go.mod
// replaces the real module with this one.
module go.mau.fi/webp

go 1.25.0
