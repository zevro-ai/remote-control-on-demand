package ralph

import (
	"context"
	"log"
	"os"
	"strings"
	"testing"
	"time"
)

func newTestRunner(calls ...mockCall) *mockRunner {
	return &mockRunner{calls: calls}
}

func newTestLogger() *log.Logger {
	return log.New(os.Stderr, "[ralph-test] ", 0)
}

func newTestClaudeClient(runner CmdRunner, dir string) *ClaudeClient {
	c := NewClaudeClient(runner, dir)
	c.bin = "codex"
	return c
}

// pushUpToDateMocks returns mock calls for pushUnpushedCommits when local and remote are in sync.
func pushUpToDateMocks() []mockCall {
	return []mockCall{
		{name: "git", contains: []string{"rev-parse", "HEAD"}, notContains: []string{"--abbrev-ref"}, stdout: "aaa111\n"},
		{name: "git", contains: []string{"rev-parse", "@{u}"}, stdout: "aaa111\n"},
	}
}

// ciPassingMocks returns mock calls for CI check that reports all passing.
func ciPassingMocks() []mockCall {
	return []mockCall{
		// currentBranch
		{name: "git", contains: []string{"rev-parse", "--abbrev-ref", "HEAD"}, stdout: "feat/test-branch"},
		// gh run list — build succeeded
		{name: "gh", contains: []string{"run", "list"}, stdout: `[{"databaseId":1,"status":"completed","conclusion":"success","name":"build"}]`},
	}
}

func TestRunOnce_PRMerged_ExitsLoop(t *testing.T) {
	calls := pushUpToDateMocks()
	calls = append(calls,
		mockCall{name: "gh", contains: []string{"pr", "view", "--json", "state"}, stdout: `{"state":"MERGED","headRefOid":"abc"}`},
	)
	m := newTestRunner(calls...)

	r := &Runner{
		cfg:    Config{Dir: "/tmp", MaxRetries: 3, PollInterval: time.Second},
		gh:     NewGitHubClient(m, "/tmp"),
		claude: newTestClaudeClient(m, "/tmp"),
		runner: m,
		log:    newTestLogger(),
	}

	shouldContinue, err := r.runOnce(context.Background(), &PRInfo{Owner: "o", Repo: "r", Number: 1})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if shouldContinue {
		t.Fatal("expected loop to exit on merged PR")
	}
}

func TestRunOnce_PRClosed_ExitsLoop(t *testing.T) {
	calls := pushUpToDateMocks()
	calls = append(calls,
		mockCall{name: "gh", contains: []string{"pr", "view", "--json", "state"}, stdout: `{"state":"CLOSED","headRefOid":"abc"}`},
	)
	m := newTestRunner(calls...)

	r := &Runner{
		cfg:    Config{Dir: "/tmp", MaxRetries: 3, PollInterval: time.Second},
		gh:     NewGitHubClient(m, "/tmp"),
		claude: newTestClaudeClient(m, "/tmp"),
		runner: m,
		log:    newTestLogger(),
	}

	shouldContinue, err := r.runOnce(context.Background(), &PRInfo{Owner: "o", Repo: "r", Number: 1})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if shouldContinue {
		t.Fatal("expected loop to exit on closed PR")
	}
}

func TestRunOnce_NoNewComments_Continues(t *testing.T) {
	calls := pushUpToDateMocks()
	calls = append(calls,
		// GetPRStatus
		mockCall{name: "gh", contains: []string{"pr", "view", "--json", "state"}, stdout: `{"state":"OPEN","headRefOid":"abc123"}`},
	)
	calls = append(calls, ciPassingMocks()...)
	calls = append(calls,
		// GetCommitDate
		mockCall{name: "gh", contains: []string{"api", "repos/o/r/commits/abc123"}, stdout: "2026-03-20T10:00:00Z"},
		// FetchGreptileComment — not updated since commit
		mockCall{name: "gh", contains: []string{"api", "issues"}, stdout: `{"body":"old","updated_at":"2026-03-20T09:00:00Z"}`},
	)

	m := newTestRunner(calls...)

	r := &Runner{
		cfg:    Config{Dir: "/tmp", MaxRetries: 3, PollInterval: time.Second},
		gh:     NewGitHubClient(m, "/tmp"),
		claude: newTestClaudeClient(m, "/tmp"),
		runner: m,
		log:    newTestLogger(),
	}

	shouldContinue, err := r.runOnce(context.Background(), &PRInfo{Owner: "o", Repo: "r", Number: 1})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !shouldContinue {
		t.Fatal("expected loop to continue when no new comments")
	}
}

func TestRunOnce_FullCycle(t *testing.T) {
	calls := pushUpToDateMocks()
	calls = append(calls,
		// GetPRStatus
		mockCall{name: "gh", contains: []string{"pr", "view", "--json", "state"}, stdout: `{"state":"OPEN","headRefOid":"abc123"}`},
	)
	calls = append(calls, ciPassingMocks()...)
	calls = append(calls,
		// GetCommitDate
		mockCall{name: "gh", contains: []string{"api", "repos/o/r/commits/abc123"}, stdout: "2026-03-20T10:00:00Z"},
		// FetchGreptileComment — updated after commit
		mockCall{name: "gh", contains: []string{"api", "issues"}, stdout: `{"body":"**Critical:** Fix the staged-changes check","updated_at":"2026-03-20T12:00:00Z"}`},
		// git pull --rebase
		mockCall{name: "git", contains: []string{"pull", "--rebase"}, stdout: "Already up to date."},
	)

	m := &mockRunner{
		calls: calls,
		fallback: func(name string, args ...string) (string, string, int, error) {
			joined := strings.Join(args, " ")

			// Claude apply fixes
			if strings.HasSuffix(name, "codex") || (name == "claude" && strings.Contains(joined, "-p")) {
				return "Fixed", "", 0, nil
			}
			// go test
			if name == "go" && strings.Contains(joined, "test") {
				return "ok", "", 0, nil
			}
			// go build
			if name == "go" && strings.Contains(joined, "build") {
				return "", "", 0, nil
			}
			// git diff --name-only (list modified tracked files)
			if name == "git" && strings.Contains(joined, "diff --name-only") {
				return "file.go\n", "", 0, nil
			}
			// git ls-files --others (list untracked new files)
			if name == "git" && strings.Contains(joined, "ls-files") {
				return "", "", 0, nil
			}
			// git diff --stat --cached (log staged changes)
			if name == "git" && strings.Contains(joined, "diff --stat --cached") {
				return " file.go | 1 +\n", "", 0, nil
			}
			// git add
			if name == "git" && strings.Contains(joined, "add") {
				return "", "", 0, nil
			}
			// git diff --cached --quiet (exit 1 = changes exist)
			if name == "git" && strings.Contains(joined, "diff --cached --quiet") {
				return "", "", 1, nil
			}
			// git commit
			if name == "git" && strings.Contains(joined, "commit") {
				return "", "", 0, nil
			}
			// git rev-parse --short
			if name == "git" && strings.Contains(joined, "rev-parse --short") {
				return "abc1234", "", 0, nil
			}
			// git push
			if name == "git" && strings.Contains(joined, "push") {
				return "", "", 0, nil
			}
			// gh pr comment
			if name == "gh" && strings.Contains(joined, "pr comment") {
				return "", "", 0, nil
			}
			return "", "unexpected: " + name + " " + joined, 1, nil
		},
	}

	r := &Runner{
		cfg:    Config{Dir: "/tmp", MaxRetries: 3, PollInterval: time.Second},
		gh:     NewGitHubClient(m, "/tmp"),
		claude: newTestClaudeClient(m, "/tmp"),
		runner: m,
		log:    newTestLogger(),
	}

	shouldContinue, err := r.runOnce(context.Background(), &PRInfo{Owner: "o", Repo: "r", Number: 1})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !shouldContinue {
		t.Fatal("expected loop to continue after successful fix cycle")
	}
}

func TestRunOnce_TestsFail_RetriesAndExits(t *testing.T) {
	testAttempt := 0

	calls := pushUpToDateMocks()
	calls = append(calls,
		mockCall{name: "gh", contains: []string{"pr", "view", "--json", "state"}, stdout: `{"state":"OPEN","headRefOid":"abc"}`},
	)
	calls = append(calls, ciPassingMocks()...)
	calls = append(calls,
		mockCall{name: "gh", contains: []string{"api", "repos/o/r/commits/abc"}, stdout: "2026-03-20T10:00:00Z"},
		mockCall{name: "gh", contains: []string{"api", "issues"}, stdout: `{"body":"Fix something","updated_at":"2026-03-20T12:00:00Z"}`},
		mockCall{name: "git", contains: []string{"pull", "--rebase"}, stdout: "ok"},
	)

	m := &mockRunner{
		calls: calls,
		fallback: func(name string, args ...string) (string, string, int, error) {
			joined := strings.Join(args, " ")
			if strings.HasSuffix(name, "codex") {
				return "Applied", "", 0, nil
			}
			if name == "go" && strings.Contains(joined, "test") {
				testAttempt++
				return "", "FAIL: TestSomething", 1, nil
			}
			// cleanup: git restore / git clean
			if name == "git" && (strings.Contains(joined, "restore") || strings.Contains(joined, "clean")) {
				return "", "", 0, nil
			}
			return "", "unexpected", 1, nil
		},
	}

	r := &Runner{
		cfg:    Config{Dir: "/tmp", MaxRetries: 2, PollInterval: time.Second},
		gh:     NewGitHubClient(m, "/tmp"),
		claude: newTestClaudeClient(m, "/tmp"),
		runner: m,
		log:    newTestLogger(),
	}

	shouldContinue, err := r.runOnce(context.Background(), &PRInfo{Owner: "o", Repo: "r", Number: 1})
	if !shouldContinue {
		t.Fatal("expected loop to continue after max retries (skip cycle, not exit)")
	}
	if err == nil {
		t.Fatal("expected error after max retries")
	}
	if !strings.Contains(err.Error(), "still failing") {
		t.Fatalf("unexpected error: %v", err)
	}
	// Initial attempt + 2 retries = 3 total test runs
	if testAttempt != 3 {
		t.Fatalf("expected 3 test attempts, got %d", testAttempt)
	}
}

func TestRunOnce_CIFailing_FixesAndContinues(t *testing.T) {
	calls := pushUpToDateMocks()
	calls = append(calls,
		// GetPRStatus
		mockCall{name: "gh", contains: []string{"pr", "view", "--json", "state"}, stdout: `{"state":"OPEN","headRefOid":"abc123"}`},
		// currentBranch
		mockCall{name: "git", contains: []string{"rev-parse", "--abbrev-ref", "HEAD"}, stdout: "feat/test-branch"},
		// gh run list — build failed
		mockCall{name: "gh", contains: []string{"run", "list"}, stdout: `[{"databaseId":999,"status":"completed","conclusion":"failure","name":"build"}]`},
		// gh run view --log (CI failure logs)
		mockCall{name: "gh", contains: []string{"run", "view", "999"}, stdout: "FAIL: TestSomething\nexit code 1"},
		// git pull --rebase
		mockCall{name: "git", contains: []string{"pull", "--rebase"}, stdout: "ok"},
	)
	m := &mockRunner{
		calls: calls,
		fallback: func(name string, args ...string) (string, string, int, error) {
			joined := strings.Join(args, " ")
			if strings.HasSuffix(name, "codex") {
				return "Fixed CI", "", 0, nil
			}
			if name == "go" && strings.Contains(joined, "test") {
				return "ok", "", 0, nil
			}
			if name == "go" && strings.Contains(joined, "build") {
				return "", "", 0, nil
			}
			// git diff --name-only (list modified tracked files)
			if name == "git" && strings.Contains(joined, "diff --name-only") {
				return "file.go\n", "", 0, nil
			}
			// git ls-files --others (list untracked new files)
			if name == "git" && strings.Contains(joined, "ls-files") {
				return "", "", 0, nil
			}
			// git diff --stat --cached (log staged changes)
			if name == "git" && strings.Contains(joined, "diff --stat --cached") {
				return " file.go | 1 +\n", "", 0, nil
			}
			if name == "git" && strings.Contains(joined, "add") {
				return "", "", 0, nil
			}
			if name == "git" && strings.Contains(joined, "diff --cached --quiet") {
				return "", "", 1, nil
			}
			if name == "git" && strings.Contains(joined, "commit") {
				return "", "", 0, nil
			}
			if name == "git" && strings.Contains(joined, "rev-parse --short") {
				return "def5678", "", 0, nil
			}
			if name == "git" && strings.Contains(joined, "push") {
				return "", "", 0, nil
			}
			// gh pr comment
			if name == "gh" && strings.Contains(joined, "pr comment") {
				return "", "", 0, nil
			}
			return "", "unexpected: " + name + " " + joined, 1, nil
		},
	}

	r := &Runner{
		cfg:    Config{Dir: "/tmp", MaxRetries: 3, PollInterval: time.Second},
		gh:     NewGitHubClient(m, "/tmp"),
		claude: newTestClaudeClient(m, "/tmp"),
		runner: m,
		log:    newTestLogger(),
	}

	shouldContinue, err := r.runOnce(context.Background(), &PRInfo{Owner: "o", Repo: "r", Number: 1})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !shouldContinue {
		t.Fatal("expected loop to continue after CI fix")
	}
}

func TestCheckCleanWorkingTree_Dirty(t *testing.T) {
	m := newTestRunner(
		mockCall{name: "git", contains: []string{"status", "--porcelain"}, stdout: " M dirty.go\n"},
	)

	r := &Runner{
		cfg:    Config{Dir: "/tmp"},
		runner: m,
		log:    newTestLogger(),
	}

	err := r.checkCleanWorkingTree(context.Background())
	if err == nil {
		t.Fatal("expected error for dirty working tree")
	}
	if !strings.Contains(err.Error(), "dirty") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCheckCleanWorkingTree_Clean(t *testing.T) {
	m := newTestRunner(
		mockCall{name: "git", contains: []string{"status", "--porcelain"}, stdout: ""},
	)

	r := &Runner{
		cfg:    Config{Dir: "/tmp"},
		runner: m,
		log:    newTestLogger(),
	}

	err := r.checkCleanWorkingTree(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestParseConfidenceScore(t *testing.T) {
	tests := []struct {
		body  string
		score int
	}{
		{"<h3>Confidence Score: 5/5</h3>", 5},
		{"<h3>Confidence Score: 2/5</h3>", 2},
		{"<h3>Confidence Score: 0/5</h3>", 0},
		{"<h3> Confidence Score: 3 / 5 </h3>", 3},
		{"no score here", 0},
		// plain text without heading should NOT match (avoids mermaid diagram false positives)
		{"confidence 5/5", 0},
		// markdown heading fallback
		{"### Confidence Score: 4/5", 4},
		{"## Confidence Score: 3/5", 3},
		{"#### Confidence Score: 5/5", 5},
		// inline text (no heading prefix) should NOT match
		{"Confidence Score: 4/5", 0},
		// HTML takes priority over markdown
		{"### Confidence Score: 2/5\n<h3>Confidence Score: 4/5</h3>", 4},
		// real Greptile body with diagram containing "confidence 5/5" — must pick the <h3> one
		{"K -- confidence 5/5 --> L\n<h3>Confidence Score: 3/5</h3>\nsome text confidence 5/5", 3},
	}
	for _, tt := range tests {
		got := parseConfidenceScore(tt.body)
		if got != tt.score {
			t.Errorf("parseConfidenceScore(%q) = %d, want %d", tt.body[:min(len(tt.body), 40)], got, tt.score)
		}
	}
}

func TestRunOnce_Approved_ExitsLoop(t *testing.T) {
	calls := pushUpToDateMocks()
	calls = append(calls,
		// GetPRStatus
		mockCall{name: "gh", contains: []string{"pr", "view", "--json", "state"}, stdout: `{"state":"OPEN","headRefOid":"abc123"}`},
	)
	calls = append(calls, ciPassingMocks()...)
	calls = append(calls,
		// GetCommitDate
		mockCall{name: "gh", contains: []string{"api", "repos/o/r/commits/abc123"}, stdout: "2026-03-20T10:00:00Z"},
		// FetchGreptileComment — confidence 5/5
		mockCall{name: "gh", contains: []string{"api", "issues"}, stdout: `{"body":"<h3>Confidence Score: 5/5</h3>\nLooks good!","updated_at":"2026-03-20T12:00:00Z"}`},
	)

	m := newTestRunner(calls...)

	r := &Runner{
		cfg:    Config{Dir: "/tmp", MaxRetries: 3, PollInterval: time.Second},
		gh:     NewGitHubClient(m, "/tmp"),
		claude: newTestClaudeClient(m, "/tmp"),
		runner: m,
		log:    newTestLogger(),
	}

	shouldContinue, err := r.runOnce(context.Background(), &PRInfo{Owner: "o", Repo: "r", Number: 28})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if shouldContinue {
		t.Fatal("expected loop to exit on confidence 5/5")
	}
}

func TestTruncate(t *testing.T) {
	if truncate("short", 10) != "short" {
		t.Fatal("short string should not be truncated")
	}
	result := truncate("this is a very long string that should be truncated", 20)
	if len(result) > 20 {
		t.Fatalf("expected max length 20, got %d", len(result))
	}
	if !strings.HasSuffix(result, "...") {
		t.Fatal("truncated string should end with ...")
	}
}
