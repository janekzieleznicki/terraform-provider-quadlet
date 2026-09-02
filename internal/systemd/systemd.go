// Package systemd is WHAT command runs: it drives the systemctl CLI over a
// transport that may be local or remote.
package systemd

import (
	"context"
	"fmt"
	"strings"

	"github.com/janekzieleznicki/terraform-provider-quadlet/internal/scope"
	"github.com/janekzieleznicki/terraform-provider-quadlet/internal/transport"
)

// DefaultSystemctl is the default path to the systemctl binary.
const (
	DefaultSystemctl  = "/usr/bin/systemctl"
	DefaultJournalctl = "/usr/bin/journalctl"
)

// UnitStatus is the subset of systemctl show properties the provider needs.
type UnitStatus struct {
	LoadState     string // "loaded", "not-found", "masked"
	ActiveState   string // "active", "inactive", "failed", "activating"
	SubState      string // "running", "exited", "dead"
	UnitFileState string // "generated" for quadlet units; empty when not found
}

// Exists reports whether systemd knows the unit. systemctl show exits 0 even
// for a unit that does not exist, so LoadState is the only discriminator.
func (s UnitStatus) Exists() bool { return s.LoadState != "" && s.LoadState != "not-found" }

// IsActive is the success predicate after a restart. It holds for every
// quadlet unit type: .container is Type=notify, .volume/.image/.network are
// Type=oneshot with RemainAfterExit=yes, and .pod is Type=forking.
func (s UnitStatus) IsActive() bool { return s.ActiveState == "active" }

// Manager controls systemd units via systemctl and journalctl commands.
type Manager struct {
	Transport  transport.Transport
	Scope      scope.Scope
	Systemctl  string // defaults to DefaultSystemctl when empty
	Journalctl string // defaults to DefaultJournalctl when empty
	Sudo       bool   // run systemctl/journalctl under "sudo -n --"
}

// env returns the extra environment for a command. User scope requires
// XDG_RUNTIME_DIR or systemctl cannot reach the user bus; system scope needs
// nothing.
func (m *Manager) env(ctx context.Context) (map[string]string, error) {
	if m.Scope != scope.ScopeUser {
		return nil, nil
	}
	rd, err := m.Transport.RuntimeDir(ctx)
	if err != nil {
		return nil, fmt.Errorf("resolve runtime dir: %w", err)
	}
	return map[string]string{"XDG_RUNTIME_DIR": rd}, nil
}

// DaemonReload runs systemctl daemon-reload.
func (m *Manager) DaemonReload(ctx context.Context) error {
	systemctl := m.Systemctl
	if systemctl == "" {
		systemctl = DefaultSystemctl
	}

	env, err := m.env(ctx)
	if err != nil {
		return err
	}

	args := append(scope.SystemctlArgs(m.Scope), "daemon-reload")
	res, err := m.Transport.Run(ctx, transport.Command{
		Path: systemctl,
		Args: args,
		Env:  env,
		Sudo: m.Sudo,
	})
	if err != nil {
		return err
	}

	if res.ExitCode != 0 {
		return fmt.Errorf("systemctl daemon-reload: exit %d: %s", res.ExitCode, strings.TrimSpace(res.Stderr))
	}

	return nil
}

// Restart restarts a unit (or starts it if inactive).
func (m *Manager) Restart(ctx context.Context, unit string) error {
	systemctl := m.Systemctl
	if systemctl == "" {
		systemctl = DefaultSystemctl
	}

	env, err := m.env(ctx)
	if err != nil {
		return err
	}

	args := append(scope.SystemctlArgs(m.Scope), "restart", unit)
	res, err := m.Transport.Run(ctx, transport.Command{
		Path: systemctl,
		Args: args,
		Env:  env,
		Sudo: m.Sudo,
	})
	if err != nil {
		return err
	}

	if res.ExitCode != 0 {
		return fmt.Errorf("systemctl restart: exit %d: %s", res.ExitCode, strings.TrimSpace(res.Stderr))
	}

	return nil
}

// Stop stops a unit.
func (m *Manager) Stop(ctx context.Context, unit string) error {
	systemctl := m.Systemctl
	if systemctl == "" {
		systemctl = DefaultSystemctl
	}

	env, err := m.env(ctx)
	if err != nil {
		return err
	}

	args := append(scope.SystemctlArgs(m.Scope), "stop", unit)
	res, err := m.Transport.Run(ctx, transport.Command{
		Path: systemctl,
		Args: args,
		Env:  env,
		Sudo: m.Sudo,
	})
	if err != nil {
		return err
	}

	if res.ExitCode != 0 {
		return fmt.Errorf("systemctl stop: exit %d: %s", res.ExitCode, strings.TrimSpace(res.Stderr))
	}

	return nil
}

// Show returns the status of a unit.
func (m *Manager) Show(ctx context.Context, unit string) (UnitStatus, error) {
	systemctl := m.Systemctl
	if systemctl == "" {
		systemctl = DefaultSystemctl
	}

	env, err := m.env(ctx)
	if err != nil {
		return UnitStatus{}, err
	}

	args := append(scope.SystemctlArgs(m.Scope), "show", "--property=LoadState,ActiveState,SubState,UnitFileState", unit)
	res, err := m.Transport.Run(ctx, transport.Command{
		Path: systemctl,
		Args: args,
		Env:  env,
		Sudo: m.Sudo,
	})
	if err != nil {
		return UnitStatus{}, err
	}

	// Show doesn't fail on exit code, so parse unconditionally
	props := ParseShow(res.Stdout)
	return UnitStatus{
		LoadState:     props["LoadState"],
		ActiveState:   props["ActiveState"],
		SubState:      props["SubState"],
		UnitFileState: props["UnitFileState"],
	}, nil
}

// Cat returns the generated unit file content.
func (m *Manager) Cat(ctx context.Context, unit string) (string, error) {
	systemctl := m.Systemctl
	if systemctl == "" {
		systemctl = DefaultSystemctl
	}

	env, err := m.env(ctx)
	if err != nil {
		return "", err
	}

	args := append(scope.SystemctlArgs(m.Scope), "cat", unit)
	res, err := m.Transport.Run(ctx, transport.Command{
		Path: systemctl,
		Args: args,
		Env:  env,
		Sudo: m.Sudo,
	})
	if err != nil {
		return "", err
	}

	if res.ExitCode != 0 {
		return "", fmt.Errorf("systemctl cat: exit %d: %s", res.ExitCode, strings.TrimSpace(res.Stderr))
	}

	return res.Stdout, nil
}

// Journal returns recent journal lines for a unit (best-effort).
func (m *Manager) Journal(ctx context.Context, unit string, lines int) (string, error) {
	journalctl := m.Journalctl
	if journalctl == "" {
		journalctl = DefaultJournalctl
	}

	env, err := m.env(ctx)
	if err != nil {
		return "", err
	}

	args := append(scope.SystemctlArgs(m.Scope), "-u", unit, "-n", fmt.Sprintf("%d", lines), "--no-pager")
	res, err := m.Transport.Run(ctx, transport.Command{
		Path: journalctl,
		Args: args,
		Env:  env,
		Sudo: m.Sudo,
	})
	if err != nil {
		return "", err
	}

	if res.ExitCode != 0 {
		return "", fmt.Errorf("journalctl failed")
	}

	return res.Stdout, nil
}

// ParseShow turns "Key=Value" lines into a map. It splits on the FIRST "="
// only, because values legitimately contain "=" (Environment=FOO=bar), and it
// is order-independent, because systemctl returns properties in its own order
// rather than the requested one. Lines without "=" are ignored.
func ParseShow(stdout string) map[string]string {
	result := make(map[string]string)
	for _, line := range strings.Split(stdout, "\n") {
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) == 2 {
			result[parts[0]] = parts[1]
		}
	}
	return result
}

// StripCatHeader removes the contiguous leading block of "#" comment lines that
// systemctl cat prepends (generated-file path and generator banner) and
// normalises the result to exactly one trailing newline.
func StripCatHeader(s string) string {
	lines := strings.Split(s, "\n")

	// Find the first non-comment line
	firstNonComment := 0
	for i, line := range lines {
		if !strings.HasPrefix(line, "#") {
			firstNonComment = i
			break
		}
	}

	// Join from first non-comment line onward
	result := strings.Join(lines[firstNonComment:], "\n")

	// Normalize to exactly one trailing newline
	result = strings.TrimRight(result, "\n")
	if result != "" {
		result += "\n"
	}

	return result
}
