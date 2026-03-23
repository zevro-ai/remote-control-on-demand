package ralph

import (
	"context"
	"fmt"
	"io"
	"log"
	"strings"
	"testing"
	"time"
)

// mockRunner records calls and returns canned responses.
type mockRunner struct {
	calls    []mockCall
	callLog  []string
	fallback func(name string, args ...string) (string, string, int, error)
}

type mockCall struct {
	name        string
	contains    []string // args must contain all of these substrings
	notContains []string // args must NOT contain any of these substrings
	stdout      string
	stderr      string
	exitCode    int
	err         error
}

func (m *mockRunner) Run(ctx context.Context, dir string, env []string, name string, args ...string) (string, string, int, error) {
	return m.RunWithStdin(ctx, dir, env, "", name, args...)
}

func (m *mockRunner) RunWithStdin(_ context.Context, _ string, _ []string, _ string, name string, args ...string) (string, string, int, error) {
	joined := strings.Join(args, " ")
	m.callLog = append(m.callLog, name+" "+joined)

	for i, c := range m.calls {
		if c.name != name {
			continue
		}
		match := true
		for _, s := range c.contains {
			if !strings.Contains(joined, s) {
				match = false
				break
			}
		}
		if match {
			for _, s := range c.notContains {
				if strings.Contains(joined, s) {
					match = false
					break
				}
			}
		}
		if match {
			// Remove used call to allow sequential matching
			m.calls = append(m.calls[:i], m.calls[i+1:]...)
			return c.stdout, c.stderr, c.exitCode, c.err
		}
	}

	if m.fallback != nil {
		return m.fallback(name, args...)
	}

	return "", fmt.Sprintf("no mock for: %s %s", name, joined), 1, nil
}

func TestParsePRURL(t *testing.T) {
	tests := []struct {
		url    string
		owner  string
		repo   string
		number int
		err    bool
	}{
		{"https://github.com/zevro-ai/remote-control-on-demand/pull/27", "zevro-ai", "remote-control-on-demand", 27, false},
		{"https://github.com/owner/repo/pull/1", "owner", "repo", 1, false},
		{"https://github.com/owner/repo/pull/1/", "owner", "repo", 1, false},
		{"invalid", "", "", 0, true},
		{"https://github.com/owner/repo", "", "", 0, true},
	}

	for _, tt := range tests {
		info, err := ParsePRRef(tt.url)
		if tt.err {
			if err == nil {
				t.Errorf("ParsePRRef(%q): expected error", tt.url)
			}
			continue
		}
		if err != nil {
			t.Errorf("ParsePRRef(%q): %v", tt.url, err)
			continue
		}
		if info.Owner != tt.owner || info.Repo != tt.repo || info.Number != tt.number {
			t.Errorf("ParsePRRef(%q) = %+v, want owner=%s repo=%s number=%d", tt.url, info, tt.owner, tt.repo, tt.number)
		}
	}
}

func TestGetPRFromBranch(t *testing.T) {
	m := &mockRunner{
		calls: []mockCall{
			{
				name:     "gh",
				contains: []string{"pr", "view", "--json"},
				stdout:   `{"number":27,"url":"https://github.com/zevro-ai/rcod/pull/27"}`,
			},
		},
	}
	gh := NewGitHubClient(m, "/tmp")
	info, err := gh.GetPRFromBranch(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if info.Owner != "zevro-ai" || info.Repo != "rcod" || info.Number != 27 {
		t.Fatalf("unexpected PR info: %+v", info)
	}
}

func TestGetPRStatus_Open(t *testing.T) {
	m := &mockRunner{
		calls: []mockCall{
			{
				name:     "gh",
				contains: []string{"pr", "view", "27"},
				stdout:   `{"state":"OPEN","headRefOid":"abc123"}`,
			},
		},
	}
	gh := NewGitHubClient(m, "/tmp")
	status, err := gh.GetPRStatus(context.Background(), &PRInfo{Owner: "o", Repo: "r", Number: 27})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if status.State != "OPEN" || status.HeadSHA != "abc123" {
		t.Fatalf("unexpected status: %+v", status)
	}
}

func TestGetPRStatus_Merged(t *testing.T) {
	m := &mockRunner{
		calls: []mockCall{
			{
				name:     "gh",
				contains: []string{"pr", "view"},
				stdout:   `{"state":"MERGED","headRefOid":"def456"}`,
			},
		},
	}
	gh := NewGitHubClient(m, "/tmp")
	status, err := gh.GetPRStatus(context.Background(), &PRInfo{Owner: "o", Repo: "r", Number: 1})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if status.State != "MERGED" {
		t.Fatalf("expected MERGED, got %s", status.State)
	}
}

func TestGetCommitDate(t *testing.T) {
	m := &mockRunner{
		calls: []mockCall{
			{
				name:     "gh",
				contains: []string{"api", "repos/o/r/commits/abc"},
				stdout:   "2026-03-20T10:00:00Z\n",
			},
		},
	}
	gh := NewGitHubClient(m, "/tmp")
	date, err := gh.GetCommitDate(context.Background(), &PRInfo{Owner: "o", Repo: "r", Number: 1}, "abc")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expected := time.Date(2026, 3, 20, 10, 0, 0, 0, time.UTC)
	if !date.Equal(expected) {
		t.Fatalf("expected %v, got %v", expected, date)
	}
}

func TestFetchGreptileComment_Found(t *testing.T) {
	since := time.Date(2026, 3, 20, 10, 0, 0, 0, time.UTC)

	m := &mockRunner{
		calls: []mockCall{
			{
				name:     "gh",
				contains: []string{"api", "issues"},
				stdout:   `{"body":"**Critical:** Fix the bug","updated_at":"2026-03-20T12:00:00Z"}`,
			},
		},
	}

	gh := NewGitHubClient(m, "/tmp")
	body, err := gh.FetchGreptileComment(context.Background(), &PRInfo{Owner: "o", Repo: "r", Number: 1}, since)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if body != "**Critical:** Fix the bug" {
		t.Fatalf("unexpected body: %q", body)
	}
}

func TestFetchGreptileComment_NotUpdatedSinceCommit(t *testing.T) {
	since := time.Date(2026, 3, 20, 14, 0, 0, 0, time.UTC)

	m := &mockRunner{
		calls: []mockCall{
			{
				name:     "gh",
				contains: []string{"api", "issues"},
				stdout:   `{"body":"old review","updated_at":"2026-03-20T12:00:00Z"}`,
			},
		},
	}

	gh := NewGitHubClient(m, "/tmp")
	body, err := gh.FetchGreptileComment(context.Background(), &PRInfo{Owner: "o", Repo: "r", Number: 1}, since)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if body != "" {
		t.Fatalf("expected empty body for old comment, got: %q", body)
	}
}

func TestGetFailedCILogs_YmlExtensionNormalized(t *testing.T) {
	runsJSON := `[{"databaseId":100,"status":"completed","conclusion":"failure","name":"build"}]`
	logOutput := "FAIL some test output"

	m := &mockRunner{
		calls: []mockCall{
			{name: "git", contains: []string{"rev-parse", "--abbrev-ref"}, stdout: "feat/test"},
			{name: "gh", contains: []string{"run", "list"}, stdout: runsJSON},
			{name: "gh", contains: []string{"run", "view", "100"}, stdout: logOutput},
		},
	}

	gh := NewGitHubClient(m, "/tmp")
	gh.ciWorkflowName = "build.yml" // user passes .yml extension
	gh.log = discardLogger()

	logs, err := gh.GetFailedCILogs(context.Background(), &PRInfo{Owner: "o", Repo: "r", Number: 1})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if logs != logOutput {
		t.Fatalf("expected CI logs %q, got %q", logOutput, logs)
	}
}

func TestGetFailedCILogs_PassingCI(t *testing.T) {
	runsJSON := `[{"databaseId":100,"status":"completed","conclusion":"success","name":"build"}]`

	m := &mockRunner{
		calls: []mockCall{
			{name: "git", contains: []string{"rev-parse", "--abbrev-ref"}, stdout: "feat/test"},
			{name: "gh", contains: []string{"run", "list"}, stdout: runsJSON},
		},
	}

	gh := NewGitHubClient(m, "/tmp")
	gh.ciWorkflowName = "build"
	gh.log = discardLogger()

	logs, err := gh.GetFailedCILogs(context.Background(), &PRInfo{Owner: "o", Repo: "r", Number: 1})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if logs != "" {
		t.Fatalf("expected empty logs for passing CI, got %q", logs)
	}
}

func discardLogger() *log.Logger {
	return log.New(io.Discard, "", 0)
}

func TestFetchGreptileComment_NoComment(t *testing.T) {
	since := time.Date(2026, 3, 20, 10, 0, 0, 0, time.UTC)

	m := &mockRunner{
		calls: []mockCall{
			{
				name:     "gh",
				contains: []string{"api", "issues"},
				stdout:   "null",
			},
		},
	}

	gh := NewGitHubClient(m, "/tmp")
	body, err := gh.FetchGreptileComment(context.Background(), &PRInfo{Owner: "o", Repo: "r", Number: 1}, since)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if body != "" {
		t.Fatalf("expected empty body when no comment, got: %q", body)
	}
}
