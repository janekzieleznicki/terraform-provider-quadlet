package transport

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
)

func TestLocal_WriteAndReadFile(t *testing.T) {
	l := NewLocal()
	ctx := context.Background()
	tmpDir := t.TempDir()

	testPath := tmpDir + "/test.txt"
	testData := []byte("hello world")

	// Write
	if err := l.WriteFile(ctx, testPath, testData, 0o644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	// Read
	data, err := l.ReadFile(ctx, testPath)
	if err != nil {
		t.Fatalf("ReadFile failed: %v", err)
	}

	if string(data) != string(testData) {
		t.Errorf("expected %q, got %q", testData, data)
	}
}

func TestLocal_ReadFileNotExist(t *testing.T) {
	l := NewLocal()
	ctx := context.Background()

	_, err := l.ReadFile(ctx, "/nonexistent/file/that/does/not/exist")
	if !errors.Is(err, ErrNotExist) {
		t.Errorf("expected ErrNotExist, got %v", err)
	}
}

func TestLocal_Remove(t *testing.T) {
	l := NewLocal()
	ctx := context.Background()
	tmpDir := t.TempDir()

	testPath := tmpDir + "/test.txt"
	if err := l.WriteFile(ctx, testPath, []byte("data"), 0o644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	// Remove the file
	if err := l.Remove(ctx, testPath); err != nil {
		t.Fatalf("Remove failed: %v", err)
	}

	// Verify it's gone
	if _, err := l.ReadFile(ctx, testPath); !errors.Is(err, ErrNotExist) {
		t.Errorf("expected file to be deleted")
	}
}

func TestLocal_RemoveAll(t *testing.T) {
	l := NewLocal()
	ctx := context.Background()
	tmpDir := t.TempDir()

	dirPath := tmpDir + "/dir"
	if err := l.MkdirAll(ctx, dirPath, 0o755); err != nil {
		t.Fatalf("MkdirAll failed: %v", err)
	}

	filePath := dirPath + "/file.txt"
	if err := l.WriteFile(ctx, filePath, []byte("data"), 0o644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	// Remove the directory tree
	if err := l.RemoveAll(ctx, dirPath); err != nil {
		t.Fatalf("RemoveAll failed: %v", err)
	}

	// Verify it's gone
	if _, err := os.Stat(dirPath); !os.IsNotExist(err) {
		t.Errorf("expected directory to be deleted")
	}
}

func TestLocal_RunShellCommand(t *testing.T) {
	l := NewLocal()
	ctx := context.Background()

	// Run a shell command that outputs to stdout and stderr and exits with code 3
	result, err := l.Run(ctx, Command{
		Path: "/bin/sh",
		Args: []string{"-c", "printf out; printf err >&2; exit 3"},
	})

	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	if result.Stdout != "out" {
		t.Errorf("expected Stdout='out', got '%s'", result.Stdout)
	}

	if result.Stderr != "err" {
		t.Errorf("expected Stderr='err', got '%s'", result.Stderr)
	}

	if result.ExitCode != 3 {
		t.Errorf("expected ExitCode=3, got %d", result.ExitCode)
	}
}

func TestLocal_RunWithEnv(t *testing.T) {
	l := NewLocal()
	ctx := context.Background()

	// Run a shell command that prints an environment variable
	result, err := l.Run(ctx, Command{
		Path: "/bin/sh",
		Args: []string{"-c", "printf \"$FOO\""},
		Env: map[string]string{
			"FOO": "bar",
		},
	})

	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	if result.Stdout != "bar" {
		t.Errorf("expected Stdout='bar', got '%s'", result.Stdout)
	}

	if result.ExitCode != 0 {
		t.Errorf("expected ExitCode=0, got %d", result.ExitCode)
	}
}

func TestLocal_RunWithCancelledContext(t *testing.T) {
	l := NewLocal()
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	// Run should fail with context error
	_, err := l.Run(ctx, Command{
		Path: "/bin/sh",
		Args: []string{"-c", "sleep 10"},
	})

	if err == nil {
		t.Errorf("expected error with cancelled context, got nil")
	}
}

func TestLocal_MkdirAll(t *testing.T) {
	l := NewLocal()
	ctx := context.Background()
	tmpDir := t.TempDir()

	path := tmpDir + "/a/b/c"
	if err := l.MkdirAll(ctx, path, 0o755); err != nil {
		t.Fatalf("MkdirAll failed: %v", err)
	}

	if _, err := os.Stat(path); err != nil {
		t.Errorf("expected directory to be created: %v", err)
	}
}

func TestLocal_UserConfigDir(t *testing.T) {
	l := NewLocal()
	ctx := context.Background()

	configDir, err := l.UserConfigDir(ctx)
	if err != nil {
		t.Fatalf("UserConfigDir failed: %v", err)
	}

	if configDir == "" {
		t.Errorf("expected non-empty config dir")
	}
}

func TestLocal_RuntimeDir_AbsoluteXDG(t *testing.T) {
	l := NewLocal()
	ctx := context.Background()

	t.Setenv("XDG_RUNTIME_DIR", "/run/user/1000")

	runtimeDir, err := l.RuntimeDir(ctx)
	if err != nil {
		t.Fatalf("RuntimeDir failed: %v", err)
	}

	if runtimeDir != "/run/user/1000" {
		t.Errorf("expected /run/user/1000, got %q", runtimeDir)
	}
}

func TestLocal_RuntimeDir_RelativeXDG(t *testing.T) {
	l := NewLocal()
	ctx := context.Background()

	t.Setenv("XDG_RUNTIME_DIR", "relative/dir")

	runtimeDir, err := l.RuntimeDir(ctx)
	if err != nil {
		t.Fatalf("RuntimeDir failed: %v", err)
	}

	// Should fall back to /run/user/<uid>
	expected := "/run/user/"
	if !strings.HasPrefix(runtimeDir, expected) {
		t.Errorf("expected %s*, got %q", expected, runtimeDir)
	}
}

func TestLocal_RuntimeDir_EmptyXDG(t *testing.T) {
	l := NewLocal()
	ctx := context.Background()

	t.Setenv("XDG_RUNTIME_DIR", "")

	runtimeDir, err := l.RuntimeDir(ctx)
	if err != nil {
		t.Fatalf("RuntimeDir failed: %v", err)
	}

	// Should fall back to /run/user/<uid>
	expected := "/run/user/"
	if !strings.HasPrefix(runtimeDir, expected) {
		t.Errorf("expected %s*, got %q", expected, runtimeDir)
	}
}

func TestEnvAssignments(t *testing.T) {
	tests := []struct {
		name  string
		input map[string]string
		want  []string
	}{
		{
			name:  "empty map",
			input: map[string]string{},
			want:  nil,
		},
		{
			name: "single entry",
			input: map[string]string{
				"FOO": "bar",
			},
			want: []string{"FOO=bar"},
		},
		{
			name: "three entries sorted by key",
			input: map[string]string{
				"C": "3",
				"A": "1",
				"B": "2",
			},
			want: []string{"A=1", "B=2", "C=3"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := envAssignments(tt.input)
			if len(got) != len(tt.want) {
				t.Errorf("expected %d entries, got %d", len(tt.want), len(got))
			}
			for i, v := range got {
				if i >= len(tt.want) || v != tt.want[i] {
					t.Errorf("at index %d: expected %q, got %q", i, tt.want[i], v)
				}
			}
		})
	}
}
