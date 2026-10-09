// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

// Command apigen generates the Go and TypeScript types of the core API from
// its schema. Run it through "go generate ./core/api"; it writes
// core/api/types.gen.go and ui/src/core-api/types.gen.ts.
package main

import (
	"fmt"
	"os"

	"github.com/quentinemusee/musubee/core/api/internal/codegen"
)

// Paths relative to core/api, where go generate runs.
const (
	schemaPath = "schema/core-api.schema.json"
	goPath     = "types.gen.go"
	tsPath     = "../../ui/src/core-api/types.gen.ts"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "apigen:", err)
		os.Exit(1)
	}
}

func run() error {
	data, err := os.ReadFile(schemaPath)
	if err != nil {
		return err
	}
	goCode, tsCode, err := codegen.Generate(data)
	if err != nil {
		return err
	}
	if err = os.WriteFile(goPath, goCode, 0o644); err != nil {
		return err
	}
	return os.WriteFile(tsPath, tsCode, 0o644)
}
