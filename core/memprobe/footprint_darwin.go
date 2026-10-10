// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build darwin && cgo

package memprobe

/*
#include <stddef.h>
#include <stdint.h>
#include <mach/mach.h>

// The fields of task_vm_info grew over the revisions of the structure; the
// kernel fills as many as count allows and returns the count it filled. A
// field is valid only if the returned count covers it, so each one is
// checked against its offset. -1 marks a field the kernel did not fill.
static int musubee_footprint(int64_t *current, int64_t *peak, int64_t *remaining) {
	task_vm_info_data_t info;
	mach_msg_type_number_t count = TASK_VM_INFO_COUNT;
	if (task_info(mach_task_self(), TASK_VM_INFO, (task_info_t)&info, &count) != KERN_SUCCESS) {
		return -1;
	}
	size_t filled = (size_t)count * sizeof(natural_t);
	*current = filled >= offsetof(task_vm_info_data_t, phys_footprint) + sizeof(info.phys_footprint)
		? (int64_t)info.phys_footprint : -1;
	*peak = filled >= offsetof(task_vm_info_data_t, ledger_phys_footprint_peak) + sizeof(info.ledger_phys_footprint_peak)
		? (int64_t)info.ledger_phys_footprint_peak : -1;
	*remaining = filled >= offsetof(task_vm_info_data_t, limit_bytes_remaining) + sizeof(info.limit_bytes_remaining)
		? (int64_t)info.limit_bytes_remaining : -1;
	return 0;
}
*/
import "C"

// OSFootprint reads the task's VM information from the Mach kernel (macOS
// and iOS): phys_footprint, the figure jetsam compares with the process's
// memory limit (what Xcode's memory gauge shows), its peak over the life of
// the process, and limit_bytes_remaining, the bytes left before the limit
// (0 without a limit).
func OSFootprint() (current, peak, limitRemaining int64) {
	var cur, pk, rem C.int64_t
	if C.musubee_footprint(&cur, &pk, &rem) != 0 {
		return -1, -1, -1
	}
	return int64(cur), int64(pk), int64(rem)
}
