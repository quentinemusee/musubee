/*
 * SPDX-FileCopyrightText: 2026 Quentin Raimbaud
 * SPDX-License-Identifier: AGPL-3.0-or-later
 *
 * C interface of the Musubee core, built as a shared library with
 *   go build -buildmode=c-shared ./core/ffi
 *
 * T1.2 spike (docs/ADR/0010-core-shared-library.md): requests, responses and
 * events are JSON documents in UTF-8. The commands are those of the Go
 * package core/embedded. The stable contract comes with T1.4.
 *
 * Memory: every musubee_buffer returned by the library is owned by the
 * caller, who must release it with musubee_free. Input pointers are only
 * read during the call; the library keeps no reference to them.
 *
 * Threads: every function may be called from any thread. musubee_call and
 * musubee_next_event block the calling thread, so call them away from a UI
 * thread. The Go runtime stays loaded until the process exits: the library
 * must not be unloaded.
 */
#ifndef MUSUBEE_H
#define MUSUBEE_H

#include <stddef.h>
#include <stdint.h>

#ifdef __cplusplus
extern "C" {
#endif

/* A core opened by musubee_open. 0 is never a valid handle. */
typedef uint64_t musubee_handle;

/* A byte buffer. data is NULL when the buffer is empty. */
typedef struct {
	uint8_t *data;
	size_t len;
} musubee_buffer;

/*
 * Opens a core from a JSON configuration, for example
 *   {"data_dir": "/path/to/app/data", "log_level": "info"}
 * Returns 0 on failure; then, if error is not NULL, *error receives a
 * message the caller must free.
 */
musubee_handle musubee_open(const uint8_t *config, size_t config_len, musubee_buffer *error);

/*
 * Runs one JSON request {"id": 1, "command": "...", "params": {...}} and
 * returns the JSON response {"id": 1, "result": ...} or
 * {"id": 1, "error": "..."}. Never returns an empty buffer.
 */
musubee_buffer musubee_call(musubee_handle handle, const uint8_t *request, size_t request_len);

/*
 * Waits up to timeout_ms milliseconds for the next JSON event. Returns an
 * empty buffer on timeout. After musubee_close, returns {"type":"closed"}.
 */
musubee_buffer musubee_next_event(musubee_handle handle, int32_t timeout_ms);

/* Stops a core and invalidates its handle. */
void musubee_close(musubee_handle handle);

/* Releases a buffer returned by the library. Accepts empty buffers. */
void musubee_free(musubee_buffer buffer);

#ifdef __cplusplus
}
#endif

#endif /* MUSUBEE_H */
