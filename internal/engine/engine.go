// Package engine orchestrates the write -> daemon-reload -> restart sequence
// and serialises it per scope.
package engine

import (
	"context"
	"errors"
	"fmt"
	"path"
	"sync"

	"github.com/janekzieleznicki/terraform-provider-quadlet/internal/quadlet"
	"github.com/janekzieleznicki/terraform-provider-quadlet/internal/scope"
	"github.com/janekzieleznicki/terraform-provider-quadlet/internal/systemd"
	"github.com/janekzieleznicki/terraform-provider-quadlet/internal/transport"
)

// Engine orchestrates unit file write, daemon-reload, and restart operations
// for Podman Quadlet units, serializing them per scope to ensure consistency.
type Engine struct {
	transport transport.Transport
	binary    string // quadlet generator path; "" means quadlet.DefaultBinary
	sudo      bool
	mus       map[scope.Scope]*sync.Mutex
}

// New returns an Engine. A single Engine instance must be shared by every
// resource targeting the same host: the daemon-reload mutex lives in it, so a
// per-resource Engine would serialise nothing.
func New(t transport.Transport, quadletBinary string, sudo bool) *Engine {
	return &Engine{
		transport: t,
		binary:    quadletBinary,
		sudo:      sudo,
		mus: map[scope.Scope]*sync.Mutex{
			scope.ScopeSystem: {},
			scope.ScopeUser:   {},
		},
	}
}

// ApplyRequest contains the unit file configuration to apply.
type ApplyRequest struct {
	Scope    scope.Scope
	Filename string // e.g. "web.container"
	Content  []byte
}

// UnitState is the current state of a Quadlet unit after Apply or Read.
type UnitState struct {
	Path          string // absolute path of the unit file
	UnitName      string // e.g. "web.service", discovered, never derived
	GeneratedUnit string // from systemctl cat, header stripped
	Content       []byte
	Status        systemd.UnitStatus
}

// lock acquires the scope's mutex and returns an unlock function.
func (e *Engine) lock(s scope.Scope) (func(), error) {
	mu, ok := e.mus[s]
	if !ok {
		return nil, fmt.Errorf("unknown scope %q", s)
	}
	mu.Lock()
	return mu.Unlock, nil
}

// Apply validates, writes, and restarts a unit.
func (e *Engine) Apply(ctx context.Context, req ApplyRequest) (UnitState, []quadlet.Problem, error) {
	// Validation first, outside the lock, because it is read-only
	v := &quadlet.Validator{Transport: e.transport, Scope: req.Scope, Binary: e.binary}
	res, problems, err := v.Validate(ctx, req.Filename, req.Content)
	if err != nil {
		return UnitState{}, nil, err
	}
	if len(problems) > 0 {
		return UnitState{}, problems, nil
	}

	// Get unit directory
	unitDir, err := scope.UnitDir(ctx, e.transport, req.Scope)
	if err != nil {
		return UnitState{}, nil, err
	}

	// Create directory if needed
	if err := e.transport.MkdirAll(ctx, unitDir, 0o755); err != nil {
		return UnitState{}, nil, err
	}

	// Acquire lock and enter critical section
	unlock, err := e.lock(req.Scope)
	if err != nil {
		return UnitState{}, nil, err
	}
	defer unlock()

	// Write the file
	fullPath := path.Join(unitDir, req.Filename)
	if err := e.transport.WriteFile(ctx, fullPath, req.Content, 0o644); err != nil {
		return UnitState{}, nil, err
	}

	// Create manager
	mgr := &systemd.Manager{Transport: e.transport, Scope: req.Scope, Sudo: e.sudo}

	// Daemon reload
	if err := mgr.DaemonReload(ctx); err != nil {
		return UnitState{}, nil, err
	}

	// Restart the unit
	if err := mgr.Restart(ctx, res.UnitName); err != nil {
		return UnitState{}, nil, err
	}

	// Check if active
	st, err := mgr.Show(ctx, res.UnitName)
	if err != nil {
		return UnitState{}, nil, err
	}
	if !st.IsActive() {
		j, _ := mgr.Journal(ctx, res.UnitName, 50)
		return UnitState{}, nil, fmt.Errorf("unit %s did not become active after restart (ActiveState=%s SubState=%s); recent journal:\n%s", res.UnitName, st.ActiveState, st.SubState, j)
	}

	// Get generated unit content
	body, err := mgr.Cat(ctx, res.UnitName)
	if err != nil {
		return UnitState{}, nil, err
	}

	return UnitState{
		Path:          fullPath,
		UnitName:      res.UnitName,
		GeneratedUnit: systemd.StripCatHeader(body),
		Content:       req.Content,
		Status:        st,
	}, nil, nil
}

// Read returns the current state of a unit file.
func (e *Engine) Read(ctx context.Context, s scope.Scope, filename, unitName string) (UnitState, bool, error) {
	unitDir, err := scope.UnitDir(ctx, e.transport, s)
	if err != nil {
		return UnitState{}, false, err
	}

	fullPath := path.Join(unitDir, filename)
	content, err := e.transport.ReadFile(ctx, fullPath)
	if err != nil {
		if errors.Is(err, transport.ErrNotExist) {
			return UnitState{}, false, nil
		}
		return UnitState{}, false, err
	}

	// If unitName is empty, discover it from the validator
	if unitName == "" {
		v := &quadlet.Validator{Transport: e.transport, Scope: s, Binary: e.binary}
		res, problems, err := v.Validate(ctx, filename, content)
		if err != nil {
			return UnitState{}, false, err
		}
		if len(problems) > 0 {
			// File on disk is not valid
			if len(problems) > 0 {
				return UnitState{}, false, fmt.Errorf("file %s on disk has problems: %s", filename, problems[0].Message)
			}
		}
		unitName = res.UnitName
	}

	// Create manager and get status
	mgr := &systemd.Manager{Transport: e.transport, Scope: s, Sudo: e.sudo}
	st, err := mgr.Show(ctx, unitName)
	if err != nil {
		return UnitState{}, false, err
	}

	// Get generated unit body if it exists
	var generatedUnit string
	if st.Exists() {
		body, err := mgr.Cat(ctx, unitName)
		if err != nil {
			return UnitState{}, false, err
		}
		generatedUnit = systemd.StripCatHeader(body)
	}

	return UnitState{
		Path:          fullPath,
		UnitName:      unitName,
		GeneratedUnit: generatedUnit,
		Content:       content,
		Status:        st,
	}, true, nil
}

// Destroy stops and removes a unit file.
func (e *Engine) Destroy(ctx context.Context, s scope.Scope, filename, unitName string) error {
	unitDir, err := scope.UnitDir(ctx, e.transport, s)
	if err != nil {
		return err
	}

	unlock, err := e.lock(s)
	if err != nil {
		return err
	}
	defer unlock()

	mgr := &systemd.Manager{Transport: e.transport, Scope: s, Sudo: e.sudo}

	// Check if running and stop if needed
	st, err := mgr.Show(ctx, unitName)
	if err != nil {
		return err
	}
	if st.Exists() && st.ActiveState != "inactive" {
		if err := mgr.Stop(ctx, unitName); err != nil {
			return err
		}
	}

	// Remove the file
	fullPath := path.Join(unitDir, filename)
	if err := e.transport.Remove(ctx, fullPath); err != nil {
		if !errors.Is(err, transport.ErrNotExist) {
			return err
		}
		// File already gone is not an error
	}

	// Daemon reload
	if err := mgr.DaemonReload(ctx); err != nil {
		return err
	}

	return nil
}

// Validate performs a read-only, plan-time dryrun of the generator for the
// given scope, filename, and content. It writes nothing to the production
// unit directory and issues no systemd commands, making it safe to call
// during Terraform's plan phase.
func (e *Engine) Validate(ctx context.Context, s scope.Scope, filename string, content []byte) (quadlet.Result, []quadlet.Problem, error) {
	v := &quadlet.Validator{Transport: e.transport, Scope: s, Binary: e.binary}
	return v.Validate(ctx, filename, content)
}

// Path returns the absolute path a unit named filename would occupy in the
// given scope, without touching the filesystem. It is a pure function of
// scope and filename and is safe to call during Terraform's plan phase.
func (e *Engine) Path(ctx context.Context, s scope.Scope, filename string) (string, error) {
	unitDir, err := scope.UnitDir(ctx, e.transport, s)
	if err != nil {
		return "", err
	}
	return path.Join(unitDir, filename), nil
}
