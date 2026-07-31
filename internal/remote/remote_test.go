package remote

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestHostConfigValidationAndJSONContainNoSecrets(t *testing.T) {
	host := HostConfig{
		ID:                    "workstation-1",
		Name:                  "Workstation 1",
		Address:               "2001:db8::10",
		Port:                  2222,
		User:                  "codex",
		IdentityFile:          "/home/codex/.ssh/id_ed25519",
		KnownHostsFile:        "/home/codex/.ssh/known_hosts",
		ConnectTimeoutSeconds: 7,
	}
	if err := host.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	if got := host.Identity().Port; got != 2222 {
		t.Fatalf("Identity().Port = %d, want 2222", got)
	}

	encoded, err := json.Marshal(struct {
		Config  HostConfig  `json:"config"`
		Runtime HostRuntime `json:"runtime"`
	}{Config: host, Runtime: HostRuntime{Identity: host.Identity(), Status: HostStatusUnknown}})
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	jsonText := string(encoded)
	for _, forbidden := range []string{"password", "private_key", "passphrase", "secret"} {
		if strings.Contains(strings.ToLower(jsonText), forbidden) {
			t.Fatalf("JSON contains forbidden credential field %q: %s", forbidden, jsonText)
		}
	}
	if !strings.Contains(jsonText, "known_hosts_file") {
		t.Fatalf("JSON omitted non-secret SSH config: %s", jsonText)
	}
}

func TestHostConfigRejectsSSHOptionInjection(t *testing.T) {
	tests := []HostConfig{
		{ID: "host", Address: "-oProxyCommand=bad"},
		{ID: "host", Address: "host;uname"},
		{ID: "host", Address: "host", User: "root;uname"},
		{ID: "host", Address: "host", IdentityFile: "-evil"},
		{ID: "host", Address: "host", Port: 65536},
	}
	for _, host := range tests {
		if err := host.Validate(); err == nil {
			t.Errorf("HostConfig(%+v).Validate() = nil, want error", host)
		}
	}
}

func TestExecuteQuotesArgumentsAndReturnsStreamsAndExitStatus(t *testing.T) {
	runner := newFakeRunner(func(_ context.Context, _ []string) func(*fakeProcess) {
		return func(process *fakeProcess) {
			go func() {
				_, _ = io.WriteString(process.stdoutWriter, "stdout")
				_, _ = io.WriteString(process.stderrWriter, "stderr")
				process.finish(fakeExitError{code: 7})
			}()
		}
	})
	executor, err := NewExecutor(validHost(), WithCommandRunner(runner))
	if err != nil {
		t.Fatalf("NewExecutor() error = %v", err)
	}

	result, err := executor.Execute(context.Background(), ExecRequest{
		WorkingDirectory: "/srv/project with spaces",
		Command:          []string{"printf", "$(touch /tmp/nope); 'quoted'"},
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if result.Stdout != "stdout" || result.Stderr != "stderr" || result.ExitCode != 7 {
		t.Fatalf("unexpected result: %+v", result)
	}

	call := runner.lastCall()
	if call.name != "ssh" {
		t.Fatalf("runner command = %q, want ssh", call.name)
	}
	if !containsArg(call.args, "--") || !containsArg(call.args, "codex@192.0.2.10") {
		t.Fatalf("SSH args missing safe target delimiter/target: %#v", call.args)
	}
	remote := call.args[len(call.args)-1]
	if !strings.HasPrefix(remote, "cd -- '/srv/project with spaces' && 'printf' ") {
		t.Errorf("remote command does not contain safely quoted cwd/command: %q", remote)
	}
	wantRemote := remoteCommand("/srv/project with spaces", []string{"printf", "$(touch /tmp/nope); 'quoted'"})
	if remote != wantRemote {
		t.Errorf("remote command has unexpected quoting: %q", remote)
	}
}

func TestProbeIsBoundedAndTruncatesOutput(t *testing.T) {
	runner := newFakeRunner(func(_ context.Context, _ []string) func(*fakeProcess) {
		return func(process *fakeProcess) {
			go func() {
				_, _ = io.WriteString(process.stdoutWriter, "123456789")
				process.finish(nil)
			}()
		}
	})
	executor, err := NewExecutor(validHost(), WithCommandRunner(runner), WithProbeLimits(time.Second, 4))
	if err != nil {
		t.Fatalf("NewExecutor() error = %v", err)
	}

	result, err := executor.Probe(context.Background(), ProbeRequest{Command: []string{"codex", "--version"}, MaxOutputBytes: 100, Timeout: time.Minute})
	if err != nil {
		t.Fatalf("Probe() error = %v", err)
	}
	if result.Stdout != "1234" || !result.Truncated {
		t.Fatalf("unexpected bounded probe result: %+v", result)
	}
}

func TestProbeReturnsContextDeadline(t *testing.T) {
	runner := newFakeRunner(func(_ context.Context, _ []string) func(*fakeProcess) {
		return func(_ *fakeProcess) {}
	})
	executor, err := NewExecutor(validHost(), WithCommandRunner(runner), WithProbeLimits(20*time.Millisecond, 128))
	if err != nil {
		t.Fatalf("NewExecutor() error = %v", err)
	}

	started := time.Now()
	_, err = executor.Probe(context.Background(), ProbeRequest{Command: []string{"sleep", "forever"}})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Probe() error = %v, want context deadline", err)
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("Probe() took %s, expected bounded cancellation", elapsed)
	}
}

func TestOpenProvidesInteractiveStdinStdoutAndStderr(t *testing.T) {
	runner := newFakeRunner(func(_ context.Context, _ []string) func(*fakeProcess) {
		return func(process *fakeProcess) {
			go func() {
				_, _ = io.WriteString(process.stdoutWriter, "ready\n")
				input, err := io.ReadAll(process.stdinReader)
				if err == nil {
					_, _ = fmt.Fprintf(process.stdoutWriter, "received:%s", input)
				}
				_, _ = io.WriteString(process.stderrWriter, "diagnostic\n")
				process.finish(nil)
			}()
		}
	})
	executor, err := NewExecutor(validHost(), WithCommandRunner(runner))
	if err != nil {
		t.Fatalf("NewExecutor() error = %v", err)
	}

	stream, err := executor.Open(context.Background(), ExecRequest{Command: []string{"codex", "rc"}})
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer stream.Close()

	buf := make([]byte, len("ready\n"))
	if _, err := io.ReadFull(stream, buf); err != nil {
		t.Fatalf("reading ready marker: %v", err)
	}
	if string(buf) != "ready\n" {
		t.Fatalf("ready marker = %q", buf)
	}
	if _, err := io.WriteString(stream, "hello\n"); err != nil {
		t.Fatalf("writing stdin: %v", err)
	}
	if err := stream.CloseWrite(); err != nil {
		t.Fatalf("CloseWrite() error = %v", err)
	}

	stderrDone := make(chan []byte, 1)
	go func() {
		stderr, _ := io.ReadAll(stream.Stderr())
		stderrDone <- stderr
	}()
	stdout, err := io.ReadAll(stream.Stdout())
	if err != nil {
		t.Fatalf("reading stdout: %v", err)
	}
	if string(stdout) != "received:hello\n" {
		t.Fatalf("stdout after ready = %q", stdout)
	}
	stderr := <-stderrDone
	if string(stderr) != "diagnostic\n" {
		t.Fatalf("stderr = %q", stderr)
	}
	if err := stream.Wait(); err != nil {
		t.Fatalf("Wait() error = %v", err)
	}
}

func validHost() HostConfig {
	return HostConfig{ID: "test-host", Address: "192.0.2.10", Port: 22, User: "codex"}
}

func containsArg(args []string, want string) bool {
	for _, arg := range args {
		if arg == want {
			return true
		}
	}
	return false
}

type fakeCall struct {
	name string
	args []string
}

type fakeRunner struct {
	mu      sync.Mutex
	calls   []fakeCall
	newProc func(context.Context, []string) func(*fakeProcess)
}

func newFakeRunner(newProc func(context.Context, []string) func(*fakeProcess)) *fakeRunner {
	return &fakeRunner{newProc: newProc}
}

func (r *fakeRunner) CommandContext(ctx context.Context, name string, args ...string) Process {
	argsCopy := append([]string(nil), args...)
	r.mu.Lock()
	r.calls = append(r.calls, fakeCall{name: name, args: argsCopy})
	r.mu.Unlock()
	process := newFakeProcess(ctx, r.newProc(ctx, argsCopy))
	return process
}

func (r *fakeRunner) lastCall() fakeCall {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.calls[len(r.calls)-1]
}

type fakeProcess struct {
	ctx context.Context

	stdinReader  *io.PipeReader
	stdinWriter  *io.PipeWriter
	stdoutReader *io.PipeReader
	stdoutWriter *io.PipeWriter
	stderrReader *io.PipeReader
	stderrWriter *io.PipeWriter

	startFn    func(*fakeProcess)
	done       chan struct{}
	finishOnce sync.Once
	mu         sync.Mutex
	waitErr    error
}

func newFakeProcess(ctx context.Context, startFn func(*fakeProcess)) *fakeProcess {
	stdinReader, stdinWriter := io.Pipe()
	stdoutReader, stdoutWriter := io.Pipe()
	stderrReader, stderrWriter := io.Pipe()
	return &fakeProcess{
		ctx: ctx, stdinReader: stdinReader, stdinWriter: stdinWriter,
		stdoutReader: stdoutReader, stdoutWriter: stdoutWriter,
		stderrReader: stderrReader, stderrWriter: stderrWriter,
		startFn: startFn, done: make(chan struct{}),
	}
}

func (p *fakeProcess) StdinPipe() (io.WriteCloser, error) { return p.stdinWriter, nil }
func (p *fakeProcess) StdoutPipe() (io.ReadCloser, error) { return p.stdoutReader, nil }
func (p *fakeProcess) StderrPipe() (io.ReadCloser, error) { return p.stderrReader, nil }
func (p *fakeProcess) Start() error {
	go func() {
		select {
		case <-p.ctx.Done():
			p.finish(p.ctx.Err())
		case <-p.done:
		}
	}()
	p.startFn(p)
	return nil
}
func (p *fakeProcess) Wait() error {
	<-p.done
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.waitErr
}
func (p *fakeProcess) Kill() error {
	select {
	case <-p.done:
		return os.ErrProcessDone
	default:
		p.finish(errors.New("killed"))
		return nil
	}
}
func (p *fakeProcess) finish(err error) {
	p.finishOnce.Do(func() {
		p.mu.Lock()
		p.waitErr = err
		p.mu.Unlock()
		_ = p.stdinReader.Close()
		_ = p.stdinWriter.Close()
		_ = p.stdoutWriter.Close()
		_ = p.stderrWriter.Close()
		close(p.done)
	})
}

type fakeExitError struct {
	code int
}

func (e fakeExitError) Error() string { return fmt.Sprintf("fake exit %d", e.code) }
func (e fakeExitError) ExitCode() int { return e.code }
