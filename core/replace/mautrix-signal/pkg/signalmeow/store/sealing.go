// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

// Musubee: this file is not part of mautrix-signal (see MUSUBEE.md at the root
// of this copy). It seals the secrets that the store keeps in the database:
// the account's keys and password, the Signal protocol's sessions and keys,
// the contacts' profile keys, the groups' master keys, and message content
// (the event buffer and the backup tables).

package store

import (
	"bytes"
	"database/sql"
	"errors"
	"fmt"

	"go.mau.fi/util/dbutil"
)

// Sealer encrypts and authenticates the store's secret values. The purpose
// names the column ("table.column"): a value sealed for one column must not
// open for another. Seal never returns nil for a non-nil value, and its
// output must be valid text, since some sealed columns are TEXT.
type Sealer interface {
	Seal(purpose string, plain []byte) []byte
	Open(purpose string, sealed []byte) ([]byte, error)
}

// The sealed columns, as "table.column".
const (
	PurposeDeviceACIIdentityKeyPair = "signalmeow_device.aci_identity_key_pair"
	PurposeDevicePNIIdentityKeyPair = "signalmeow_device.pni_identity_key_pair"
	PurposeDevicePassword           = "signalmeow_device.password"
	PurposeDeviceMasterKey          = "signalmeow_device.master_key"
	PurposeDeviceAccountRecord      = "signalmeow_device.account_record"
	PurposeDeviceAccountEntropyPool = "signalmeow_device.account_entropy_pool"
	PurposeDeviceEphemeralBackupKey = "signalmeow_device.ephemeral_backup_key"
	PurposeDeviceMediaRootBackupKey = "signalmeow_device.media_root_backup_key"
	PurposeSessionRecord            = "signalmeow_sessions.record"
	PurposePreKey                   = "signalmeow_pre_keys.key_pair"
	PurposeKyberPreKey              = "signalmeow_kyber_pre_keys.key_pair"
	PurposeSenderKey                = "signalmeow_sender_keys.key_record"
	PurposeProfileKey               = "signalmeow_recipients.profile_key"
	PurposeGroupMasterKey           = "signalmeow_groups.master_key"
	PurposeEventPlaintext           = "signalmeow_event_buffer.plaintext"
	PurposeBackupRecipient          = "signalmeow_backup_recipient.data"
	PurposeBackupGroupMasterKey     = "signalmeow_backup_recipient.group_master_key"
	PurposeBackupChat               = "signalmeow_backup_chat.data"
	PurposeBackupMessage            = "signalmeow_backup_message.data"
)

// Purposes lists every sealed column.
var Purposes = []string{
	PurposeDeviceACIIdentityKeyPair,
	PurposeDevicePNIIdentityKeyPair,
	PurposeDevicePassword,
	PurposeDeviceMasterKey,
	PurposeDeviceAccountRecord,
	PurposeDeviceAccountEntropyPool,
	PurposeDeviceEphemeralBackupKey,
	PurposeDeviceMediaRootBackupKey,
	PurposeSessionRecord,
	PurposePreKey,
	PurposeKyberPreKey,
	PurposeSenderKey,
	PurposeProfileKey,
	PurposeGroupMasterKey,
	PurposeEventPlaintext,
	PurposeBackupRecipient,
	PurposeBackupGroupMasterKey,
	PurposeBackupChat,
	PurposeBackupMessage,
}

// SetSealer makes the store seal its secret values with s. Call it once,
// before the store is used: the sealer is read without a lock. With a
// sealer, a stored value that does not open (written in clear, or with
// another key) is an error, never read as it is. Without one (the default),
// the store keeps upstream's behaviour.
func (c *Container) SetSealer(s Sealer) {
	c.sealer = s
}

// sealBytes returns what to write for a secret value: nil stays NULL.
func (c *Container) sealBytes(purpose string, plain []byte) []byte {
	if c.sealer == nil || plain == nil {
		return plain
	}
	return c.sealer.Seal(purpose, plain)
}

// sealString returns what to write for a secret TEXT value.
func (c *Container) sealString(purpose, plain string) string {
	if c.sealer == nil {
		return plain
	}
	return string(c.sealer.Seal(purpose, []byte(plain)))
}

// opened returns a scan destination that opens a sealed value into dst.
func (c *Container) opened(purpose string, dst *[]byte) openedBytes {
	return openedBytes{c.sealer, purpose, dst}
}

// openedString returns a scan destination that opens a sealed TEXT value
// into dst; NULL leaves dst invalid.
func (c *Container) openedString(purpose string, dst *sql.NullString) openedString {
	return openedString{openedBytes{c.sealer, purpose, nil}, dst}
}

type openedBytes struct {
	sealer  Sealer
	purpose string
	dst     *[]byte
}

var _ sql.Scanner = openedBytes{}

func (o openedBytes) Scan(src any) error {
	var raw []byte
	switch v := src.(type) {
	case nil:
		*o.dst = nil
		return nil
	case []byte:
		// The driver may reuse its buffer after Scan returns.
		raw = bytes.Clone(v)
	case string:
		raw = []byte(v)
	default:
		return fmt.Errorf("unexpected type %T for %s", src, o.purpose)
	}
	if o.sealer == nil {
		*o.dst = raw
		return nil
	}
	plain, err := o.sealer.Open(o.purpose, raw)
	if err != nil {
		return fmt.Errorf("failed to open %s: %w", o.purpose, err)
	}
	*o.dst = plain
	return nil
}

type openedString struct {
	bytes openedBytes
	dst   *sql.NullString
}

var _ sql.Scanner = openedString{}

func (o openedString) Scan(src any) error {
	var raw []byte
	b := o.bytes
	b.dst = &raw
	if err := b.Scan(src); err != nil {
		return err
	}
	if src == nil {
		*o.dst = sql.NullString{}
		return nil
	}
	*o.dst = sql.NullString{String: string(raw), Valid: true}
	return nil
}

// scanSealedRecord is scanRecord (prekey_store.go) for a sealed column.
func scanSealedRecord[T any](c *Container, row dbutil.Scannable, purpose string, deserializer func([]byte) (*T, error)) (*T, error) {
	var record []byte
	err := row.Scan(c.opened(purpose, &record))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	return deserializer(record)
}
