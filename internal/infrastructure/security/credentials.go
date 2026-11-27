// Package security adapts NSGo's secure credential store to the application.
package security

import (
	"github.com/nsgo-dev/nsgo/core"

	"chandao/zentaotray/internal/application"
)

// SecretsCredentialStore delegates credential operations to NSGo. Callers
// should supply cx.Secrets(), whose default service is the NSGo app ID.
// There is intentionally no plaintext fallback when secure storage is unavailable.
type SecretsCredentialStore struct {
	Secrets core.Secrets
}

var _ application.CredentialStore = SecretsCredentialStore{}

// Get returns the secure value for account, if one exists.
func (s SecretsCredentialStore) Get(account string) (string, bool, error) {
	return s.Secrets.Get(account)
}

// Set securely stores value for account.
func (s SecretsCredentialStore) Set(account, value string) error {
	return s.Secrets.Set(account, value)
}

// Delete removes the secure value for account.
func (s SecretsCredentialStore) Delete(account string) error {
	return s.Secrets.Delete(account)
}
