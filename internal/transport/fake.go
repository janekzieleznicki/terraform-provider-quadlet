package transport

import (
	"context"
	"fmt"
	"io/fs"
	"path"
	"strings"
	"sync"
)

// Fake is an in-memory Transport used by tier-1 tests.
type Fake struct {
	mu             sync.Mutex
	Files          map[string][]byte             // path -> contents
	Dirs           map[string]bool               // path -> exists
	Commands       []Command                     // every Run call, in order
	RunFunc        func(Command) (Result, error) // nil => zero Result, nil error
	ConfigDir      string                        // UserConfigDir result; default "/home/fake/.config"
	RuntimeDirPath string                        // RuntimeDir result; default "/run/user/1000"
	TempRoot       string                        // MkdirTemp parent;      default "/tmp"

	tempSeq int
}

// NewFake creates a new in-memory transport with initialized maps.
func NewFake() *Fake {
	return &Fake{
		Files:          make(map[string][]byte),
		Dirs:           make(map[string]bool),
		ConfigDir:      "/home/fake/.config",
		RuntimeDirPath: "/run/user/1000",
		TempRoot:       "/tmp",
	}
}

// Run appends a copy of the command to Commands and delegates to RunFunc if set.
func (f *Fake) Run(_ context.Context, c Command) (Result, error) {
	// Deep copy the command to prevent mutations from affecting the record
	cmdCopy := Command{
		Path: c.Path,
		Args: append([]string(nil), c.Args...),
		Env:  make(map[string]string),
		Sudo: c.Sudo,
	}
	for k, v := range c.Env {
		cmdCopy.Env[k] = v
	}
	f.mu.Lock()
	f.Commands = append(f.Commands, cmdCopy)
	f.mu.Unlock()

	if f.RunFunc != nil {
		return f.RunFunc(c)
	}
	return Result{}, nil
}

// WriteFile stores a copy of data at the given path.
func (f *Fake) WriteFile(_ context.Context, path string, data []byte, _ fs.FileMode) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Files[path] = append([]byte(nil), data...)
	return nil
}

// ReadFile returns the contents at the given path, or ErrNotExist if not found.
func (f *Fake) ReadFile(_ context.Context, path string) ([]byte, error) {
	f.mu.Lock()
	data, ok := f.Files[path]
	f.mu.Unlock()

	if !ok {
		return nil, fmt.Errorf("%s: %w", path, ErrNotExist)
	}
	return append([]byte(nil), data...), nil
}

// Remove deletes a file, or returns ErrNotExist if not found.
func (f *Fake) Remove(_ context.Context, path string) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	if _, ok := f.Files[path]; !ok {
		return fmt.Errorf("%s: %w", path, ErrNotExist)
	}
	delete(f.Files, path)
	return nil
}

// RemoveAll deletes a directory and all files under it. Missing paths are not an error.
func (f *Fake) RemoveAll(_ context.Context, pathStr string) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	// Ensure trailing slash for prefix matching
	if !strings.HasSuffix(pathStr, "/") {
		pathStr += "/"
	}

	// Remove files under this path
	for p := range f.Files {
		if strings.HasPrefix(p, pathStr) || p == strings.TrimSuffix(pathStr, "/") {
			delete(f.Files, p)
		}
	}

	// Remove directory entries
	for p := range f.Dirs {
		if strings.HasPrefix(p, pathStr) || p == strings.TrimSuffix(pathStr, "/") {
			delete(f.Dirs, p)
		}
	}

	return nil
}

// MkdirAll records a directory and all its ancestors.
func (f *Fake) MkdirAll(_ context.Context, pathStr string, _ fs.FileMode) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	// Record the directory itself
	f.Dirs[pathStr] = true

	// Record all parent directories
	for {
		parent := path.Dir(pathStr)
		if parent == pathStr || parent == "." {
			break
		}
		f.Dirs[parent] = true
		pathStr = parent
	}

	return nil
}

// MkdirTemp creates a directory with a name derived from pattern.
func (f *Fake) MkdirTemp(_ context.Context, pattern string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.tempSeq++
	// Use a simple counter-based approach instead of random
	name := fmt.Sprintf("%s-%d", pattern, f.tempSeq)
	dir := path.Join(f.TempRoot, name)
	f.Dirs[dir] = true
	return dir, nil
}

// UserConfigDir returns the configured config directory.
func (f *Fake) UserConfigDir(_ context.Context) (string, error) {
	return f.ConfigDir, nil
}

// RuntimeDir returns the configured runtime directory.
func (f *Fake) RuntimeDir(_ context.Context) (string, error) {
	return f.RuntimeDirPath, nil
}

// Close returns nil.
func (f *Fake) Close() error {
	return nil
}

// Compile-time assertion
var _ Transport = (*Fake)(nil)
