/*
 * SPDX-FileCopyrightText: 2026 Quentin Raimbaud
 * SPDX-License-Identifier: AGPL-3.0-or-later
 *
 * Host program of the T1.2 spike: loads the core shared library, calls it
 * through the C interface of musubee.h, and prints one JSON line of
 * measurements on stdout. Run by ffi_test.go; see
 * docs/ADR/0010-core-shared-library.md.
 *
 *   host DATA_DIR PING_ITERATIONS ROUNDTRIP_ITERATIONS [--leak]
 *
 * --leak skips musubee_free on ping responses, so that the test can check
 * that its memory measurement detects a leak.
 */
#include <inttypes.h>
#include <stdarg.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

#include "musubee.h"

#ifdef _WIN32
#include <windows.h>
#include <psapi.h>
#elif defined(__APPLE__)
#include <mach/mach.h>
#include <time.h>
#else
#include <time.h>
#include <unistd.h>
#endif

#define PING_PAYLOAD_SIZE 1024
#define EVENT_TIMEOUT_MS 15000
#define WARMUP_ITERATIONS 1000

static uint64_t now_ns(void) {
#ifdef _WIN32
	LARGE_INTEGER counter, frequency;
	QueryPerformanceCounter(&counter);
	QueryPerformanceFrequency(&frequency);
	return (uint64_t)((double)counter.QuadPart * 1e9 / (double)frequency.QuadPart);
#else
	struct timespec ts;
	clock_gettime(CLOCK_MONOTONIC, &ts);
	return (uint64_t)ts.tv_sec * 1000000000u + (uint64_t)ts.tv_nsec;
#endif
}

/*
 * Memory of the process: private bytes on Windows (the working set can be
 * trimmed by the system at any time), resident set elsewhere.
 */
static uint64_t memory_bytes(void) {
#ifdef _WIN32
	PROCESS_MEMORY_COUNTERS_EX counters;
	if (!GetProcessMemoryInfo(GetCurrentProcess(), (PROCESS_MEMORY_COUNTERS *)&counters, sizeof(counters))) {
		return 0;
	}
	return counters.PrivateUsage;
#elif defined(__APPLE__)
	mach_task_basic_info_data_t info;
	mach_msg_type_number_t count = MACH_TASK_BASIC_INFO_COUNT;
	if (task_info(mach_task_self(), MACH_TASK_BASIC_INFO, (task_info_t)&info, &count) != KERN_SUCCESS) {
		return 0;
	}
	return info.resident_size;
#else
	unsigned long size, resident;
	FILE *f = fopen("/proc/self/statm", "r");
	if (f == NULL) {
		return 0;
	}
	int n = fscanf(f, "%lu %lu", &size, &resident);
	fclose(f);
	return n == 2 ? (uint64_t)resident * (uint64_t)sysconf(_SC_PAGESIZE) : 0;
#endif
}

static void fail(const char *format, ...) {
	va_list args;
	va_start(args, format);
	fputs("host: ", stderr);
	vfprintf(stderr, format, args);
	fputc('\n', stderr);
	va_end(args);
	exit(1);
}

/* Copies a buffer into a NUL-terminated string and releases the buffer. */
static char *take_string(musubee_buffer buffer) {
	char *s = malloc(buffer.len + 1);
	if (s == NULL) {
		fail("out of memory");
	}
	if (buffer.len > 0) {
		memcpy(s, buffer.data, buffer.len);
	}
	s[buffer.len] = '\0';
	musubee_free(buffer);
	return s;
}

static char *call(musubee_handle core, const char *request) {
	char *response = take_string(musubee_call(core, (const uint8_t *)request, strlen(request)));
	if (strstr(response, "\"error\"") != NULL) {
		fail("request %.80s failed: %s", request, response);
	}
	return response;
}

/* Reads an unsigned integer field such as "heap_alloc_bytes":123. */
static uint64_t number_field(const char *json, const char *name) {
	char key[64];
	snprintf(key, sizeof(key), "\"%s\":", name);
	const char *at = strstr(json, key);
	if (at == NULL) {
		fail("no %s in %s", name, json);
	}
	return strtoull(at + strlen(key), NULL, 10);
}

/* Reads a string field without escapes, such as "room_id":"!abc:x". */
static void string_field(const char *json, const char *name, char *out, size_t out_len) {
	char key[64];
	snprintf(key, sizeof(key), "\"%s\":\"", name);
	const char *start = strstr(json, key);
	if (start == NULL) {
		fail("no %s in %.200s", name, json);
	}
	start += strlen(key);
	const char *end = strchr(start, '"');
	if (end == NULL || (size_t)(end - start) >= out_len) {
		fail("invalid %s in %.200s", name, json);
	}
	memcpy(out, start, (size_t)(end - start));
	out[end - start] = '\0';
}

typedef struct {
	uint64_t heap_alloc;
	uint64_t goroutines;
} go_stats;

static go_stats stats(musubee_handle core) {
	char *response = call(core, "{\"id\":1,\"command\":\"stats\"}");
	go_stats s = {number_field(response, "heap_alloc_bytes"), number_field(response, "goroutines")};
	free(response);
	return s;
}

/* Sends ping requests with a 1 KiB payload; returns the time per call. */
static uint64_t ping_loop(musubee_handle core, long iterations, int leak) {
	static char request[PING_PAYLOAD_SIZE + 64];
	char payload[PING_PAYLOAD_SIZE + 1];
	memset(payload, 'x', PING_PAYLOAD_SIZE);
	payload[PING_PAYLOAD_SIZE] = '\0';
	int request_len = snprintf(request, sizeof(request), "{\"id\":2,\"command\":\"ping\",\"params\":{\"payload\":\"%s\"}}", payload);
	uint64_t start = now_ns();
	for (long i = 0; i < iterations; i++) {
		musubee_buffer response = musubee_call(core, (const uint8_t *)request, (size_t)request_len);
		if (response.len < PING_PAYLOAD_SIZE) {
			fail("short ping response");
		}
		if (!leak) {
			musubee_free(response);
		}
	}
	return iterations > 0 ? (now_ns() - start) / (uint64_t)iterations : 0;
}

/* Finds the room of a contact in the response of the login command. */
static void find_room(const char *login, const char *name, char *room_id, size_t room_id_len) {
	char needle[128];
	snprintf(needle, sizeof(needle), "\"name\":\"%s\"", name);
	const char *at = strstr(login, needle);
	if (at == NULL) {
		fail("no room named %s in %s", name, login);
	}
	/* Each room is {"room_id":"...","name":"..."}: go back to its start. */
	const char *start = at;
	while (start > login && *start != '{') {
		start--;
	}
	string_field(start, "room_id", room_id, room_id_len);
}

/*
 * Waits for the echo of a message: a message event that is not from the
 * user and contains marker.
 */
static void wait_echo(musubee_handle core, const char *marker) {
	uint64_t deadline = now_ns() + (uint64_t)EVENT_TIMEOUT_MS * 1000000u;
	while (now_ns() < deadline) {
		char *event = take_string(musubee_next_event(core, EVENT_TIMEOUT_MS));
		int found = strstr(event, "\"type\":\"message\"") != NULL &&
			strstr(event, "\"from_me\":false") != NULL &&
			strstr(event, marker) != NULL;
		if (strstr(event, "\"type\":\"closed\"") != NULL || strstr(event, "\"type\":\"overflow\"") != NULL) {
			fail("unexpected event while waiting for %s: %s", marker, event);
		}
		free(event);
		if (found) {
			return;
		}
	}
	fail("no echo of %s within %d ms", marker, EVENT_TIMEOUT_MS);
}

typedef struct {
	uint64_t mean_ns;
	uint64_t max_ns;
} latency;

/* Sends messages to the instant echo contact and waits for each echo. */
static latency roundtrip_loop(musubee_handle core, const char *room_id, long iterations) {
	latency result = {0, 0};
	uint64_t total = 0;
	for (long i = 0; i < iterations; i++) {
		char marker[32], request[256];
		snprintf(marker, sizeof(marker), "roundtrip-%ld", i);
		snprintf(request, sizeof(request), "{\"id\":3,\"command\":\"send\",\"params\":{\"room_id\":\"%s\",\"text\":\"%s\"}}", room_id, marker);
		uint64_t start = now_ns();
		free(call(core, request));
		wait_echo(core, marker);
		uint64_t elapsed = now_ns() - start;
		total += elapsed;
		if (elapsed > result.max_ns) {
			result.max_ns = elapsed;
		}
	}
	result.mean_ns = iterations > 0 ? total / (uint64_t)iterations : 0;
	return result;
}

/* Escapes backslashes for the data directory in the JSON configuration. */
static void json_path(const char *path, char *out, size_t out_len) {
	size_t j = 0;
	for (size_t i = 0; path[i] != '\0'; i++) {
		if (j + 3 >= out_len) {
			fail("path too long");
		}
		if (path[i] == '\\' || path[i] == '"') {
			out[j++] = '\\';
		}
		out[j++] = path[i];
	}
	out[j] = '\0';
}

int main(int argc, char **argv) {
	if (argc < 4) {
		fail("usage: host DATA_DIR PING_ITERATIONS ROUNDTRIP_ITERATIONS [--leak]");
	}
	long ping_iterations = strtol(argv[2], NULL, 10);
	long roundtrip_iterations = strtol(argv[3], NULL, 10);
	int leak = argc > 4 && strcmp(argv[4], "--leak") == 0;

	uint64_t memory_start = memory_bytes();
	char dir[1024], config[1200];
	json_path(argv[1], dir, sizeof(dir));
	snprintf(config, sizeof(config), "{\"data_dir\":\"%s\",\"log_level\":\"info\"}", dir);

	uint64_t start = now_ns();
	musubee_buffer error = {0};
	musubee_handle core = musubee_open((const uint8_t *)config, strlen(config), &error);
	if (core == 0) {
		fail("musubee_open: %s", take_string(error));
	}
	uint64_t open_ns = now_ns() - start;
	uint64_t memory_open = memory_bytes();

	/* Requests that fail must answer with an error, not crash. */
	char *response = take_string(musubee_call(core, (const uint8_t *)"{oops", 5));
	if (strstr(response, "\"error\"") == NULL) {
		fail("invalid JSON accepted: %s", response);
	}
	free(response);

	ping_loop(core, WARMUP_ITERATIONS, 0);
	go_stats stats_before = stats(core);
	uint64_t memory_before = memory_bytes();
	uint64_t ping_ns = ping_loop(core, ping_iterations, leak);
	go_stats stats_after_ping = stats(core);
	uint64_t memory_after_ping = memory_bytes();

	char *login = call(core, "{\"id\":4,\"command\":\"login\",\"params\":{\"username\":\"host\"}}");
	char room_id[256];
	find_room(login, "Instant Echo", room_id, sizeof(room_id));
	free(login);
	roundtrip_loop(core, room_id, 10); /* warm-up */
	uint64_t memory_before_roundtrip = memory_bytes();
	latency roundtrip = roundtrip_loop(core, room_id, roundtrip_iterations);
	go_stats stats_after_roundtrip = stats(core);
	uint64_t memory_after_roundtrip = memory_bytes();

	start = now_ns();
	musubee_close(core);
	uint64_t close_ns = now_ns() - start;

	/* A closed handle answers with an error and a closed event. */
	response = take_string(musubee_call(core, (const uint8_t *)"{}", 2));
	if (strstr(response, "invalid or closed handle") == NULL) {
		fail("call after close: %s", response);
	}
	free(response);
	response = take_string(musubee_next_event(core, 0));
	if (strcmp(response, "{\"type\":\"closed\"}") != 0) {
		fail("event after close: %s", response);
	}
	free(response);

	printf("{\"memory_start\":%" PRIu64 ",\"memory_open\":%" PRIu64 ",\"open_ns\":%" PRIu64 ",\"close_ns\":%" PRIu64
	       ",\"ping\":{\"iterations\":%ld,\"ns_per_call\":%" PRIu64 ",\"memory_before\":%" PRIu64 ",\"memory_after\":%" PRIu64
	       ",\"heap_before\":%" PRIu64 ",\"heap_after\":%" PRIu64 "}"
	       ",\"roundtrip\":{\"iterations\":%ld,\"mean_ns\":%" PRIu64 ",\"max_ns\":%" PRIu64 ",\"memory_before\":%" PRIu64
	       ",\"memory_after\":%" PRIu64 ",\"heap_after\":%" PRIu64 ",\"goroutines_after\":%" PRIu64 "}}\n",
	       memory_start, memory_open, open_ns, close_ns,
	       ping_iterations, ping_ns, memory_before, memory_after_ping, stats_before.heap_alloc, stats_after_ping.heap_alloc,
	       roundtrip_iterations, roundtrip.mean_ns, roundtrip.max_ns, memory_before_roundtrip, memory_after_roundtrip,
	       stats_after_roundtrip.heap_alloc, stats_after_roundtrip.goroutines);
	return 0;
}
