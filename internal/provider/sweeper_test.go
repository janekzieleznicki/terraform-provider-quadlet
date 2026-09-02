package provider

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"

	"github.com/janekzieleznicki/terraform-provider-quadlet/internal/scope"
	"github.com/janekzieleznicki/terraform-provider-quadlet/internal/systemd"
	"github.com/janekzieleznicki/terraform-provider-quadlet/internal/transport"
)

// TestMain registers sweepers before running tests.
func TestMain(m *testing.M) {
	resource.AddTestSweepers("quadlet", &resource.Sweeper{
		Name: "quadlet_unit",
		F: func(_ string) error {
			return sweepQuadletUnits()
		},
	})

	code := m.Run()
	os.Exit(code)
}

func sweepQuadletUnits() error {
	ctx := context.Background()
	t := transport.NewLocal()

	scopes := []scope.Scope{scope.ScopeSystem, scope.ScopeUser}
	for _, s := range scopes {
		unitDir, err := scope.UnitDir(ctx, t, s)
		if err != nil {
			// Directory may not exist; skip this scope
			continue
		}

		entries, err := os.ReadDir(unitDir)
		if err != nil {
			if os.IsNotExist(err) {
				// Directory doesn't exist; OK to skip
				continue
			}
			return fmt.Errorf("failed to read unit directory %s: %v", unitDir, err)
		}

		// Stop and remove all tf-acc-* units
		for _, entry := range entries {
			if !entry.IsDir() && strings.HasPrefix(entry.Name(), "tf-acc-") {
				// Extract base name without extension for systemctl
				baseName := strings.TrimSuffix(entry.Name(), filepath.Ext(entry.Name()))
				unitName := baseName + ".service"

				// Best-effort stop
				mgr := &systemd.Manager{Transport: t, Scope: s}
				_ = mgr.Stop(ctx, unitName)

				// Best-effort remove
				fullPath := filepath.Join(unitDir, entry.Name())
				_ = t.Remove(ctx, fullPath)
			}
		}

		// Daemon reload for this scope
		mgr := &systemd.Manager{Transport: t, Scope: s}
		_ = mgr.DaemonReload(ctx)
	}

	return nil
}
