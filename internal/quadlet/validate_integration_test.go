package quadlet

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/janekzieleznicki/terraform-provider-quadlet/internal/scope"
	"github.com/janekzieleznicki/terraform-provider-quadlet/internal/transport"
)

func TestValidate_RealGenerator(t *testing.T) {
	// Skip if the binary is not present
	if _, err := os.Stat(DefaultBinary); err != nil {
		t.Skipf("quadlet binary not present: %v", err)
	}

	ctx := context.Background()
	local := transport.NewLocal()
	defer local.Close()

	validator := &Validator{
		Transport: local,
		Scope:     scope.ScopeUser,
		Binary:    DefaultBinary,
	}

	// Test 1: Valid container unit
	t.Run("valid_container", func(t *testing.T) {
		result, problems, err := validator.Validate(
			ctx,
			"web.container",
			[]byte("[Container]\nImage=quay.io/libpod/alpine:latest\n"),
		)

		if err != nil {
			t.Errorf("expected no error, got %v", err)
		}

		if len(problems) != 0 {
			t.Errorf("expected no problems, got %v", problems)
		}

		if result.UnitName != "web.service" {
			t.Errorf("expected UnitName='web.service', got %q", result.UnitName)
		}

		if !strings.Contains(result.GeneratedUnit, "ExecStart=") {
			t.Errorf("expected GeneratedUnit to contain 'ExecStart='")
		}
	})

	// Test 2: Misspelled key
	t.Run("typo_imagee", func(t *testing.T) {
		result, problems, err := validator.Validate(
			ctx,
			"web.container",
			[]byte("[Container]\nImagee=quay.io/libpod/alpine:latest\n"),
		)

		if err != nil {
			t.Errorf("expected no error from validator, got %v", err)
		}

		if len(problems) != 1 {
			t.Errorf("expected 1 problem, got %d", len(problems))
		}

		if !strings.Contains(problems[0].Message, "unsupported key 'Imagee'") {
			t.Errorf("expected message to contain \"unsupported key 'Imagee'\", got %q", problems[0].Message)
		}

		if result.UnitName != "" {
			t.Errorf("expected empty result on rejection")
		}
	})

	// Test 3: Malformed INI
	t.Run("malformed_ini", func(t *testing.T) {
		result, problems, err := validator.Validate(
			ctx,
			"web.container",
			[]byte("[Container\n"),
		)

		if err != nil {
			t.Errorf("expected no error from validator, got %v", err)
		}

		if len(problems) == 0 {
			t.Errorf("expected at least one problem")
		}

		found := false
		for _, p := range problems {
			if strings.Contains(p.Message, "not a key-value pair") {
				found = true
				break
			}
		}

		if !found {
			t.Errorf("expected problem message to contain 'not a key-value pair'")
		}

		if result.UnitName != "" {
			t.Errorf("expected empty result on rejection")
		}
	})

	// Test 4: Volume unit with discovered suffix
	t.Run("volume_suffix_discovered", func(t *testing.T) {
		result, problems, err := validator.Validate(
			ctx,
			"web.volume",
			[]byte("[Volume]\n"),
		)

		if err != nil {
			t.Errorf("expected no error, got %v", err)
		}

		if len(problems) != 0 {
			t.Errorf("expected no problems, got %v", problems)
		}

		// The critical assertion: the suffix is discovered from the generator, not derived
		if result.UnitName != "web-volume.service" {
			t.Errorf("expected UnitName='web-volume.service', got %q", result.UnitName)
		}
	})
}
