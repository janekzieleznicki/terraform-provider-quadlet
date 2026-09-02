package quadlet

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseGeneratedUnits_Single(t *testing.T) {
	// Read the test fixture
	data, err := os.ReadFile(filepath.Join("testdata", "stdout_single.txt"))
	if err != nil {
		t.Fatalf("failed to read fixture: %v", err)
	}

	units := ParseGeneratedUnits(string(data))

	if len(units) != 1 {
		t.Errorf("expected 1 unit, got %d", len(units))
	}

	if units[0].Name != "web.service" {
		t.Errorf("expected Name='web.service', got '%s'", units[0].Name)
	}

	if !strings.Contains(units[0].Body, "ExecStart=") {
		t.Errorf("expected Body to contain 'ExecStart=', got %q", units[0].Body)
	}

	// Body must end with exactly one newline
	if !strings.HasSuffix(units[0].Body, "\n") {
		t.Errorf("expected Body to end with newline")
	}
	if strings.HasSuffix(units[0].Body, "\n\n") {
		t.Errorf("expected Body to end with exactly one newline, got double newline")
	}
}

func TestParseGeneratedUnits_Multiple(t *testing.T) {
	stdout := `---first.service---
[Unit]
Description=first

---second.service---
[Unit]
Description=second
`
	units := ParseGeneratedUnits(stdout)

	if len(units) != 2 {
		t.Errorf("expected 2 units, got %d", len(units))
	}

	if units[0].Name != "first.service" {
		t.Errorf("expected first unit Name='first.service', got '%s'", units[0].Name)
	}

	if units[1].Name != "second.service" {
		t.Errorf("expected second unit Name='second.service', got '%s'", units[1].Name)
	}
}

func TestParseGeneratedUnits_Empty(t *testing.T) {
	units := ParseGeneratedUnits("")

	if len(units) != 0 {
		t.Errorf("expected 0 units, got %d", len(units))
	}
}

func TestParseProblems_Typo(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("testdata", "stderr_typo.txt"))
	if err != nil {
		t.Fatalf("failed to read fixture: %v", err)
	}

	problems := ParseProblems(string(data))

	if len(problems) != 1 {
		t.Errorf("expected 1 problem, got %d", len(problems))
	}

	if problems[0].File != "typo.container" {
		t.Errorf("expected File='typo.container', got '%s'", problems[0].File)
	}

	if !strings.Contains(problems[0].Message, "unsupported key 'Imagee'") {
		t.Errorf("expected message to contain \"unsupported key 'Imagee'\", got %q", problems[0].Message)
	}
}

func TestParseProblems_NoImage(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("testdata", "stderr_noimage.txt"))
	if err != nil {
		t.Fatalf("failed to read fixture: %v", err)
	}

	problems := ParseProblems(string(data))

	if len(problems) != 1 {
		t.Errorf("expected 1 problem, got %d", len(problems))
	}

	if problems[0].File != "noimage.container" {
		t.Errorf("expected File='noimage.container', got '%s'", problems[0].File)
	}

	if !strings.Contains(problems[0].Message, "no Image or Rootfs key specified") {
		t.Errorf("expected message to contain 'no Image or Rootfs key specified', got %q", problems[0].Message)
	}
}

func TestParseProblems_Malformed(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("testdata", "stderr_malformed.txt"))
	if err != nil {
		t.Fatalf("failed to read fixture: %v", err)
	}

	problems := ParseProblems(string(data))

	if len(problems) != 1 {
		t.Errorf("expected 1 problem, got %d", len(problems))
	}

	if problems[0].File != "broken.container" {
		t.Errorf("expected File='broken.container', got '%s'", problems[0].File)
	}

	if !strings.Contains(problems[0].Message, "not a key-value pair") {
		t.Errorf("expected message to contain 'not a key-value pair', got %q", problems[0].Message)
	}
}
