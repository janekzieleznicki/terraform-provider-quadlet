// Package transport defines where a command runs and where files live.
package transport

import (
	"context"
	"errors"
	"io/fs"
)

// ErrNotExist is returned (wrapped) by ReadFile, Remove and RemoveAll when the
// target path does not exist. Implementations MUST wrap so that
// errors.Is(err, transport.ErrNotExist) holds.
var ErrNotExist = errors.New("transport: path does not exist")

// Command is a single process invocation on the target host.
type Command struct {
	Path string            // absolute path to the executable
	Args []string          // arguments, excluding argv[0]
	Env  map[string]string // extra environment; merged over the inherited environment
	Sudo bool              // run via "sudo -n --"
}

// Result is the outcome of a Command. ExitCode is meaningful only when the
// corresponding Run call returned a nil error.
type Result struct {
	Stdout   string
	Stderr   string
	ExitCode int
}

// Transport is WHERE a command runs and where files live. Implementations are
// local (this host) or ssh (a remote host). Nothing in this interface
// knows anything about systemd or quadlet.
type Transport interface {
	Run(ctx context.Context, c Command) (Result, error)
	WriteFile(ctx context.Context, path string, data []byte, mode fs.FileMode) error
	ReadFile(ctx context.Context, path string) ([]byte, error)
	Remove(ctx context.Context, path string) error
	RemoveAll(ctx context.Context, path string) error
	MkdirAll(ctx context.Context, path string, mode fs.FileMode) error

	// MkdirTemp creates a new directory with a name derived from pattern and
	// returns its ABSOLUTE path. Absolute is mandatory: quadlet rejects a
	// relative entry in QUADLET_UNIT_DIRS. Callers are responsible for
	// RemoveAll.
	MkdirTemp(ctx context.Context, pattern string) (string, error)

	// UserConfigDir returns the target user's config directory following Go's
	// os.UserConfigDir semantics: $XDG_CONFIG_HOME when set and absolute,
	// otherwise $HOME/.config. Upstream quadlet resolves the rootless unit
	// directory the same way, so this must not be hardcoded to ~/.config.
	UserConfigDir(ctx context.Context) (string, error)

	// RuntimeDir returns the target user's runtime directory, following
	// $XDG_RUNTIME_DIR when set and absolute, otherwise /run/user/<uid>.
	// systemctl --user cannot reach the user bus without it, so every
	// user-scope command must carry it explicitly.
	RuntimeDir(ctx context.Context) (string, error)

	Close() error
}
