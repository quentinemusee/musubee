/*
 * SPDX-FileCopyrightText: 2026 Quentin Raimbaud
 * SPDX-License-Identifier: AGPL-3.0-or-later
 *
 * The control of the memory probe: a C program that does nothing but read
 * its own footprint, as core/memprobe does (task_info, TASK_VM_INFO), and
 * print it as a probe report with one step. Run on the simulator next to
 * the Go probe (run-simulator.sh), it gives the footprint of an empty
 * process, so that the difference is what the Go runtime and the core cost.
 */
#include <mach/mach.h>
#include <stddef.h>
#include <stdint.h>
#include <stdio.h>

#define FILLED(info, count, field) \
	((size_t)(count) * sizeof(natural_t) >= offsetof(task_vm_info_data_t, field) + sizeof((info).field))

int main(void) {
	task_vm_info_data_t info;
	mach_msg_type_number_t count = TASK_VM_INFO_COUNT;
	if (task_info(mach_task_self(), TASK_VM_INFO, (task_info_t)&info, &count) != KERN_SUCCESS) {
		fprintf(stderr, "task_info failed\n");
		return 1;
	}
	int64_t current = FILLED(info, count, phys_footprint) ? (int64_t)info.phys_footprint : -1;
	int64_t peak = FILLED(info, count, ledger_phys_footprint_peak) ? (int64_t)info.ledger_phys_footprint_peak : -1;
	int64_t remaining = FILLED(info, count, limit_bytes_remaining) ? (int64_t)info.limit_bytes_remaining : -1;
	printf("{\"goos\":\"none\",\"sqlite_driver\":\"none\",\"config\":{},\"steps\":[{\"step\":\"empty_c_process\","
	       "\"duration_ms\":0,\"footprint_bytes\":%lld,\"peak_footprint_bytes\":%lld,\"limit_remaining_bytes\":%lld,"
	       "\"go_mapped_bytes\":0,\"go_heap_objects_bytes\":0,\"go_stack_bytes\":0,\"goroutines\":0}]}\n",
	       (long long)current, (long long)peak, (long long)remaining);
	return 0;
}
