// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build cgo

// Command memprobe measures the memory of the core and of goolm step by step
// (package memprobe). It is built two ways:
//
// As a program, it runs the probe and prints the JSON report:
//
//	go run ./core/cmd/memprobe -data-dir /tmp/probe -core -crypto
//
// As a C static library for iOS (apps/mobile/ios-probe/build-go-xcframework.sh),
// it exports musubee_memprobe_run (memprobe.h), which the test app of
// apps/mobile/ios-probe calls from its Notification Service Extension.
//
// The package needs cgo: the iOS library requires it, and so does the
// footprint on macOS.
package main

/*
#include <stdlib.h>
*/
import "C"

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"github.com/quentinemusee/musubee/core/memprobe"
)

// run decodes a JSON configuration, runs the probe and returns the JSON
// report. An invalid configuration gives a report with only the error.
func run(config []byte) []byte {
	var cfg memprobe.Config
	var report memprobe.Report
	if err := json.Unmarshal(config, &cfg); err != nil {
		report.Error = fmt.Sprintf("invalid configuration: %v", err)
	} else {
		report = memprobe.Run(cfg, memprobe.OSFootprint)
	}
	out, err := json.Marshal(report)
	if err != nil {
		return []byte(`{"error":"cannot encode the report"}`)
	}
	return out
}

// musubee_memprobe_run runs the probe with a JSON configuration (memprobe.Config)
// and returns the JSON report as a NUL-terminated string, which the caller
// releases with free.
//
//export musubee_memprobe_run
func musubee_memprobe_run(config *C.char) *C.char {
	report := run([]byte(C.GoString(config)))
	return C.CString(string(report))
}

func main() {
	var cfg memprobe.Config
	flag.StringVar(&cfg.DataDir, "data-dir", "", "directory for the core's data (emptied first)")
	flag.BoolVar(&cfg.Core, "core", false, "run the core steps")
	flag.BoolVar(&cfg.Crypto, "crypto", false, "run the goolm steps")
	flag.IntVar(&cfg.Messages, "messages", 0, "messages per step (default 20)")
	flag.IntVar(&cfg.MemoryLimitMB, "memory-limit-mb", 0, "Go soft memory limit in MiB (0: unchanged)")
	flag.IntVar(&cfg.GCPercent, "gc-percent", 0, "Go GC percent (0: unchanged)")
	flag.Parse()
	report := memprobe.Run(cfg, memprobe.OSFootprint)
	out, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Println(string(out))
	if report.Error != "" {
		os.Exit(1)
	}
}
