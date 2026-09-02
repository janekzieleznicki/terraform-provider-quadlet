// Package scope defines system and user systemd scopes.
package scope

import (
	"context"
	"fmt"
	"path"

	"github.com/janekzieleznicki/terraform-provider-quadlet/internal/transport"
)

// Scope represents either system or user scope for systemd units.
type Scope string

// Scope constants define the two systemd scopes.
const (
	ScopeSystem Scope = "system"
	ScopeUser   Scope = "user"
)

// SystemUnitDir is upstream quadlet's UnitDirAdmin.
const SystemUnitDir = "/etc/containers/systemd"

// Parse parses a scope string into a Scope constant.
func Parse(s string) (Scope, error) {
	switch Scope(s) {
	case ScopeSystem, ScopeUser:
		return Scope(s), nil
	default:
		return "", fmt.Errorf("invalid scope %q; must be %q or %q", s, ScopeSystem, ScopeUser)
	}
}

// Valid returns true if the scope is a valid constant.
func (s Scope) Valid() bool {
	return s == ScopeSystem || s == ScopeUser
}

// UnitDir returns the directory where unit files are installed, mirroring upstream
// GetInstallUnitDirPath: SystemUnitDir for system, and
// <UserConfigDir>/containers/systemd for user.
func UnitDir(ctx context.Context, t transport.Transport, s Scope) (string, error) {
	switch s {
	case ScopeSystem:
		return SystemUnitDir, nil
	case ScopeUser:
		configDir, err := t.UserConfigDir(ctx)
		if err != nil {
			return "", fmt.Errorf("user unit dir: %w", err)
		}
		return path.Join(configDir, "containers", "systemd"), nil
	default:
		return "", fmt.Errorf("unknown scope %q", s)
	}
}

// SystemctlArgs returns the scope-selecting flags for systemctl:
// {"--user"} for user, nil for system.
func SystemctlArgs(s Scope) []string {
	if s == ScopeUser {
		return []string{"--user"}
	}
	return nil
}

// QuadletFlags returns the scope-selecting flags for the quadlet generator:
// {"-user"} for user, nil for system. Note the single dash.
func QuadletFlags(s Scope) []string {
	if s == ScopeUser {
		return []string{"-user"}
	}
	return nil
}
