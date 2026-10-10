// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

package memprobe

import (
	"bufio"
	"os"
	"strconv"
	"strings"
)

// OSFootprint approximates the footprint on Linux (and Android) with the
// resident set size of /proc/self/status: VmRSS now and VmHWM at its peak.
// Unlike Apple's phys_footprint, it counts shared and file-backed pages and
// not swapped ones, so it only gives an order of magnitude. There is no
// limit to report: limitRemaining is -1.
func OSFootprint() (current, peak, limitRemaining int64) {
	current, peak, limitRemaining = -1, -1, -1
	f, err := os.Open("/proc/self/status")
	if err != nil {
		return
	}
	defer func() { _ = f.Close() }()
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		key, value, ok := strings.Cut(scanner.Text(), ":")
		if !ok || (key != "VmRSS" && key != "VmHWM") {
			continue
		}
		kib, err := strconv.ParseInt(strings.TrimSuffix(strings.TrimSpace(value), " kB"), 10, 64)
		if err != nil {
			continue
		}
		if key == "VmRSS" {
			current = kib << 10
		} else {
			peak = kib << 10
		}
	}
	return
}
