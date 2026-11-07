// Package vault provides the HashiCorp Vault SecretProvider implementation.
package vault

import (
	"github.com/warpcode/cloakenv/internal/provider"
)

// VaultProvider aliases provider.VaultProvider.
type VaultProvider = provider.VaultProvider

// NewVaultProvider constructs a new VaultProvider.
func NewVaultProvider() *VaultProvider {
	return provider.NewVaultProvider()
}
