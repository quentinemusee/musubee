// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/google/licensecheck"
)

// minCoverage is the share of a license file that must match known license
// texts for the detection to be trusted.
const minCoverage = 75

// REUSE-IgnoreStart
var (
	licenseFileName = regexp.MustCompile(`(?i)^(licen[cs]e|copying|unlicense)([._-].*)?$`)
	spdxTag         = regexp.MustCompile(`SPDX-License-Identifier:\s*([^\r\n*]+)`)
)

// REUSE-IgnoreEnd

// LicenseFiles returns the license files at the top of a directory.
func LicenseFiles(dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var files []string
	for _, e := range entries {
		if !e.IsDir() && licenseFileName.MatchString(e.Name()) {
			files = append(files, filepath.Join(dir, e.Name()))
		}
	}
	sort.Strings(files)
	return files
}

// DetectLicenses classifies the license files of a directory and returns
// the license identifiers found (all of them apply), or nil when nothing is
// recognized with enough confidence.
func DetectLicenses(dir string) []string {
	seen := map[string]bool{}
	for _, file := range LicenseFiles(dir) {
		data, err := os.ReadFile(file)
		if err != nil {
			continue
		}
		for _, id := range classify(data) {
			seen[id] = true
		}
	}
	ids := make([]string, 0, len(seen))
	for id := range seen {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

// classify returns the license identifiers of one license file: from an
// SPDX-License-Identifier line when present, else from the license text.
// Note that a GNU license text alone does not say "only" or "or later" (that
// is written in source headers), so licensecheck reports "GPL-2.0" and the
// policy, which only accepts "GPL-2.0-or-later", rejects it.
func classify(text []byte) []string {
	if m := spdxTag.FindSubmatch(text); m != nil {
		return IDs(strings.TrimSpace(string(m[1])))
	}
	coverage := licensecheck.Scan(text)
	if coverage.Percent < minCoverage {
		return nil
	}
	var ids []string
	for _, m := range coverage.Match {
		if !m.IsURL {
			ids = append(ids, m.ID)
		}
	}
	return ids
}
