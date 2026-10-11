// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

// Musubee: this file is not part of mautrix-signal (see MUSUBEE.md at the root
// of this copy). It chooses the Signal service that signalmeow talks to,
// production or staging.

package signalmeow

import (
	_ "embed"
	"encoding/hex"
	"fmt"

	"go.mau.fi/mautrix-signal/pkg/libsignalgo"
	"go.mau.fi/mautrix-signal/pkg/signalmeow/web"
)

// Environment names a Signal service.
type Environment string

const (
	// EnvironmentProduction is Signal's public service, the default.
	EnvironmentProduction Environment = "production"
	// EnvironmentStaging is Signal's staging service, where accounts are
	// separate from production ones. Musubee's tests use it.
	EnvironmentStaging Environment = "staging"
)

// staging-server-public-params.dat holds the zkgroup parameters of the staging
// service: ZKGROUP_SERVER_PUBLIC_PARAMS of the staging flavor in
// Signal-Android's app/build.gradle.kts, the same value as in signal-cli's
// StagingConfig.java (v0.14.9).
//
//go:embed staging-server-public-params.dat
var stagingServerPublicParamsSlice []byte

type environmentConfig struct {
	apiHostname     string
	storageHostname string
	cdnHostnames    [3]string
	grpcTarget      string
	cdsiServer      string
	// cdsiMrenclave must be one of the enclaves that libsignal's attestation
	// accepts (rust/attest/src/constants.rs in the libsignal version in use).
	cdsiMrenclave      string
	serverPublicParams []byte
}

var environments = map[Environment]environmentConfig{
	EnvironmentProduction: {
		apiHostname:        "chat.signal.org",
		storageHostname:    "storage.signal.org",
		cdnHostnames:       [3]string{"cdn.signal.org", "cdn2.signal.org", "cdn3.signal.org"},
		grpcTarget:         "dns://grpc.chat.signal.org:443",
		cdsiServer:         ProdContactDiscoveryServer,
		cdsiMrenclave:      ProdContactDiscoveryMrenclave,
		serverPublicParams: prodServerPublicParamsSlice,
	},
	EnvironmentStaging: {
		apiHostname:     "chat.staging.signal.org",
		storageHostname: "storage-staging.signal.org",
		cdnHostnames:    [3]string{"cdn-staging.signal.org", "cdn2-staging.signal.org", "cdn3-staging.signal.org"},
		// libsignal's DOMAIN_CONFIG_CHAT_STAGING (rust/net/src/env.rs).
		grpcTarget: "dns://grpc.chat.staging.signal.org:443",
		cdsiServer: "cdsi.staging.signal.org",
		// ENCLAVE_ID_CDSI_STAGING in libsignal v0.102.2.
		cdsiMrenclave:      "6d9b9649fa3a337754a98059c66d48ac77aaca5299d3b27d6ed1e646c7c81c0a",
		serverPublicParams: stagingServerPublicParamsSlice,
	},
}

var currentEnvironment = EnvironmentProduction

// CurrentEnvironment returns the Signal service in use.
func CurrentEnvironment() Environment {
	return currentEnvironment
}

// SetEnvironment points signalmeow at a Signal service. The choice applies to
// the whole process: call it once, before creating any Client or starting any
// provisioning, and never while one runs.
func SetEnvironment(env Environment) error {
	config, ok := environments[env]
	if !ok {
		return fmt.Errorf("unknown Signal environment %q", env)
	}
	params, err := libsignalgo.DeserializeServerPublicParams(config.serverPublicParams)
	if err != nil {
		return fmt.Errorf("failed to parse the zkgroup parameters of %s: %w", env, err)
	}
	mrenclave, err := hex.DecodeString(config.cdsiMrenclave)
	if err != nil {
		return fmt.Errorf("failed to parse the contact discovery enclave of %s: %w", env, err)
	}

	web.APIHostname = config.apiHostname
	web.StorageHostname = config.storageHostname
	web.CDN1Hostname = config.cdnHostnames[0]
	web.CDN2Hostname = config.cdnHostnames[1]
	web.CDN3Hostname = config.cdnHostnames[2]
	// The same layout as web.CDNHosts upstream: CDN number 0 and 1 are both
	// served by the first host.
	web.CDNHosts = []string{config.cdnHostnames[0], config.cdnHostnames[0], config.cdnHostnames[1], config.cdnHostnames[2]}
	web.GRPCTarget = config.grpcTarget
	contactDiscoveryServer = config.cdsiServer
	contactDiscoveryMrenclave = config.cdsiMrenclave
	prodContactDiscoveryMrenclaveBytes = mrenclave
	prodServerPublicParams = params
	currentEnvironment = env
	return nil
}
