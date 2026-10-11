// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

// Musubee: this file is not part of mautrix-signal (see MUSUBEE.md at the root
// of this copy).

package signalmeow

import (
	"testing"

	"go.mau.fi/mautrix-signal/pkg/signalmeow/web"
)

func TestSetEnvironment(t *testing.T) {
	t.Cleanup(func() {
		if err := SetEnvironment(EnvironmentProduction); err != nil {
			t.Fatal(err)
		}
	})
	if err := SetEnvironment(EnvironmentStaging); err != nil {
		t.Fatal(err)
	}
	if CurrentEnvironment() != EnvironmentStaging || contactDiscoveryServer != "cdsi.staging.signal.org" ||
		web.APIHostname != "chat.staging.signal.org" || web.GRPCTarget != "dns://grpc.chat.staging.signal.org:443" {
		t.Errorf("staging not applied: %s, %s, %s, %s", CurrentEnvironment(), contactDiscoveryServer, web.APIHostname, web.GRPCTarget)
	}
	if err := SetEnvironment(EnvironmentProduction); err != nil {
		t.Fatal(err)
	}
	if contactDiscoveryServer != ProdContactDiscoveryServer || contactDiscoveryMrenclave != ProdContactDiscoveryMrenclave ||
		web.APIHostname != "chat.signal.org" || web.CDNHosts[3] != "cdn3.signal.org" {
		t.Errorf("production not restored: %s, %s", contactDiscoveryServer, web.APIHostname)
	}
	if err := SetEnvironment("elsewhere"); err == nil {
		t.Error("an unknown environment was accepted")
	}
}
