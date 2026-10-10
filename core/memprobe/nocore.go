// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build memprobe_nocore

package memprobe

import "errors"

// Built with memprobe_nocore: the core is not linked in (see core.go).

const sqliteDriver = "none"

func (p *prober) coreSteps(Config) bool {
	return p.step("core_open", func() error {
		return errors.New("the core is not built in (memprobe_nocore)")
	})
}
