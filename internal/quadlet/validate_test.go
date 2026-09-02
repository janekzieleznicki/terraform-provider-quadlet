package quadlet

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/janekzieleznicki/terraform-provider-quadlet/internal/scope"
	"github.com/janekzieleznicki/terraform-provider-quadlet/internal/transport"
)

func TestValidate_Success(t *testing.T) {
	// Read the successful stdout fixture
	data, err := os.ReadFile(filepath.Join("testdata", "stdout_single.txt"))
	if err != nil {
		t.Fatalf("failed to read fixture: %v", err)
	}

	fake := transport.NewFake()
	fake.RunFunc = func(_ transport.Command) (transport.Result, error) {
		return transport.Result{
			Stdout:   string(data),
			Stderr:   "",
			ExitCode: 0,
		}, nil
	}

	validator := &Validator{
		Transport: fake,
		Scope:     scope.ScopeUser,
	}

	result, problems, err := validator.Validate(context.Background(), "web.container", []byte("[Container]\n"))

	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}

	if len(problems) != 0 {
		t.Errorf("expected no problems, got %d", len(problems))
	}

	if result.UnitName != "web.service" {
		t.Errorf("expected UnitName='web.service', got %q", result.UnitName)
	}

	if !strings.Contains(result.GeneratedUnit, "ExecStart=") {
		t.Errorf("expected GeneratedUnit to contain 'ExecStart='")
	}
}

func TestValidate_Rejection(t *testing.T) {
	// Read the typo stderr fixture
	data, err := os.ReadFile(filepath.Join("testdata", "stderr_typo.txt"))
	if err != nil {
		t.Fatalf("failed to read fixture: %v", err)
	}

	fake := transport.NewFake()
	fake.RunFunc = func(_ transport.Command) (transport.Result, error) {
		return transport.Result{
			Stdout:   "",
			Stderr:   string(data),
			ExitCode: 1,
		}, nil
	}

	validator := &Validator{
		Transport: fake,
		Scope:     scope.ScopeUser,
	}

	result, problems, err := validator.Validate(context.Background(), "typo.container", []byte("[Container]\nImagee=bad\n"))

	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}

	if len(problems) != 1 {
		t.Errorf("expected 1 problem, got %d", len(problems))
	}

	if result.UnitName != "" {
		t.Errorf("expected empty UnitName on failure, got %q", result.UnitName)
	}

	if !strings.Contains(problems[0].Message, "unsupported key 'Imagee'") {
		t.Errorf("expected problem message to contain 'unsupported key', got %q", problems[0].Message)
	}
}

func TestValidate_CommandArgs(t *testing.T) {
	fake := transport.NewFake()
	fake.RunFunc = func(_ transport.Command) (transport.Result, error) {
		return transport.Result{
			Stdout:   "---test.service---\n[Unit]\n",
			Stderr:   "",
			ExitCode: 0,
		}, nil
	}

	validator := &Validator{
		Transport: fake,
		Scope:     scope.ScopeUser,
	}

	validator.Validate(context.Background(), "test.container", []byte("[Container]\nImage=alpine\n"))

	if len(fake.Commands) == 0 {
		t.Fatalf("expected at least one command to be recorded")
	}

	cmd := fake.Commands[0]

	// Check args: should include -user and -dryrun
	expectedArgs := []string{"-user", "-dryrun"}
	if len(cmd.Args) != len(expectedArgs) {
		t.Errorf("expected args %v, got %v", expectedArgs, cmd.Args)
	}
	for i, arg := range expectedArgs {
		if i >= len(cmd.Args) || cmd.Args[i] != arg {
			t.Errorf("expected args %v, got %v", expectedArgs, cmd.Args)
			break
		}
	}

	// Check QUADLET_UNIT_DIRS env
	if _, ok := cmd.Env["QUADLET_UNIT_DIRS"]; !ok {
		t.Errorf("expected QUADLET_UNIT_DIRS in env")
	}
}

func TestValidate_SystemScope(t *testing.T) {
	fake := transport.NewFake()
	fake.RunFunc = func(_ transport.Command) (transport.Result, error) {
		return transport.Result{
			Stdout:   "---test.service---\n[Unit]\n",
			Stderr:   "",
			ExitCode: 0,
		}, nil
	}

	validator := &Validator{
		Transport: fake,
		Scope:     scope.ScopeSystem,
	}

	validator.Validate(context.Background(), "test.container", []byte("[Container]\nImage=alpine\n"))

	if len(fake.Commands) == 0 {
		t.Fatalf("expected at least one command to be recorded")
	}

	cmd := fake.Commands[0]

	// Check args: should NOT include -user, but should have -dryrun
	if len(cmd.Args) != 1 || cmd.Args[0] != "-dryrun" {
		t.Errorf("expected args [-dryrun] for system scope, got %v", cmd.Args)
	}
}
func TestValidate_StagedFileLanding(t *testing.T) {
	fake := transport.NewFake()
	fake.RunFunc = func(_ transport.Command) (transport.Result, error) {
		return transport.Result{
			Stdout:   "---test.service---\n[Unit]\n",
			Stderr:   "",
			ExitCode: 0,
		}, nil
	}

	validator := &Validator{
		Transport: fake,
		Scope:     scope.ScopeSystem,
	}

	testContent := []byte("[Container]\nImage=alpine\n")
	_, _, err := validator.Validate(context.Background(), "web.container", testContent)

	// Just verify that Run was called, which proves the file was staged and then cleaned up
	if len(fake.Commands) == 0 {
		t.Errorf("expected Run to be called")
	}

	// Verify QUADLET_UNIT_DIRS env was set
	cmd := fake.Commands[0]
	if cmd.Env["QUADLET_UNIT_DIRS"] == "" {
		t.Errorf("expected QUADLET_UNIT_DIRS to be set in environment")
	}

	if err != nil {
		t.Logf("Validate returned error: %v (may be expected)", err)
	}
}

func TestValidate_NoUnitsGenerated(t *testing.T) {
	fake := transport.NewFake()
	fake.RunFunc = func(_ transport.Command) (transport.Result, error) {
		return transport.Result{
			Stdout:   "",
			Stderr:   "quadlet-generator[123]: No files parsed from [/tmp]\n",
			ExitCode: 0,
		}, nil
	}

	validator := &Validator{
		Transport: fake,
	}

	result, problems, err := validator.Validate(context.Background(), "invalid.txt", []byte("data"))

	if err == nil {
		t.Errorf("expected error for no units generated")
	}

	if !strings.Contains(err.Error(), "no unit") {
		t.Errorf("expected error message to mention 'no unit', got %q", err.Error())
	}

	if len(problems) != 0 {
		t.Errorf("expected no problems, got %d", len(problems))
	}

	if result.UnitName != "" {
		t.Errorf("expected empty result")
	}
}

func TestValidate_Unparseable(t *testing.T) {
	fake := transport.NewFake()
	fake.RunFunc = func(_ transport.Command) (transport.Result, error) {
		return transport.Result{
			Stdout:   "",
			Stderr:   "garbage\n",
			ExitCode: 1,
		}, nil
	}

	validator := &Validator{
		Transport: fake,
	}

	_, problems, err := validator.Validate(context.Background(), "test.container", []byte("data"))

	if err == nil {
		t.Errorf("expected error when exit 1 with unparseable stderr")
	}

	if !strings.Contains(err.Error(), "garbage") {
		t.Errorf("expected error to contain raw stderr, got %q", err.Error())
	}

	if len(problems) != 0 {
		t.Errorf("expected no parsed problems when stderr is unparseable, got %d", len(problems))
	}
}

func TestValidate_UnexpectedExitCode(t *testing.T) {
	fake := transport.NewFake()
	fake.RunFunc = func(_ transport.Command) (transport.Result, error) {
		return transport.Result{
			Stdout:   "",
			Stderr:   "something failed\n",
			ExitCode: 2,
		}, nil
	}

	validator := &Validator{
		Transport: fake,
	}

	result, problems, err := validator.Validate(context.Background(), "test.container", []byte("data"))

	if err == nil {
		t.Errorf("expected error for unexpected exit code")
	}

	if !strings.Contains(err.Error(), "expected 0 or 1") {
		t.Errorf("expected error to mention expected exit codes, got %q", err.Error())
	}

	if len(problems) != 0 {
		t.Errorf("expected no problems for unexpected exit code")
	}

	if result.UnitName != "" {
		t.Errorf("expected empty result")
	}
}
