// Package remote provides the transport primitives used to inspect and control
// RCOD sessions on another machine over SSH.
package remote

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	DefaultProbeTimeout = 10 * time.Second
	DefaultMaxOutput    = 64 * 1024
	MaxProbeOutput      = 1024 * 1024
	MaxProbeTimeout     = 2 * time.Minute
)

// HostIdentity is the non-secret identity of an SSH host.
type HostIdentity struct {
	ID      string `json:"id"`
	Name    string `json:"name,omitempty"`
	Address string `json:"address"`
	Port    int    `json:"port"`
	User    string `json:"user,omitempty"`
}

// HostConfig describes how to connect to a host. It deliberately contains no
// password, private-key contents, or other credentials. IdentityFile and
// KnownHostsFile are local filesystem paths, never secret material.
type HostConfig struct {
	ID                    string `json:"id" yaml:"id"`
	Name                  string `json:"name,omitempty" yaml:"name,omitempty"`
	Address               string `json:"address" yaml:"address"`
	Port                  int    `json:"port,omitempty" yaml:"port,omitempty"`
	User                  string `json:"user,omitempty" yaml:"user,omitempty"`
	IdentityFile          string `json:"identity_file,omitempty" yaml:"identity_file,omitempty"`
	KnownHostsFile        string `json:"known_hosts_file,omitempty" yaml:"known_hosts_file,omitempty"`
	ConnectTimeoutSeconds int    `json:"connect_timeout_seconds,omitempty" yaml:"connect_timeout_seconds,omitempty"`
	BaseFolder            string `json:"base_folder,omitempty" yaml:"base_folder,omitempty"`
	AgentCommand          string `json:"agent_command,omitempty" yaml:"agent_command,omitempty"`
	AgentWorkDir          string `json:"agent_work_dir,omitempty" yaml:"agent_work_dir,omitempty"`
}

func (c HostConfig) Identity() HostIdentity {
	port := c.Port
	if port == 0 {
		port = 22
	}
	return HostIdentity{
		ID:      c.ID,
		Name:    c.Name,
		Address: c.Address,
		Port:    port,
		User:    c.User,
	}
}

func (c HostConfig) Validate() error {
	if !validIdentifier(c.ID) {
		return fmt.Errorf("host id must contain only letters, digits, '.', '_' or '-'")
	}
	if err := validateDisplayName(c.Name); err != nil {
		return fmt.Errorf("host name: %w", err)
	}
	if err := validateAddress(c.Address); err != nil {
		return fmt.Errorf("host address: %w", err)
	}
	if c.Port < 0 || c.Port > 65535 {
		return fmt.Errorf("host port must be between 0 and 65535 (0 defaults to 22)")
	}
	if err := validateUser(c.User); err != nil {
		return fmt.Errorf("host user: %w", err)
	}
	if err := validatePath(c.IdentityFile, "identity file"); err != nil {
		return err
	}
	if err := validatePath(c.KnownHostsFile, "known hosts file"); err != nil {
		return err
	}
	if c.ConnectTimeoutSeconds < 0 || c.ConnectTimeoutSeconds > 3600 {
		return fmt.Errorf("connect timeout must be between 0 and 3600 seconds")
	}
	if strings.IndexByte(c.BaseFolder, 0) >= 0 || strings.IndexByte(c.AgentWorkDir, 0) >= 0 {
		return errors.New("remote paths cannot contain NUL")
	}
	if strings.IndexByte(c.AgentCommand, 0) >= 0 {
		return errors.New("agent command cannot contain NUL")
	}
	return nil
}

type HostStatus string

const (
	HostStatusUnknown     HostStatus = "unknown"
	HostStatusReachable   HostStatus = "reachable"
	HostStatusUnreachable HostStatus = "unreachable"
)

// HostRuntime is safe to expose in API responses and contains no credentials.
type HostRuntime struct {
	Identity      HostIdentity `json:"identity"`
	Status        HostStatus   `json:"status"`
	LastCheckedAt time.Time    `json:"last_checked_at,omitempty"`
	LatencyMs     int64        `json:"latency_ms,omitempty"`
	LastError     string       `json:"last_error,omitempty"`
}

// ExecRequest describes one remote argv-style command. Command is never
// interpreted by a local shell; each argument is quoted for the remote shell.
type ExecRequest struct {
	WorkingDirectory string
	Command          []string
	MaxOutputBytes   int
	Timeout          time.Duration
}

// ProbeRequest is an inventory/read-only probe with enforced time and output
// bounds. The caller supplies an argv-style command; the transport itself does
// not provide a shell string or credentials to that command.
type ProbeRequest struct {
	WorkingDirectory string
	Command          []string
	MaxOutputBytes   int
	Timeout          time.Duration
}

type CommandResult struct {
	Stdout     string
	Stderr     string
	ExitCode   int
	DurationMs int64
	Truncated  bool
}

// Process is the subset of os/exec.Cmd needed by Executor. It is intentionally
// injectable so transport behavior can be tested without a real SSH server.
type Process interface {
	StdinPipe() (io.WriteCloser, error)
	StdoutPipe() (io.ReadCloser, error)
	StderrPipe() (io.ReadCloser, error)
	Start() error
	Wait() error
	Kill() error
}

type CommandRunner interface {
	CommandContext(ctx context.Context, name string, args ...string) Process
}

type execRunner struct{}

func (execRunner) CommandContext(ctx context.Context, name string, args ...string) Process {
	return &execProcess{Cmd: exec.CommandContext(ctx, name, args...)}
}

type execProcess struct {
	*exec.Cmd
}

func (p *execProcess) Kill() error {
	if p.Process == nil {
		return os.ErrProcessDone
	}
	return p.Process.Kill()
}

type ExecutorOption func(*Executor) error

func WithCommandRunner(runner CommandRunner) ExecutorOption {
	return func(e *Executor) error {
		if runner == nil {
			return errors.New("command runner cannot be nil")
		}
		e.runner = runner
		return nil
	}
}

func WithProbeLimits(timeout time.Duration, maxOutputBytes int) ExecutorOption {
	return func(e *Executor) error {
		if timeout <= 0 || timeout > MaxProbeTimeout {
			return fmt.Errorf("probe timeout must be between 1ns and %s", MaxProbeTimeout)
		}
		if maxOutputBytes <= 0 || maxOutputBytes > MaxProbeOutput {
			return fmt.Errorf("probe output limit must be between 1 and %d bytes", MaxProbeOutput)
		}
		e.probeTimeout = timeout
		e.probeMaxOutput = maxOutputBytes
		return nil
	}
}

type Executor struct {
	host           HostConfig
	runner         CommandRunner
	probeTimeout   time.Duration
	probeMaxOutput int
}

func NewExecutor(host HostConfig, options ...ExecutorOption) (*Executor, error) {
	if err := host.Validate(); err != nil {
		return nil, err
	}
	e := &Executor{
		host:           host,
		runner:         execRunner{},
		probeTimeout:   DefaultProbeTimeout,
		probeMaxOutput: DefaultMaxOutput,
	}
	for _, option := range options {
		if option == nil {
			continue
		}
		if err := option(e); err != nil {
			return nil, err
		}
	}
	return e, nil
}

func (e *Executor) Host() HostConfig {
	return e.host
}

func (e *Executor) Run(ctx context.Context, workingDirectory string, command ...string) (CommandResult, error) {
	return e.Execute(ctx, ExecRequest{WorkingDirectory: workingDirectory, Command: command})
}

func (e *Executor) Execute(ctx context.Context, request ExecRequest) (CommandResult, error) {
	if err := validateRequest(request.Command, request.WorkingDirectory); err != nil {
		return CommandResult{ExitCode: -1}, err
	}
	maxOutput, err := normalizeOutputLimit(request.MaxOutputBytes, MaxProbeOutput)
	if err != nil {
		return CommandResult{ExitCode: -1}, err
	}
	if request.Timeout < 0 {
		return CommandResult{ExitCode: -1}, errors.New("command timeout cannot be negative")
	}
	return e.execute(ctx, request.WorkingDirectory, request.Command, request.Timeout, maxOutput)
}

func (e *Executor) Probe(ctx context.Context, request ProbeRequest) (CommandResult, error) {
	if err := validateRequest(request.Command, request.WorkingDirectory); err != nil {
		return CommandResult{ExitCode: -1}, err
	}
	timeout := request.Timeout
	if timeout <= 0 || timeout > e.probeTimeout {
		timeout = e.probeTimeout
	}
	maxOutput := request.MaxOutputBytes
	if maxOutput <= 0 || maxOutput > e.probeMaxOutput {
		maxOutput = e.probeMaxOutput
	}
	return e.execute(ctx, request.WorkingDirectory, request.Command, timeout, maxOutput)
}

func (e *Executor) execute(ctx context.Context, workingDirectory string, command []string, timeout time.Duration, maxOutput int) (CommandResult, error) {
	if err := ctx.Err(); err != nil {
		return CommandResult{ExitCode: -1}, err
	}

	runCtx := ctx
	cancel := func() {}
	if timeout > 0 {
		runCtx, cancel = context.WithTimeout(ctx, timeout)
	}
	defer cancel()

	process := e.runner.CommandContext(runCtx, "ssh", e.sshArgs(workingDirectory, command)...)
	stdout, err := process.StdoutPipe()
	if err != nil {
		return CommandResult{ExitCode: -1}, fmt.Errorf("opening SSH stdout: %w", err)
	}
	stderr, err := process.StderrPipe()
	if err != nil {
		_ = stdout.Close()
		return CommandResult{ExitCode: -1}, fmt.Errorf("opening SSH stderr: %w", err)
	}
	if err := process.Start(); err != nil {
		_ = stdout.Close()
		_ = stderr.Close()
		if runCtx.Err() != nil {
			return CommandResult{ExitCode: -1}, runCtx.Err()
		}
		return CommandResult{ExitCode: -1}, fmt.Errorf("starting SSH: %w", err)
	}

	startedAt := time.Now()
	stdoutDone := make(chan capturedOutput, 1)
	stderrDone := make(chan capturedOutput, 1)
	go captureOutput(stdout, maxOutput, stdoutDone)
	go captureOutput(stderr, maxOutput, stderrDone)
	waitErr := process.Wait()
	out := <-stdoutDone
	errOut := <-stderrDone
	result := CommandResult{
		Stdout:     out.value,
		Stderr:     errOut.value,
		ExitCode:   -1,
		DurationMs: time.Since(startedAt).Milliseconds(),
		Truncated:  out.truncated || errOut.truncated,
	}

	if code, ok := exitCode(waitErr); ok {
		result.ExitCode = code
		waitErr = nil
	}
	if runCtx.Err() != nil {
		return result, runCtx.Err()
	}
	if waitErr != nil {
		return result, fmt.Errorf("waiting for SSH: %w", waitErr)
	}
	return result, nil
}

func (e *Executor) sshArgs(workingDirectory string, command []string) []string {
	args := []string{"-T", "-o", "BatchMode=yes"}
	if e.host.ConnectTimeoutSeconds > 0 {
		args = append(args, "-o", "ConnectTimeout="+strconv.Itoa(e.host.ConnectTimeoutSeconds))
	}
	port := e.host.Port
	if port == 0 {
		port = 22
	}
	args = append(args, "-p", strconv.Itoa(port))
	if e.host.IdentityFile != "" {
		args = append(args, "-i", e.host.IdentityFile, "-o", "IdentitiesOnly=yes")
	}
	if e.host.KnownHostsFile != "" {
		args = append(args, "-o", "UserKnownHostsFile="+e.host.KnownHostsFile)
	}

	address := e.host.Address
	if strings.Contains(address, ":") && !strings.HasPrefix(address, "[") {
		address = "[" + address + "]"
	}
	target := address
	if e.host.User != "" {
		target = e.host.User + "@" + address
	}
	args = append(args, "--", target, remoteCommand(workingDirectory, command))
	return args
}

func remoteCommand(workingDirectory string, command []string) string {
	quoted := make([]string, len(command))
	for i, arg := range command {
		quoted[i] = shellQuote(arg)
	}
	remote := strings.Join(quoted, " ")
	if workingDirectory != "" {
		return "cd -- " + shellQuote(workingDirectory) + " && " + remote
	}
	return remote
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}

type capturedOutput struct {
	value     string
	truncated bool
}

type boundedBuffer struct {
	bytes.Buffer
	max       int
	truncated bool
}

// ReadFrom prevents bytes.Buffer's promoted ReadFrom method from bypassing
// Write and therefore bypassing the output bound when io.Copy detects it.
func (b *boundedBuffer) ReadFrom(reader io.Reader) (int64, error) {
	var total int64
	chunk := make([]byte, 32*1024)
	for {
		read, err := reader.Read(chunk)
		if read > 0 {
			_, _ = b.Write(chunk[:read])
			total += int64(read)
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				return total, nil
			}
			return total, err
		}
	}
}

func (b *boundedBuffer) Write(p []byte) (int, error) {
	remaining := b.max - b.Len()
	if remaining > 0 {
		if len(p) > remaining {
			_, _ = b.Buffer.Write(p[:remaining])
			b.truncated = true
		} else {
			_, _ = b.Buffer.Write(p)
		}
	} else if len(p) > 0 {
		b.truncated = true
	}
	return len(p), nil
}

func captureOutput(reader io.Reader, max int, done chan<- capturedOutput) {
	b := &boundedBuffer{max: max}
	_, _ = io.Copy(b, reader)
	done <- capturedOutput{value: b.String(), truncated: b.truncated}
}

type exitCoder interface {
	ExitCode() int
}

func exitCode(err error) (int, bool) {
	if err == nil {
		return 0, true
	}
	var coder exitCoder
	if errors.As(err, &coder) {
		return coder.ExitCode(), true
	}
	return -1, false
}

type Transport struct {
	stdin   io.WriteCloser
	stdout  io.ReadCloser
	stderr  io.ReadCloser
	process Process
	cancel  context.CancelFunc
	done    chan struct{}

	closeOnce sync.Once
	mu        sync.Mutex
	waitErr   error
	closeErr  error
}

// Open starts an SSH command with live stdin/stdout/stderr pipes. Closing the
// transport closes stdin and terminates the underlying SSH process.
func (e *Executor) Open(ctx context.Context, request ExecRequest) (*Transport, error) {
	if err := validateRequest(request.Command, request.WorkingDirectory); err != nil {
		return nil, err
	}
	if request.Timeout < 0 {
		return nil, errors.New("command timeout cannot be negative")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	streamCtx, cancel := context.WithCancel(ctx)
	if request.Timeout > 0 {
		var timeoutCancel context.CancelFunc
		streamCtx, timeoutCancel = context.WithTimeout(streamCtx, request.Timeout)
		previousCancel := cancel
		cancel = func() {
			timeoutCancel()
			previousCancel()
		}
	}
	process := e.runner.CommandContext(streamCtx, "ssh", e.sshArgs(request.WorkingDirectory, request.Command)...)
	stdin, err := process.StdinPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("opening SSH stdin: %w", err)
	}
	stdout, err := process.StdoutPipe()
	if err != nil {
		_ = stdin.Close()
		cancel()
		return nil, fmt.Errorf("opening SSH stdout: %w", err)
	}
	stderr, err := process.StderrPipe()
	if err != nil {
		_ = stdin.Close()
		_ = stdout.Close()
		cancel()
		return nil, fmt.Errorf("opening SSH stderr: %w", err)
	}
	if err := process.Start(); err != nil {
		_ = stdin.Close()
		_ = stdout.Close()
		_ = stderr.Close()
		cancel()
		if streamCtx.Err() != nil {
			return nil, streamCtx.Err()
		}
		return nil, fmt.Errorf("starting SSH: %w", err)
	}

	stream := &Transport{
		stdin: stdin, stdout: stdout, stderr: stderr,
		process: process, cancel: cancel, done: make(chan struct{}),
	}
	go func() {
		err := process.Wait()
		if streamCtx.Err() != nil {
			err = streamCtx.Err()
		}
		stream.mu.Lock()
		stream.waitErr = err
		stream.mu.Unlock()
		close(stream.done)
		cancel()
	}()
	return stream, nil
}

func (s *Transport) Read(p []byte) (int, error)  { return s.stdout.Read(p) }
func (s *Transport) Write(p []byte) (int, error) { return s.stdin.Write(p) }
func (s *Transport) Stdout() io.Reader           { return s.stdout }
func (s *Transport) Stderr() io.Reader           { return s.stderr }

func (s *Transport) CloseWrite() error {
	return s.stdin.Close()
}

func (s *Transport) Wait() error {
	<-s.done
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.waitErr
}

func (s *Transport) Close() error {
	s.closeOnce.Do(func() {
		_ = s.stdin.Close()
		if err := s.process.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) {
			s.closeErr = err
		}
		s.cancel()
	})
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.closeErr
}

func validateRequest(command []string, workingDirectory string) error {
	if len(command) == 0 {
		return errors.New("command cannot be empty")
	}
	if strings.IndexByte(workingDirectory, 0) >= 0 {
		return errors.New("working directory cannot contain NUL")
	}
	for i, arg := range command {
		if strings.IndexByte(arg, 0) >= 0 {
			return fmt.Errorf("command argument %d contains NUL", i)
		}
	}
	return nil
}

func normalizeOutputLimit(limit, max int) (int, error) {
	if limit < 0 {
		return 0, errors.New("maximum output cannot be negative")
	}
	if limit == 0 {
		limit = DefaultMaxOutput
	}
	if limit > max {
		limit = max
	}
	return limit, nil
}

func validIdentifier(value string) bool {
	if value == "" {
		return false
	}
	for _, r := range value {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '.' || r == '_' || r == '-' {
			continue
		}
		return false
	}
	return true
}

func validateAddress(value string) error {
	if value == "" {
		return errors.New("cannot be empty")
	}
	if strings.HasPrefix(value, "-") {
		return errors.New("cannot start with '-'")
	}
	for _, r := range value {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("._:-%", r) {
			continue
		}
		return errors.New("contains unsupported characters")
	}
	return nil
}

func validateUser(value string) error {
	if value == "" {
		return nil
	}
	if !validIdentifier(value) {
		return errors.New("contains unsupported characters")
	}
	return nil
}

func validateDisplayName(value string) error {
	if strings.ContainsAny(value, "\x00\r\n") {
		return errors.New("contains control characters")
	}
	return nil
}

func validatePath(value, label string) error {
	if strings.IndexByte(value, 0) >= 0 {
		return fmt.Errorf("%s cannot contain NUL", label)
	}
	if strings.HasPrefix(value, "-") {
		return fmt.Errorf("%s cannot start with '-'", label)
	}
	return nil
}
