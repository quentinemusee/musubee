// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

// Policy lists the licenses accepted for dependencies and the justified
// exceptions. It is read from scripts/license-policy.json.
type Policy struct {
	// Allowed holds SPDX identifiers compatible with AGPL-3.0-or-later.
	Allowed []string `json:"allowed"`
	// Exceptions accept specific packages that the rules alone would reject
	// or cannot classify. Each one must give a reason.
	Exceptions []Exception `json:"exceptions"`

	allowed map[string]bool
}

// Exception accepts one package (or a family, with a trailing "*").
type Exception struct {
	Ecosystem string `json:"ecosystem"` // "go" or "npm"
	Package   string `json:"package"`   // exact name, or prefix ending with "*"
	License   string `json:"license"`   // what we know about its license
	Reason    string `json:"reason"`    // why it is acceptable
}

// LoadPolicy reads and validates a policy file.
func LoadPolicy(path string) (*Policy, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var p Policy
	if err := json.Unmarshal(data, &p); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if err := p.init(); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return &p, nil
}

func (p *Policy) init() error {
	p.allowed = map[string]bool{}
	for _, id := range p.Allowed {
		p.allowed[NormalizeID(id)] = true
	}
	for i, e := range p.Exceptions {
		if e.Ecosystem != "go" && e.Ecosystem != "npm" {
			return fmt.Errorf("exception %d: ecosystem must be \"go\" or \"npm\"", i)
		}
		if e.Package == "" || strings.TrimSpace(e.Reason) == "" || strings.TrimSpace(e.License) == "" {
			return fmt.Errorf("exception %d (%s): package, license and reason are required", i, e.Package)
		}
	}
	return nil
}

// IsAllowed reports whether a single license identifier is accepted.
func (p *Policy) IsAllowed(id string) bool {
	return p.allowed[NormalizeID(id)]
}

// ExceptionFor returns the exception covering a package, if any.
func (p *Policy) ExceptionFor(ecosystem, pkg string) (Exception, bool) {
	for _, e := range p.Exceptions {
		if e.Ecosystem != ecosystem {
			continue
		}
		if strings.HasSuffix(e.Package, "*") {
			if strings.HasPrefix(pkg, strings.TrimSuffix(e.Package, "*")) {
				return e, true
			}
		} else if e.Package == pkg {
			return e, true
		}
	}
	return Exception{}, false
}
