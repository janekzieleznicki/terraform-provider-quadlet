package transport

import (
	"bytes"
	"context"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path"
	"sort"
	"strconv"
)

// Local implements Transport against the local host.
type Local struct {
}

// NewLocal creates a new local transport.
func NewLocal() *Local {
	return &Local{}
}

// Run executes a command on the local host.
func (l *Local) Run(ctx context.Context, c Command) (Result, error) {
	// Build argv
	argv := []string{c.Path}
	if c.Sudo {
		argv = append([]string{"sudo", "-n", "--"}, argv...)
		if len(c.Env) > 0 {
			// sudo's env_reset drops cmd.Env, so the variables must be
			// re-applied on the far side of sudo.
			envArgv := envAssignments(c.Env)
			// Build: sudo -n -- env K=V ... -- /path/to/cmd
			prefix := []string{"sudo", "-n", "--", "env"}
			prefix = append(prefix, envArgv...)
			prefix = append(prefix, "--", c.Path)
			argv = prefix
		}
	}
	argv = append(argv, c.Args...)

	// Create command
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)

	// Set environment: inherited environment + extra env entries sorted by key
	cmd.Env = append(os.Environ(), envAssignments(c.Env)...)

	// Capture stdout and stderr separately
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	// Run the command
	err := cmd.Run()
	if err == nil {
		return Result{
			Stdout:   stdout.String(),
			Stderr:   stderr.String(),
			ExitCode: 0,
		}, nil
	}

	// Check for exit error
	if ee, ok := err.(*exec.ExitError); ok {
		return Result{
			Stdout:   stdout.String(),
			Stderr:   stderr.String(),
			ExitCode: ee.ExitCode(),
		}, nil
	}

	// Check for context error
	if ctx.Err() != nil {
		return Result{}, ctx.Err()
	}

	// Any other error
	return Result{}, fmt.Errorf("run %s: %w", c.Path, err)
}

// WriteFile writes data to a file on the local host.
func (l *Local) WriteFile(_ context.Context, path string, data []byte, mode fs.FileMode) error {
	return os.WriteFile(path, data, mode)
}

// ReadFile reads a file from the local host.
func (l *Local) ReadFile(_ context.Context, path string) ([]byte, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("%s: %w", path, ErrNotExist)
		}
		return nil, err
	}
	return b, nil
}

// Remove deletes a file on the local host.
func (l *Local) Remove(_ context.Context, path string) error {
	err := os.Remove(path)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("%s: %w", path, ErrNotExist)
		}
		return err
	}
	return nil
}

// RemoveAll deletes a directory tree on the local host.
func (l *Local) RemoveAll(_ context.Context, path string) error {
	return os.RemoveAll(path)
}

// MkdirAll creates a directory tree on the local host.
func (l *Local) MkdirAll(_ context.Context, path string, mode fs.FileMode) error {
	return os.MkdirAll(path, mode)
}

// MkdirTemp creates a temporary directory on the local host.
func (l *Local) MkdirTemp(_ context.Context, pattern string) (string, error) {
	return os.MkdirTemp("", pattern)
}

// UserConfigDir returns the user's config directory.
func (l *Local) UserConfigDir(_ context.Context) (string, error) {
	return os.UserConfigDir()
}

// RuntimeDir returns the user's runtime directory, following
// $XDG_RUNTIME_DIR when set and absolute, otherwise /run/user/<uid>.
func (l *Local) RuntimeDir(_ context.Context) (string, error) {
	if d := os.Getenv("XDG_RUNTIME_DIR"); path.IsAbs(d) {
		return d, nil
	}
	return "/run/user/" + strconv.Itoa(os.Getuid()), nil
}

// Close returns nil (local transport has no resources to close).
func (l *Local) Close() error {
	return nil
}

// envAssignments returns "K=V" strings sorted by key, for deterministic argv.
func envAssignments(env map[string]string) []string {
	if len(env) == 0 {
		return nil
	}
	// Sort keys for deterministic environment
	keys := make([]string, 0, len(env))
	for k := range env {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	result := make([]string, 0, len(env))
	for _, k := range keys {
		result = append(result, k+"="+env[k])
	}
	return result
}

// Compile-time assertion
var _ Transport = (*Local)(nil)
