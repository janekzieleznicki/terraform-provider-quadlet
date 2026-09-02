package transport

import (
	"context"
	"errors"
	"testing"
)

func TestFake_WriteAndReadFile(t *testing.T) {
	f := NewFake()
	ctx := context.Background()
	testData := []byte("hello world")
	testPath := "/tmp/test.txt"

	// Write
	if err := f.WriteFile(ctx, testPath, testData, 0o644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	// Read
	data, err := f.ReadFile(ctx, testPath)
	if err != nil {
		t.Fatalf("ReadFile failed: %v", err)
	}

	if string(data) != string(testData) {
		t.Errorf("expected %q, got %q", testData, data)
	}
}

func TestFake_ReadFileNotExist(t *testing.T) {
	f := NewFake()
	ctx := context.Background()

	_, err := f.ReadFile(ctx, "/nonexistent/file")
	if !errors.Is(err, ErrNotExist) {
		t.Errorf("expected ErrNotExist, got %v", err)
	}
}

func TestFake_RemoveAll(t *testing.T) {
	f := NewFake()
	ctx := context.Background()

	// Set up directory structure with files
	if err := f.MkdirAll(ctx, "/tmp/dir", 0o755); err != nil {
		t.Fatalf("MkdirAll failed: %v", err)
	}
	if err := f.WriteFile(ctx, "/tmp/dir/file1.txt", []byte("data1"), 0o644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}
	if err := f.WriteFile(ctx, "/tmp/dir/file2.txt", []byte("data2"), 0o644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	// RemoveAll should delete the directory and all files under it
	if err := f.RemoveAll(ctx, "/tmp/dir"); err != nil {
		t.Fatalf("RemoveAll failed: %v", err)
	}

	// Verify files are gone
	if _, err := f.ReadFile(ctx, "/tmp/dir/file1.txt"); !errors.Is(err, ErrNotExist) {
		t.Errorf("expected file1 to be deleted")
	}
	if _, err := f.ReadFile(ctx, "/tmp/dir/file2.txt"); !errors.Is(err, ErrNotExist) {
		t.Errorf("expected file2 to be deleted")
	}
}

func TestFake_MkdirTemp(t *testing.T) {
	f := NewFake()
	ctx := context.Background()

	// Create two temp directories
	dir1, err := f.MkdirTemp(ctx, "test-*-dir")
	if err != nil {
		t.Fatalf("MkdirTemp failed: %v", err)
	}

	dir2, err := f.MkdirTemp(ctx, "test-*-dir")
	if err != nil {
		t.Fatalf("MkdirTemp failed: %v", err)
	}

	// Verify they are distinct
	if dir1 == dir2 {
		t.Errorf("expected distinct directories, got %q and %q", dir1, dir2)
	}

	// Verify they are absolute (start with /)
	if !isAbsolute(dir1) || !isAbsolute(dir2) {
		t.Errorf("expected absolute paths, got %q and %q", dir1, dir2)
	}
}

func TestFake_RunRecordsCommands(t *testing.T) {
	f := NewFake()
	ctx := context.Background()

	cmd := Command{
		Path: "/bin/echo",
		Args: []string{"hello", "world"},
		Env:  map[string]string{"FOO": "bar"},
	}

	// Mutate the original args and env after Run to verify deep copy
	origArgs := cmd.Args
	origEnv := cmd.Env
	f.Run(ctx, cmd)
	origArgs[0] = "mutated"
	origEnv["FOO"] = "mutated"

	// Check that the recorded command is unchanged
	if len(f.Commands) != 1 {
		t.Fatalf("expected 1 command, got %d", len(f.Commands))
	}
	recorded := f.Commands[0]
	if recorded.Args[0] != "hello" {
		t.Errorf("expected args[0]='hello', got '%s' (deep copy failed)", recorded.Args[0])
	}
	if recorded.Env["FOO"] != "bar" {
		t.Errorf("expected Env[FOO]='bar', got '%s' (deep copy failed)", recorded.Env["FOO"])
	}
}

func TestFake_UserConfigDir(t *testing.T) {
	f := NewFake()
	ctx := context.Background()

	// Test default
	configDir, err := f.UserConfigDir(ctx)
	if err != nil {
		t.Fatalf("UserConfigDir failed: %v", err)
	}
	if configDir != "/home/fake/.config" {
		t.Errorf("expected /home/fake/.config, got %q", configDir)
	}

	// Test overridden value
	f.ConfigDir = "/custom/config"
	configDir, err = f.UserConfigDir(ctx)
	if err != nil {
		t.Fatalf("UserConfigDir failed: %v", err)
	}
	if configDir != "/custom/config" {
		t.Errorf("expected /custom/config, got %q", configDir)
	}
}

func TestFake_RuntimeDir(t *testing.T) {
	f := NewFake()
	ctx := context.Background()

	// Test default
	runtimeDir, err := f.RuntimeDir(ctx)
	if err != nil {
		t.Fatalf("RuntimeDir failed: %v", err)
	}
	if runtimeDir != "/run/user/1000" {
		t.Errorf("expected /run/user/1000, got %q", runtimeDir)
	}

	// Test overridden value
	f.RuntimeDirPath = "/custom/runtime"
	runtimeDir, err = f.RuntimeDir(ctx)
	if err != nil {
		t.Fatalf("RuntimeDir failed: %v", err)
	}
	if runtimeDir != "/custom/runtime" {
		t.Errorf("expected /custom/runtime, got %q", runtimeDir)
	}
}

func isAbsolute(p string) bool {
	return len(p) > 0 && p[0] == '/'
}
