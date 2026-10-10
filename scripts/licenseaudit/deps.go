// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Dependency is one third-party package found in the repository.
type Dependency struct {
	Ecosystem string // "go" or "npm"
	Name      string
	Version   string
	// License is an SPDX expression: declared by the package (npm) or
	// detected from its license files (Go, or npm without a declaration).
	License string
	// Source explains where the license comes from.
	Source string
	// From is the manifest that brings the dependency in.
	From string
}

// GoTagSets are the sets of build tags whose dependencies are audited: the
// core's pure-Go Olm (goolm, docs/ADR/0018), which every build has; test-only
// code (integration, telegram); and the SQLite driver of Android builds
// (musubee_cgo_sqlite, see core/storage/sqlite), which replaces the default
// one.
var GoTagSets = []string{"goolm,integration,telegram", "goolm,integration,telegram,musubee_cgo_sqlite"}

// RepositoryLicense is the license of the code of this repository (REUSE
// checks that every file says so, see scripts/license_check.py).
const RepositoryLicense = "AGPL-3.0-or-later"

// GoDependencies lists the modules compiled into the packages and tests of
// each Go module, with the licenses detected in the module cache. A module
// replaced by a directory of the repository (root) is the repository's code.
func GoDependencies(root string, moduleDirs []string) ([]Dependency, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	type key struct{ path, version string }
	seen := map[key]Dependency{}
	for _, dir := range moduleDirs {
		var out []byte
		for _, tags := range GoTagSets {
			cmd := exec.Command("go", "list", "-deps", "-test", "-tags="+tags,
				"-f", "{{with .Module}}{{if not .Main}}{{.Path}}\t{{.Version}}\t{{.Dir}}{{end}}{{end}}", "./...")
			cmd.Dir = dir
			var stderr bytes.Buffer
			cmd.Stderr = &stderr
			tagged, err := cmd.Output()
			if err != nil {
				return nil, fmt.Errorf("go list -tags=%s in %s: %w: %s", tags, dir, err, strings.TrimSpace(stderr.String()))
			}
			out = append(out, tagged...)
		}
		scanner := bufio.NewScanner(bytes.NewReader(out))
		for scanner.Scan() {
			fields := strings.Split(scanner.Text(), "\t")
			if len(fields) != 3 || fields[0] == "" {
				continue
			}
			k := key{fields[0], fields[1]}
			if _, ok := seen[k]; ok {
				continue
			}
			seen[k] = goDependency(root, fields[0], fields[1], fields[2], filepath.Join(dir, "go.mod"))
		}
	}
	deps := make([]Dependency, 0, len(seen))
	for _, d := range seen {
		deps = append(deps, d)
	}
	sortDeps(deps)
	return deps, nil
}

// goDependency returns a Go module with its license: the repository's when
// the module's directory is in the repository, else from its license files.
func goDependency(root, path, version, moduleDir, from string) Dependency {
	dep := Dependency{Ecosystem: "go", Name: path, Version: version, From: from}
	if rel, err := filepath.Rel(root, moduleDir); err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		dep.License = RepositoryLicense
		dep.Source = "replaced by " + filepath.ToSlash(rel) + " in this repository"
		return dep
	}
	if ids := DetectLicenses(moduleDir); len(ids) > 0 {
		dep.License = strings.Join(ids, " AND ")
		dep.Source = "license file"
	} else {
		dep.Source = "no recognizable license file"
	}
	return dep
}

type lockFile struct {
	LockfileVersion int                  `json:"lockfileVersion"`
	Packages        map[string]lockEntry `json:"packages"`
}

type lockEntry struct {
	Name    string          `json:"name"`
	Version string          `json:"version"`
	License json.RawMessage `json:"license"`
	Link    bool            `json:"link"`
}

// NPMRegistry is the registry queried for packages that are not installed
// on this platform (optional, platform-specific packages), whose license
// appears nowhere locally. Tests replace it.
var NPMRegistry = "https://registry.npmjs.org"

var registryClient = &http.Client{Timeout: 20 * time.Second}

// registryLicense returns the license that the npm registry declares for
// one version of a package.
func registryLicense(name, version string) (string, error) {
	endpoint := NPMRegistry + "/" + url.PathEscape(name) + "/" + url.PathEscape(version)
	resp, err := registryClient.Get(endpoint)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("registry answered HTTP %d for %s@%s", resp.StatusCode, name, version)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return "", err
	}
	var manifest struct {
		License  json.RawMessage `json:"license"`
		Licenses json.RawMessage `json:"licenses"`
	}
	if err := json.Unmarshal(body, &manifest); err != nil {
		return "", err
	}
	if v := licenseValue(manifest.License); v != "" {
		return v, nil
	}
	return licenseValue(manifest.Licenses), nil
}

// NPMDependencies lists the packages of a package-lock.json (lockfile v2 or
// v3). The declared license comes from the lock file, else from the
// installed package.json, else from its license files; for packages not
// installed on this platform, from the npm registry.
func NPMDependencies(lockPath string) ([]Dependency, error) {
	data, err := os.ReadFile(lockPath)
	if err != nil {
		return nil, err
	}
	var lock lockFile
	if err := json.Unmarshal(data, &lock); err != nil {
		return nil, fmt.Errorf("%s: %w", lockPath, err)
	}
	if lock.LockfileVersion < 2 {
		return nil, fmt.Errorf("%s: lockfile version %d is not supported (need 2 or more)", lockPath, lock.LockfileVersion)
	}
	base := filepath.Dir(lockPath)
	var deps []Dependency
	for key, entry := range lock.Packages {
		if key == "" || entry.Link {
			continue
		}
		name := entry.Name
		if i := strings.LastIndex(key, "node_modules/"); name == "" && i >= 0 {
			name = key[i+len("node_modules/"):]
		}
		dep := Dependency{Ecosystem: "npm", Name: name, Version: entry.Version, From: lockPath}
		dir := filepath.Join(base, filepath.FromSlash(key))
		switch {
		case licenseValue(entry.License) != "":
			dep.License, dep.Source = licenseValue(entry.License), "package-lock.json"
		case readPackageLicense(dir) != "":
			dep.License, dep.Source = readPackageLicense(dir), "installed package.json"
		default:
			if ids := DetectLicenses(dir); len(ids) > 0 {
				dep.License, dep.Source = strings.Join(ids, " AND "), "license file"
			} else if _, err := os.Stat(filepath.Join(dir, "package.json")); err == nil {
				// Installed (an empty directory, as old npm versions leave
				// for skipped optional packages, does not count).
				dep.Source = "no license declared or detected"
			} else if license, err := registryLicense(name, entry.Version); err != nil {
				dep.Source = "not installed here, and the npm registry could not be read: " + err.Error()
			} else if license != "" {
				dep.License, dep.Source = license, "npm registry (not installed on this platform)"
			} else {
				dep.Source = "not installed here, and the npm registry declares no license"
			}
		}
		deps = append(deps, dep)
	}
	sortDeps(deps)
	return deps, nil
}

// licenseValue reads the "license" field, which is a string, or (in old
// packages) an object or a list of objects with a "type".
func licenseValue(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return strings.TrimSpace(s)
	}
	var obj struct {
		Type string `json:"type"`
	}
	if json.Unmarshal(raw, &obj) == nil && obj.Type != "" {
		return obj.Type
	}
	var list []struct {
		Type string `json:"type"`
	}
	if json.Unmarshal(raw, &list) == nil {
		var types []string
		for _, l := range list {
			if l.Type != "" {
				types = append(types, l.Type)
			}
		}
		if len(types) > 1 {
			return "(" + strings.Join(types, " OR ") + ")"
		}
		return strings.Join(types, "")
	}
	return ""
}

func readPackageLicense(dir string) string {
	data, err := os.ReadFile(filepath.Join(dir, "package.json"))
	if err != nil {
		return ""
	}
	var pkg struct {
		License  json.RawMessage `json:"license"`
		Licenses json.RawMessage `json:"licenses"`
	}
	if json.Unmarshal(data, &pkg) != nil {
		return ""
	}
	if v := licenseValue(pkg.License); v != "" {
		return v
	}
	return licenseValue(pkg.Licenses)
}

func sortDeps(deps []Dependency) {
	sort.Slice(deps, func(i, j int) bool {
		if deps[i].Name != deps[j].Name {
			return deps[i].Name < deps[j].Name
		}
		return deps[i].Version < deps[j].Version
	})
}
