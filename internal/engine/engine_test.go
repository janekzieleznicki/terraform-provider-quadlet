package engine

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/janekzieleznicki/terraform-provider-quadlet/internal/quadlet"
	"github.com/janekzieleznicki/terraform-provider-quadlet/internal/scope"
	"github.com/janekzieleznicki/terraform-provider-quadlet/internal/transport"
)

// newFakeHost returns a Fake with programmed responses for quadlet and systemd
func newFakeHost(showOutput, catOutput string) *transport.Fake {
	f := transport.NewFake()
	f.RuntimeDirPath = "/run/user/1000"
	f.RunFunc = func(c transport.Command) (transport.Result, error) {
		switch c.Path {
		case quadlet.DefaultBinary:
			// Simulate quadlet generator
			return transport.Result{
				Stdout:   "---web.service---\n[Unit]\nDescription=web\n",
				ExitCode: 0,
			}, nil
		case "/usr/bin/systemctl":
			if len(c.Args) > 0 {
				cmd := c.Args[0]
				if cmd == "--user" && len(c.Args) > 1 {
					cmd = c.Args[1]
				}
				switch cmd {
				case "show":
					return transport.Result{Stdout: showOutput, ExitCode: 0}, nil
				case "cat":
					return transport.Result{Stdout: catOutput, ExitCode: 0}, nil
				default:
					return transport.Result{ExitCode: 0}, nil
				}
			}
		}
		return transport.Result{ExitCode: 0}, nil
	}
	return f
}

func TestApply_HappyPath(t *testing.T) {
	f := newFakeHost(
		"LoadState=loaded\nActiveState=active\nSubState=running\nUnitFileState=generated\n",
		"# /run/user/1000/systemd/generator/web.service\n# Auto\n[Unit]\nDescription=web\n",
	)
	e := New(f, "", false)

	state, problems, err := e.Apply(context.Background(), ApplyRequest{
		Scope:    scope.ScopeUser,
		Filename: "web.container",
		Content:  []byte("[Container]\nImage=alpine\n"),
	})

	if err != nil {
		t.Fatalf("Apply failed: %v", err)
	}
	if len(problems) > 0 {
		t.Fatalf("Apply returned problems: %v", problems)
	}
	if state.UnitName != "web.service" {
		t.Errorf("expected UnitName=web.service, got %s", state.UnitName)
	}
	if !state.Status.IsActive() {
		t.Errorf("expected IsActive()=true")
	}
	if !strings.Contains(state.GeneratedUnit, "[Unit]") {
		t.Errorf("expected GeneratedUnit to contain [Unit], got %q", state.GeneratedUnit)
	}
	if strings.HasPrefix(state.GeneratedUnit, "#") {
		t.Errorf("expected GeneratedUnit to not start with #, got %q", state.GeneratedUnit)
	}
}
func TestApply_ValidationRejects(t *testing.T) {
	f := transport.NewFake()
	f.RuntimeDirPath = "/run/user/1000"
	f.RunFunc = func(c transport.Command) (transport.Result, error) {
		if c.Path == quadlet.DefaultBinary {
			// Simulate validation error with proper problem format
			return transport.Result{
				ExitCode: 1,
				Stderr:   "filename=typo.container severity=error message=typo error\n",
			}, nil
		}
		return transport.Result{ExitCode: 0}, nil
	}
	e := New(f, "", false)

	_, problems, err := e.Apply(context.Background(), ApplyRequest{
		Scope:    scope.ScopeUser,
		Filename: "typo.container",
		Content:  []byte("bad\n"),
	})

	// Should return problems or error, but not both
	if err == nil && len(problems) == 0 {
		t.Fatalf("Apply should reject invalid input")
	}

	// Check that file was NOT written
	if _, ok := f.Files["/home/fake/.config/containers/systemd/typo.container"]; ok {
		t.Errorf("file should not be written when validation fails")
	}
	// Check that no systemctl was invoked
	for _, cmd := range f.Commands {
		if cmd.Path == "/usr/bin/systemctl" {
			t.Errorf("no systemctl invocation expected when validation fails")
		}
	}
}

func TestApply_NotActive(t *testing.T) {
	f := newFakeHost(
		"LoadState=loaded\nActiveState=failed\nSubState=failed\nUnitFileState=generated\n",
		"[Unit]\nDescription=web\n",
	)
	e := New(f, "", false)

	_, _, err := e.Apply(context.Background(), ApplyRequest{
		Scope:    scope.ScopeUser,
		Filename: "web.container",
		Content:  []byte("[Container]\nImage=alpine\n"),
	})

	if err == nil {
		t.Fatalf("expected error when unit is not active")
	}
	if !strings.Contains(err.Error(), "did not become active") {
		t.Errorf("expected error about 'did not become active', got: %v", err)
	}
	if !strings.Contains(err.Error(), "ActiveState=failed") {
		t.Errorf("expected error to contain ActiveState=failed, got: %v", err)
	}
}

func TestApply_Ordering(t *testing.T) {
	f := newFakeHost(
		"LoadState=loaded\nActiveState=active\nSubState=running\nUnitFileState=generated\n",
		"[Unit]\nDescription=web\n",
	)
	e := New(f, "", false)

	e.Apply(context.Background(), ApplyRequest{
		Scope:    scope.ScopeUser,
		Filename: "web.container",
		Content:  []byte("[Container]\nImage=alpine\n"),
	})

	// Extract systemctl commands in order
	var verbs []string
	for _, cmd := range f.Commands {
		if cmd.Path == "/usr/bin/systemctl" && len(cmd.Args) > 0 {
			idx := 0
			if cmd.Args[0] == "--user" {
				idx = 1
			}
			if idx < len(cmd.Args) {
				verbs = append(verbs, cmd.Args[idx])
			}
		}
	}

	expected := []string{"daemon-reload", "restart", "show", "cat"}
	if len(verbs) != len(expected) {
		t.Errorf("expected %d systemctl invocations, got %d", len(expected), len(verbs))
	}
	for i, v := range verbs {
		if i < len(expected) && v != expected[i] {
			t.Errorf("at position %d: expected %q, got %q", i, expected[i], v)
		}
	}
}

func TestApply_MutexSerialization(t *testing.T) {
	f := transport.NewFake()
	f.RuntimeDirPath = "/run/user/1000"

	f.RunFunc = func(c transport.Command) (transport.Result, error) {
		if c.Path == quadlet.DefaultBinary {
			return transport.Result{
				Stdout:   "---web.service---\n[Unit]\nDescription=web\n",
				ExitCode: 0,
			}, nil
		}
		if c.Path == "/usr/bin/systemctl" && len(c.Args) > 0 {
			idx := 0
			if c.Args[0] == "--user" {
				idx = 1
			}
			if idx < len(c.Args) {
				switch c.Args[idx] {
				case "cat":
					return transport.Result{
						Stdout:   "[Unit]\nDescription=web\n",
						ExitCode: 0,
					}, nil
				}
			}
		}
		return transport.Result{
			Stdout:   "LoadState=loaded\nActiveState=active\nSubState=running\nUnitFileState=generated\n",
			ExitCode: 0,
		}, nil
	}

	e := New(f, "", false)

	// Run 8 concurrent applies; verify they all complete successfully
	var wg sync.WaitGroup
	var results []error
	var resultsMu sync.Mutex

	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, _, err := e.Apply(context.Background(), ApplyRequest{
				Scope:    scope.ScopeUser,
				Filename: fmt.Sprintf("web%d.container", i),
				Content:  []byte("[Container]\nImage=alpine\n"),
			})
			resultsMu.Lock()
			results = append(results, err)
			resultsMu.Unlock()
		}(i)
	}
	wg.Wait()

	// All concurrent applies should succeed
	for i, err := range results {
		if err != nil {
			t.Errorf("Apply %d failed: %v", i, err)
		}
	}

	// Verify all files were written
	for i := 0; i < 8; i++ {
		path := fmt.Sprintf("/home/fake/.config/containers/systemd/web%d.container", i)
		if _, ok := f.Files[path]; !ok {
			t.Errorf("file %s not written", path)
		}
	}
}

func TestApply_IndependentScopes(t *testing.T) {
	f := transport.NewFake()
	e := New(f, "", false)

	mu1 := e.mus[scope.ScopeSystem]
	mu2 := e.mus[scope.ScopeUser]

	if mu1 == mu2 {
		t.Errorf("expected different mutexes for different scopes")
	}
}

func TestRead_Present(t *testing.T) {
	f := transport.NewFake()
	f.Files["/home/fake/.config/containers/systemd/web.container"] = []byte("[Container]\nImage=alpine\n")
	f.RuntimeDirPath = "/run/user/1000"
	f.RunFunc = func(c transport.Command) (transport.Result, error) {
		if c.Path == "/usr/bin/systemctl" {
			if len(c.Args) > 0 {
				idx := 0
				if c.Args[0] == "--user" {
					idx = 1
				}
				if idx < len(c.Args) && c.Args[idx] == "show" {
					return transport.Result{
						Stdout:   "LoadState=loaded\nActiveState=active\nSubState=running\nUnitFileState=generated\n",
						ExitCode: 0,
					}, nil
				}
				if idx < len(c.Args) && c.Args[idx] == "cat" {
					return transport.Result{
						Stdout:   "[Unit]\nDescription=web\n",
						ExitCode: 0,
					}, nil
				}
			}
		}
		return transport.Result{ExitCode: 0}, nil
	}

	e := New(f, "", false)
	state, found, err := e.Read(context.Background(), scope.ScopeUser, "web.container", "web.service")

	if err != nil {
		t.Fatalf("Read failed: %v", err)
	}
	if !found {
		t.Errorf("expected found=true")
	}
	if state.UnitName != "web.service" {
		t.Errorf("expected UnitName=web.service, got %s", state.UnitName)
	}
}

func TestRead_Absent(t *testing.T) {
	f := transport.NewFake()
	f.RuntimeDirPath = "/run/user/1000"
	f.RunFunc = func(c transport.Command) (transport.Result, error) {
		if c.Path == "/usr/bin/systemctl" && len(c.Args) > 0 {
			idx := 0
			if c.Args[0] == "--user" {
				idx = 1
			}
			if idx < len(c.Args) && c.Args[idx] == "show" {
				return transport.Result{ExitCode: 5}, nil
			}
		}
		return transport.Result{ExitCode: 0}, nil
	}

	e := New(f, "", false)
	_, found, err := e.Read(context.Background(), scope.ScopeUser, "web.container", "web.service")

	if err != nil {
		t.Fatalf("Read failed: %v", err)
	}
	if found {
		t.Errorf("expected found=false")
	}
}

func TestValidate_Valid(t *testing.T) {
	f := newFakeHost(
		"",
		"# /run/user/1000/systemd/generator/web.service\n# Auto\n[Unit]\nDescription=web\n",
	)
	e := New(f, "", false)

	result, problems, err := e.Validate(context.Background(), scope.ScopeUser, "web.container", []byte("[Container]\nImage=alpine\n"))

	if err != nil {
		t.Fatalf("Validate failed: %v", err)
	}
	if len(problems) > 0 {
		t.Fatalf("Validate returned problems: %v", problems)
	}
	if result.UnitName != "web.service" {
		t.Errorf("expected UnitName=web.service, got %s", result.UnitName)
	}
	if !strings.Contains(result.GeneratedUnit, "[Unit]") {
		t.Errorf("expected GeneratedUnit to contain [Unit], got %q", result.GeneratedUnit)
	}
	// Verify no files were written (only uses temp staging within Fake)
	if len(f.Files) > 0 {
		t.Errorf("expected no files written, got %v", f.Files)
	}
}

func TestValidate_Invalid(t *testing.T) {
	f := transport.NewFake()
	f.RuntimeDirPath = "/run/user/1000"
	f.RunFunc = func(c transport.Command) (transport.Result, error) {
		if c.Path == quadlet.DefaultBinary {
			// Simulate validation error with proper problem format
			return transport.Result{
				ExitCode: 1,
				Stderr:   "filename=web.container severity=error message=Invalid Image key\n",
			}, nil
		}
		return transport.Result{ExitCode: 0}, nil
	}

	e := New(f, "", false)
	_, problems, err := e.Validate(context.Background(), scope.ScopeUser, "web.container", []byte("[Container]\nImagee=alpine\n"))

	// Validation errors should return problems
	if err == nil && len(problems) == 0 {
		t.Fatalf("Validate should reject invalid input")
	}

	// Verify no systemctl commands were issued
	for _, cmd := range f.Commands {
		if cmd.Path == "/usr/bin/systemctl" {
			t.Errorf("Validate should not issue systemctl commands, but got: %v", cmd)
		}
	}
}

func TestPath(t *testing.T) {
	f := newFakeHost("", "")
	e := New(f, "", false)

	fullPath, err := e.Path(context.Background(), scope.ScopeUser, "web.container")

	if err != nil {
		t.Fatalf("Path failed: %v", err)
	}
	expected := "/home/fake/.config/containers/systemd/web.container"
	if fullPath != expected {
		t.Errorf("expected path %s, got %s", expected, fullPath)
	}
	// Verify no Run commands were issued (Path is pure)
	if len(f.Commands) > 0 {
		t.Errorf("Path should not issue any commands, but got %d commands", len(f.Commands))
	}
}
