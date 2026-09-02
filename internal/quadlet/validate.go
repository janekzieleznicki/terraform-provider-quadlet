package quadlet

import (
	"context"
	"fmt"
	"path"

	"github.com/janekzieleznicki/terraform-provider-quadlet/internal/scope"
	"github.com/janekzieleznicki/terraform-provider-quadlet/internal/transport"
)

// Validator validates quadlet unit files using the quadlet generator.
type Validator struct {
	Transport transport.Transport
	Scope     scope.Scope
	Binary    string // defaults to DefaultBinary when empty
}

// DefaultBinary is the default path to the quadlet generator binary.
const DefaultBinary = "/usr/libexec/podman/quadlet"

// Result is the outcome of validation.
type Result struct {
	UnitName      string // from the stdout delimiter; authoritative
	GeneratedUnit string
}

// Validate stages exactly one unit file in an isolated directory and runs the
// generator in dryrun mode. Problems is non-empty only when the generator
// rejected the input; err is reserved for failures to execute or interpret the
// generator at all.
func (v *Validator) Validate(ctx context.Context, filename string, content []byte) (Result, []Problem, error) {
	bin := v.Binary
	if bin == "" {
		bin = DefaultBinary
	}

	// Create a temporary directory
	dir, err := v.Transport.MkdirTemp(ctx, "tf-quadlet-validate-*")
	if err != nil {
		return Result{}, nil, fmt.Errorf("create temp dir: %w", err)
	}
	// Defer cleanup, but ignore errors
	defer v.Transport.RemoveAll(ctx, dir) //nolint:errcheck

	// Stage the unit file
	if err := v.Transport.WriteFile(ctx, path.Join(dir, filename), content, 0o644); err != nil {
		return Result{}, nil, fmt.Errorf("stage unit file: %w", err)
	}

	// Build the quadlet command
	args := append(scope.QuadletFlags(v.Scope), "-dryrun")

	// Run the generator
	res, err := v.Transport.Run(ctx, transport.Command{
		Path: bin,
		Args: args,
		Env: map[string]string{
			"QUADLET_UNIT_DIRS": dir,
		},
	})
	if err != nil {
		return Result{}, nil, fmt.Errorf("run generator: %w", err)
	}

	// Branch on exit code
	switch res.ExitCode {
	case 0:
		// Success: parse stdout
		units := ParseGeneratedUnits(res.Stdout)

		switch len(units) {
		case 0:
			// No units generated; this is an error
			return Result{}, nil, fmt.Errorf("quadlet generator produced no unit for %s; confirm the extension is one of %v; stderr:\n%s", filename, Types(), res.Stderr)

		case 1:
			// Exactly one unit; this is what we expect
			return Result{
				UnitName:      units[0].Name,
				GeneratedUnit: units[0].Body,
			}, nil, nil

		default:
			// Multiple units; this shouldn't happen with a single file
			names := make([]string, len(units))
			for i, u := range units {
				names[i] = u.Name
			}
			return Result{}, nil, fmt.Errorf("quadlet generator produced multiple units for %s: %v", filename, names)
		}

	case 1:
		// Failure: parse problems from stderr
		problems := ParseProblems(res.Stderr)

		if len(problems) == 0 {
			// Generator rejected the input but emitted no recognizable diagnostic
			return Result{}, nil, fmt.Errorf("quadlet generator rejected %s but emitted no recognisable diagnostic; stderr:\n%s", filename, res.Stderr)
		}

		return Result{}, problems, nil

	default:
		// Unexpected exit code
		return Result{}, nil, fmt.Errorf("quadlet generator exited %d (expected 0 or 1); stderr:\n%s", res.ExitCode, res.Stderr)
	}
}
