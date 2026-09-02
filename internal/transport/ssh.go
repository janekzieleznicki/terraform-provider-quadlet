package transport

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
	"golang.org/x/crypto/ssh/knownhosts"
)

// SSHConfig holds SSH connection parameters.
type SSHConfig struct {
	Host           string        // hostname or address; required
	Port           int           // defaults to 22
	User           string        // required
	PrivateKey     []byte        // PEM; when empty, the ssh-agent at $SSH_AUTH_SOCK is used
	Passphrase     []byte        // optional, for an encrypted PrivateKey
	KnownHostsPath string        // defaults to $HOME/.ssh/known_hosts
	Insecure       bool          // skip host key verification
	Timeout        time.Duration // dial timeout; defaults to 30s
}

// SSH is a Transport implementation using SSH/SFTP.
type SSH struct {
	client *ssh.Client
	sftp   *sftp.Client

	mu         sync.Mutex
	configDir  string // cached UserConfigDir
	runtimeDir string // cached RuntimeDir
}

// NewSSH establishes an SSH connection and returns a Transport implementation.
func NewSSH(_ context.Context, cfg SSHConfig) (*SSH, error) {
	if cfg.Host == "" {
		return nil, errors.New("ssh: Host is required")
	}
	if cfg.User == "" {
		return nil, errors.New("ssh: User is required")
	}

	if cfg.Port == 0 {
		cfg.Port = 22
	}
	if cfg.Timeout == 0 {
		cfg.Timeout = 30 * time.Second
	}

	// Get auth methods
	authMethods, err := authMethods(cfg)
	if err != nil {
		return nil, err
	}

	// Get host key callback
	hostKeyCallback, err := hostKeyCallback(cfg)
	if err != nil {
		return nil, err
	}

	// Connect
	addr := net.JoinHostPort(cfg.Host, strconv.Itoa(cfg.Port))
	sshConfig := &ssh.ClientConfig{
		User:              cfg.User,
		Auth:              authMethods,
		HostKeyCallback:   hostKeyCallback,
		Timeout:           cfg.Timeout,
		HostKeyAlgorithms: []string{ssh.KeyAlgoRSA, ssh.KeyAlgoECDSA256, ssh.KeyAlgoED25519},
	}

	client, err := ssh.Dial("tcp", addr, sshConfig)
	if err != nil {
		return nil, fmt.Errorf("ssh dial %s: %w", addr, err)
	}

	// Open SFTP
	sftpClient, err := sftp.NewClient(client)
	if err != nil {
		_ = client.Close() //nolint:errcheck
		return nil, fmt.Errorf("sftp open: %w", err)
	}

	return &SSH{
		client: client,
		sftp:   sftpClient,
	}, nil
}

// Run executes a command over SSH.
func (s *SSH) Run(ctx context.Context, c Command) (Result, error) {
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}

	sess, err := s.client.NewSession()
	if err != nil {
		return Result{}, fmt.Errorf("ssh session: %w", err)
	}
	_ = sess.Close() //nolint:errcheck

	var stdout, stderr bytes.Buffer
	sess.Stdout = &stdout
	sess.Stderr = &stderr

	cmdStr := buildRemoteCommand(c)
	err = sess.Run(cmdStr)

	if err == nil {
		return Result{
			Stdout:   stdout.String(),
			Stderr:   stderr.String(),
			ExitCode: 0,
		}, nil
	}

	// Check for exit error
	if ee, ok := err.(*ssh.ExitError); ok {
		return Result{
			Stdout:   stdout.String(),
			Stderr:   stderr.String(),
			ExitCode: ee.ExitStatus(),
		}, nil
	}

	return Result{}, fmt.Errorf("ssh run: %w", err)
}

// WriteFile writes data to a file over SFTP.
func (s *SSH) WriteFile(ctx context.Context, path string, data []byte, mode os.FileMode) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	f, err := s.sftp.Create(path)
	if err != nil {
		return fmt.Errorf("ssh create %s: %w", path, err)
	}
	_ = f.Close() //nolint:errcheck

	if _, err := f.Write(data); err != nil {
		return fmt.Errorf("ssh write %s: %w", path, err)
	}

	if err := s.sftp.Chmod(path, mode); err != nil {
		return fmt.Errorf("ssh chmod %s: %w", path, err)
	}

	return nil
}

// ReadFile reads a file over SFTP.
func (s *SSH) ReadFile(ctx context.Context, path string) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	f, err := s.sftp.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("%s: %w", path, ErrNotExist)
		}
		return nil, fmt.Errorf("ssh open %s: %w", path, err)
	}
	_ = f.Close() //nolint:errcheck

	data, err := io.ReadAll(f)
	if err != nil {
		return nil, fmt.Errorf("ssh read %s: %w", path, err)
	}

	return data, nil
}

// Remove deletes a file over SFTP.
func (s *SSH) Remove(ctx context.Context, path string) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	err := s.sftp.Remove(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("%s: %w", path, ErrNotExist)
		}
		return fmt.Errorf("ssh remove %s: %w", path, err)
	}

	return nil
}

// RemoveAll deletes a directory tree over SFTP.
func (s *SSH) RemoveAll(ctx context.Context, path string) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	// Note: RemoveAll behavior on missing paths - SFTP doesn't fail on missing paths
	err := s.sftp.RemoveAll(path)
	if err != nil {
		// Still return nil if path doesn't exist
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("ssh removeall %s: %w", path, err)
	}

	return nil
}

// MkdirAll creates a directory tree over SFTP.
func (s *SSH) MkdirAll(ctx context.Context, path string, mode os.FileMode) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	if err := s.sftp.MkdirAll(path); err != nil {
		return fmt.Errorf("ssh mkdir %s: %w", path, err)
	}

	if err := s.sftp.Chmod(path, mode); err != nil {
		return fmt.Errorf("ssh chmod %s: %w", path, err)
	}

	return nil
}

// MkdirTemp creates a temporary directory over SFTP.
func (s *SSH) MkdirTemp(ctx context.Context, pattern string) (string, error) {
	res, err := s.runShell(ctx, "mktemp -d "+shellQuote(mktempTemplate(pattern)))
	if err != nil {
		return "", err
	}

	if res.ExitCode != 0 {
		return "", fmt.Errorf("mktemp failed: %s", res.Stderr)
	}

	dir := strings.TrimSpace(res.Stdout)
	if dir == "" {
		return "", fmt.Errorf("mktemp produced empty output")
	}

	if !path.IsAbs(dir) {
		return "", fmt.Errorf("mktemp produced non-absolute path: %s", dir)
	}

	return dir, nil
}

// UserConfigDir returns the user config directory via SSH.
func (s *SSH) UserConfigDir(ctx context.Context) (string, error) {
	s.mu.Lock()
	if s.configDir != "" {
		defer s.mu.Unlock()
		return s.configDir, nil
	}
	s.mu.Unlock()

	res, err := s.runShell(ctx, `printf %s "${XDG_CONFIG_HOME:-$HOME/.config}"`)
	if err != nil {
		return "", err
	}

	if res.ExitCode != 0 {
		return "", fmt.Errorf("userConfigDir failed: %s", res.Stderr)
	}

	dir := res.Stdout
	s.mu.Lock()
	s.configDir = dir
	s.mu.Unlock()

	return dir, nil
}

// RuntimeDir returns the runtime directory via SSH.
func (s *SSH) RuntimeDir(ctx context.Context) (string, error) {
	s.mu.Lock()
	if s.runtimeDir != "" {
		defer s.mu.Unlock()
		return s.runtimeDir, nil
	}
	s.mu.Unlock()

	res, err := s.runShell(ctx, `printf %s "${XDG_RUNTIME_DIR:-/run/user/$(id -u)}"`)
	if err != nil {
		return "", err
	}

	if res.ExitCode != 0 {
		return "", fmt.Errorf("runtimeDir failed: %s", res.Stderr)
	}

	dir := res.Stdout
	s.mu.Lock()
	s.runtimeDir = dir
	s.mu.Unlock()

	return dir, nil
}

// Close closes the SSH connection.
func (s *SSH) Close() error {
	var err1, err2 error

	if s.sftp != nil {
		err1 = s.sftp.Close()
	}

	if s.client != nil {
		err2 = s.client.Close()
	}

	return errors.Join(err1, err2)
}

// runShell runs a raw shell script, bypassing command building
func (s *SSH) runShell(ctx context.Context, script string) (Result, error) {
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}

	sess, err := s.client.NewSession()
	if err != nil {
		return Result{}, fmt.Errorf("ssh session: %w", err)
	}
	_ = sess.Close() //nolint:errcheck

	var stdout, stderr bytes.Buffer
	sess.Stdout = &stdout
	sess.Stderr = &stderr

	err = sess.Run(script)
	if err == nil {
		return Result{
			Stdout:   stdout.String(),
			Stderr:   stderr.String(),
			ExitCode: 0,
		}, nil
	}

	if ee, ok := err.(*ssh.ExitError); ok {
		return Result{
			Stdout:   stdout.String(),
			Stderr:   stderr.String(),
			ExitCode: ee.ExitStatus(),
		}, nil
	}

	return Result{}, fmt.Errorf("ssh run: %w", err)
}

// shellQuote wraps s in single quotes, escaping embedded single quotes
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}

// buildRemoteCommand renders a Command as one shell string
func buildRemoteCommand(c Command) string {
	var parts []string

	parts = append(parts, "sudo", "-n", "--")

	if len(c.Env) > 0 {
		parts = append(parts, "env")
		parts = append(parts, envAssignments(c.Env)...)
		parts = append(parts, "--")
	}

	parts = append(parts, c.Path)
	parts = append(parts, c.Args...)

	// Quote all parts
	var sb strings.Builder
	for i, part := range parts {
		if i > 0 {
			sb.WriteString(" ")
		}
		sb.WriteString(shellQuote(part))
	}

	return sb.String()
}

// mktempTemplate converts a Go pattern into a mktemp template
func mktempTemplate(pattern string) string {
	// Go uses *, mktemp uses X
	// Remove all * and append X's
	base := strings.ReplaceAll(pattern, "*", "")
	if base == "" {
		base = "tmp"
	}
	return "/tmp/" + base + "-XXXXXXXXXX"
}

// authMethods returns authentication methods for SSH
func authMethods(cfg SSHConfig) ([]ssh.AuthMethod, error) {
	if len(cfg.PrivateKey) > 0 {
		var signer ssh.Signer
		var err error

		if len(cfg.Passphrase) > 0 {
			signer, err = ssh.ParsePrivateKeyWithPassphrase(cfg.PrivateKey, cfg.Passphrase)
		} else {
			signer, err = ssh.ParsePrivateKey(cfg.PrivateKey)
		}

		if err != nil {
			return nil, fmt.Errorf("parse private key: %w", err)
		}

		return []ssh.AuthMethod{ssh.PublicKeys(signer)}, nil
	}

	// Try ssh-agent
	sock := os.Getenv("SSH_AUTH_SOCK")
	if sock == "" {
		return nil, errors.New("ssh: no authentication available: set PrivateKey or run an ssh-agent")
	}

	conn, err := net.Dial("unix", sock)
	if err != nil {
		return nil, fmt.Errorf("ssh-agent connect: %w", err)
	}

	agentClient := agent.NewClient(conn)
	return []ssh.AuthMethod{ssh.PublicKeysCallback(agentClient.Signers)}, nil
}

// hostKeyCallback returns host key verification callback
func hostKeyCallback(cfg SSHConfig) (ssh.HostKeyCallback, error) {
	if cfg.Insecure {
		return ssh.InsecureIgnoreHostKey(), nil
	}

	knownHostsPath := cfg.KnownHostsPath
	if knownHostsPath == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("get home dir: %w", err)
		}
		knownHostsPath = path.Join(home, ".ssh", "known_hosts")
	}

	callback, err := knownhosts.New(knownHostsPath)
	if err != nil {
		return nil, fmt.Errorf("known_hosts %s: %w", knownHostsPath, err)
	}

	return callback, nil
}

// Compile-time assertion
var _ Transport = (*SSH)(nil)
