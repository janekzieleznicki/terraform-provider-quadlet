package engine

import (
	"context"
	"os"
	"testing"

	"github.com/janekzieleznicki/terraform-provider-quadlet/internal/quadlet"
	"github.com/janekzieleznicki/terraform-provider-quadlet/internal/scope"
	"github.com/janekzieleznicki/terraform-provider-quadlet/internal/transport"
)

func TestEngine_Live(t *testing.T) {
	if os.Getenv("QUADLET_LIVE_TEST") != "1" {
		t.Skip("set QUADLET_LIVE_TEST=1 to run against real systemd")
	}

	if _, err := os.Stat(quadlet.DefaultBinary); err != nil {
		t.Skipf("quadlet binary not present: %v", err)
	}

	// Create transport and engine
	local := transport.NewLocal()
	e := New(local, "", false)

	ctx := context.Background()

	// Register cleanup before applying
	t.Cleanup(func() {
		// Destroy the unit
		e.Destroy(ctx, scope.ScopeUser, "tf-phase2-live.volume", "tf-phase2-live-volume.service")

		// Clean up podman volume (best-effort)
		// This would be podman volume rm systemd-tf-phase2-live
		// but we skip it here to avoid dependency on podman CLI
	})

	// Apply the unit
	state, problems, err := e.Apply(ctx, ApplyRequest{
		Scope:    scope.ScopeUser,
		Filename: "tf-phase2-live.volume",
		Content:  []byte("[Volume]\n"),
	})

	if err != nil {
		t.Fatalf("Apply failed: %v", err)
	}
	if len(problems) > 0 {
		t.Fatalf("Apply returned problems: %v", problems)
	}

	// Verify discovered unit name
	if state.UnitName != "tf-phase2-live-volume.service" {
		t.Errorf("expected UnitName=tf-phase2-live-volume.service, got %s", state.UnitName)
	}

	// Verify status
	if !state.Status.IsActive() {
		t.Errorf("expected IsActive()=true, got %v", state.Status.ActiveState)
	}
	if state.Status.SubState != "exited" {
		t.Errorf("expected SubState=exited, got %s", state.Status.SubState)
	}

	// Verify generated unit
	if state.GeneratedUnit == "" {
		t.Errorf("expected non-empty GeneratedUnit")
	}
	if !contains(state.GeneratedUnit, "podman volume create") {
		t.Errorf("expected GeneratedUnit to contain 'podman volume create'")
	}
	if startswith(state.GeneratedUnit, "#") {
		t.Errorf("expected GeneratedUnit to not start with #")
	}

	// Test Read with empty unitName (import path)
	state2, found, err := e.Read(ctx, scope.ScopeUser, "tf-phase2-live.volume", "")
	if err != nil {
		t.Fatalf("Read (import) failed: %v", err)
	}
	if !found {
		t.Errorf("Read (import) expected found=true")
	}
	if state2.UnitName != "tf-phase2-live-volume.service" {
		t.Errorf("Read (import) expected UnitName=tf-phase2-live-volume.service, got %s", state2.UnitName)
	}

	// Test Destroy
	err = e.Destroy(ctx, scope.ScopeUser, "tf-phase2-live.volume", "tf-phase2-live-volume.service")
	if err != nil {
		t.Fatalf("Destroy failed: %v", err)
	}

	// Verify Read returns not found after destroy
	state3, found, err := e.Read(ctx, scope.ScopeUser, "tf-phase2-live.volume", "tf-phase2-live-volume.service")
	if err != nil {
		t.Fatalf("Read (after destroy) failed: %v", err)
	}
	if found {
		t.Errorf("Read (after destroy) expected found=false, got true")
	}
	if state3.Status.Exists() {
		t.Errorf("expected Exists()=false after destroy")
	}
}

func contains(s, substr string) bool {
	for i := 0; i < len(s)-len(substr)+1; i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func startswith(s, prefix string) bool {
	return len(s) >= len(prefix) && s[:len(prefix)] == prefix
}
