/*
 * SPDX-FileCopyrightText: 2026 Quentin Raimbaud
 * SPDX-License-Identifier: AGPL-3.0-or-later
 *
 * C interface of the memory probe (core/cmd/memprobe), built as a static
 * library for iOS. Test tool of T1.7 (docs/ADR/0015-ios-nse-memory.md), not
 * part of the app.
 */
#ifndef MUSUBEE_MEMPROBE_H
#define MUSUBEE_MEMPROBE_H

#ifdef __cplusplus
extern "C" {
#endif

/*
 * Runs the probe with a JSON configuration, for example
 *   {"data_dir": "/path", "core": true, "crypto": true, "messages": 20}
 * and returns the JSON report, NUL-terminated, which the caller releases
 * with free(). Blocks for the duration of the probe (about a second).
 */
char *musubee_memprobe_run(const char *config);

#ifdef __cplusplus
}
#endif

#endif /* MUSUBEE_MEMPROBE_H */
