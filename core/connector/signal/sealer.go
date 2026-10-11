// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build musubee_signal

package signal

import (
	"sync"

	"go.mau.fi/mautrix-signal/pkg/signalmeow/store"

	"github.com/quentinemusee/musubee/core/secrets"
)

// purposePrefix starts the purpose of every key that seals a column of
// signalmeow's store: "signal/" and the column (docs/ADR/0026).
const purposePrefix = "signal/"

// storeSealer seals signalmeow's secret columns (our copy of
// mautrix-signal, store/sealing.go) with AES-256-GCM, one derived key per
// column.
type storeSealer struct {
	key     secrets.Key
	mu      sync.Mutex
	sealers map[string]*secrets.Sealer
}

var _ store.Sealer = (*storeSealer)(nil)

func newStoreSealer(key secrets.Key) *storeSealer {
	return &storeSealer{key: key, sealers: make(map[string]*secrets.Sealer)}
}

func (s *storeSealer) sealer(purpose string) *secrets.Sealer {
	s.mu.Lock()
	defer s.mu.Unlock()
	sealer, ok := s.sealers[purpose]
	if !ok {
		sealer = secrets.NewSealer(s.key, purposePrefix+purpose)
		s.sealers[purpose] = sealer
	}
	return sealer
}

// Seal returns the sealed value as text ("musubee-sealed:v1:" and base64),
// which also fits the store's TEXT columns.
func (s *storeSealer) Seal(purpose string, plain []byte) []byte {
	return []byte(s.sealer(purpose).Seal(plain))
}

func (s *storeSealer) Open(purpose string, sealed []byte) ([]byte, error) {
	plain, err := s.sealer(purpose).Open(string(sealed))
	if err != nil {
		return nil, err
	}
	if plain == nil {
		// An empty value stays empty, not NULL: the event buffer tells a
		// handled event by its NULL plaintext.
		plain = []byte{}
	}
	return plain, nil
}
