package ralph

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Config holds ralph's runtime configuration.
type Config struct {
	PROwner        string
	PRRepo         string
	PRNumber       int
	PollInterval   time.Duration
	MaxRetries     int
	Dir            string
	TelegramToken  string
	TelegramUserID int64
	CIWorkflowName string // defaults to "build" if empty
}

// Runner orchestrates the review-fix loop.
type Runner struct {
	cfg    Config
	gh     *GitHubClient
	claude *ClaudeClient
	runner CmdRunner
	log    *log.Logger
}

// NewRunner creates a Runner with all dependencies.
func NewRunner(cfg Config, cmdRunner CmdRunner, logger *log.Logger) *Runner {
	gh := NewGitHubClient(cmdRunner, cfg.Dir)
	gh.ciWorkflowName = cfg.CIWorkflowName
	gh.log = logger
	return &Runner{
		cfg:    cfg,
		gh:     gh,
		claude: NewClaudeClient(cmdRunner, cfg.Dir),
		runner: cmdRunner,
		log:    logger,
	}
}

// Run executes the main loop until the context is cancelled or the PR is closed/merged.
func (r *Runner) Run(ctx context.Context) error {
	pr := &PRInfo{
		Owner:  r.cfg.PROwner,
		Repo:   r.cfg.PRRepo,
		Number: r.cfg.PRNumber,
	}

	// Auto-detect PR from branch if not specified
	if pr.Number == 0 {
		detected, err := r.gh.GetPRFromBranch(ctx)
		if err != nil {
			return fmt.Errorf("detecting PR from branch: %w", err)
		}
		pr = detected
		r.log.Printf("detected PR #%d (%s/%s)", pr.Number, pr.Owner, pr.Repo)
	}

	// Check working tree is clean
	if err := r.checkCleanWorkingTree(ctx); err != nil {
		return err
	}

	r.log.Printf("starting ralph loop for PR #%d (poll every %s, max %d fix attempts)",
		pr.Number, r.cfg.PollInterval, r.cfg.MaxRetries)

	for {
		shouldContinue, err := r.runOnce(ctx, pr)
		if err != nil {
			r.log.Printf("iteration error: %v", err)
		}
		if !shouldContinue {
			return err
		}

		r.log.Printf("next check in %s...", r.cfg.PollInterval)
		timer := time.NewTimer(r.cfg.PollInterval)
		select {
		case <-ctx.Done():
			timer.Stop()
			r.log.Println("shutting down")
			return ctx.Err()
		case <-timer.C:
		}
	}
}

func (r *Runner) checkCleanWorkingTree(ctx context.Context) error {
	stdout, _, _, err := r.runner.Run(ctx, r.cfg.Dir, nil, "git", "status", "--porcelain")
	if err != nil {
		return fmt.Errorf("checking git status: %w", err)
	}
	if strings.TrimSpace(stdout) != "" {
		return fmt.Errorf("working tree is dirty; commit or stash changes before running ralph")
	}
	return nil
}

// pushUnpushedCommits detects local commits that haven't been pushed (e.g. after
// a previous push failure) and retries the push before proceeding.
func (r *Runner) pushUnpushedCommits(ctx context.Context) error {
	localSHA, _, exitCode, err := r.runner.Run(ctx, r.cfg.Dir, nil, "git", "rev-parse", "HEAD")
	if err != nil || exitCode != 0 {
		return fmt.Errorf("git rev-parse HEAD failed")
	}
	remoteSHA, _, exitCode, err := r.runner.Run(ctx, r.cfg.Dir, nil, "git", "rev-parse", "@{u}")
	if err != nil || exitCode != 0 {
		// No upstream configured — nothing to push
		return nil
	}
	if strings.TrimSpace(localSHA) == strings.TrimSpace(remoteSHA) {
		return nil
	}

	r.log.Println("detected unpushed local commits; retrying push")
	_, stderr, exitCode, pushErr := r.runner.Run(ctx, r.cfg.Dir, nil, "git", "push")
	if pushErr != nil || exitCode != 0 {
		return fmt.Errorf("retrying push of stranded commits: %s", strings.TrimSpace(stderr))
	}
	r.log.Println("successfully pushed previously stranded commits")
	return nil
}

// runOnce performs a single iteration of the loop.
// Returns false if the loop should exit.
func (r *Runner) runOnce(ctx context.Context, pr *PRInfo) (bool, error) {
	// Step 0: Retry pushing any stranded local commits from a previous failed push
	if err := r.pushUnpushedCommits(ctx); err != nil {
		return true, fmt.Errorf("pushing stranded commits: %w", err)
	}

	// Step 1: Check PR status
	status, err := r.gh.GetPRStatus(ctx, pr)
	if err != nil {
		return true, fmt.Errorf("checking PR status: %w", err)
	}

	switch status.State {
	case "MERGED":
		r.log.Println("PR has been merged. Ralph loop complete.")
		return false, nil
	case "CLOSED":
		r.log.Println("PR was closed without merge. Ralph loop exiting.")
		return false, nil
	}

	// Step 2: Check CI pipeline — fix failures before looking at review
	ciLogs, err := r.gh.GetFailedCILogs(ctx, pr)
	if err != nil {
		return true, fmt.Errorf("checking CI: %w", err)
	}
	if ciLogs != "" {
		r.log.Println("CI pipeline failing — fixing before checking review")
		return r.fixCI(ctx, pr, ciLogs)
	}

	// Step 3: Get commit date as filter baseline
	commitDate, err := r.gh.GetCommitDate(ctx, pr, status.HeadSHA)
	if err != nil {
		return true, fmt.Errorf("fetching commit date: %w", err)
	}

	// Step 4: Fetch Greptile review comment (updated after last commit)
	reviewBody, err := r.gh.FetchGreptileComment(ctx, pr, commitDate)
	if err != nil {
		return true, fmt.Errorf("fetching greptile comment: %w", err)
	}

	if reviewBody == "" {
		r.log.Println("no new review comments")
		return true, nil
	}

	r.log.Println("found updated Greptile review comment")

	// Check if Greptile approved (confidence 5/5)
	if parseConfidenceScore(reviewBody) == 5 {
		r.log.Println("Greptile confidence 5/5 — review approved!")
		prURL := fmt.Sprintf("https://github.com/%s/%s/pull/%d", pr.Owner, pr.Repo, pr.Number)
		r.notifyTelegram(fmt.Sprintf("Greptile approved PR #%d: %s", pr.Number, prURL))
		return false, nil
	}

	// Pull latest changes before applying fixes
	if err := r.gitPull(ctx); err != nil {
		return true, fmt.Errorf("pulling latest: %w", err)
	}

	// Step 5: Apply fixes via Claude
	output, commitPrefix, err := r.claude.ApplyReviewFixes(ctx, reviewBody)
	if err != nil {
		return true, fmt.Errorf("applying fixes: %w", err)
	}
	r.log.Printf("claude output: %s", truncate(output, 200))

	// Step 6: Run tests and builds with retry
	if err := r.verifyWithRetry(ctx, 0); err != nil {
		r.cleanupWorkingTree()
		prURL := fmt.Sprintf("https://github.com/%s/%s/pull/%d", pr.Owner, pr.Repo, pr.Number)
		r.notifyTelegram(fmt.Sprintf("❌ ralph gave up on PR #%d after %d fix attempts: %v\n%s", pr.Number, r.cfg.MaxRetries, err, prURL))
		return true, err
	}

	// Step 7: Commit and push
	sha, err := r.commitAndPush(ctx, commitPrefix+": address code review feedback")
	if err != nil {
		r.cleanupWorkingTree()
		if strings.Contains(err.Error(), "no changes to commit") {
			r.log.Println("claude applied fixes but no file changes detected; continuing to poll")
			return true, nil
		}
		return true, fmt.Errorf("commit/push failed (will retry next cycle): %w", err)
	}
	r.log.Printf("pushed commit %s", sha)
	r.postPRComment(ctx, pr, fmt.Sprintf("Applied review fixes in %s", sha))

	return true, nil
}

func (r *Runner) fixCI(ctx context.Context, pr *PRInfo, ciLogs string) (bool, error) {
	if err := r.gitPull(ctx); err != nil {
		return true, fmt.Errorf("pulling latest: %w", err)
	}

	// Feed CI logs to Claude before running local verification
	_, err := r.claude.FixBuildErrors(ctx, ciLogs, 1)
	if err != nil {
		return true, fmt.Errorf("claude CI fix failed: %w", err)
	}

	if err := r.verifyWithRetry(ctx, 1); err != nil {
		r.cleanupWorkingTree()
		prURL := fmt.Sprintf("https://github.com/%s/%s/pull/%d", pr.Owner, pr.Repo, pr.Number)
		r.notifyTelegram(fmt.Sprintf("❌ ralph gave up on CI fix for PR #%d after %d fix attempts: %v\n%s",
			pr.Number, r.cfg.MaxRetries, err, prURL))
		return true, err
	}

	sha, err := r.commitAndPush(ctx, "fix: address CI pipeline failures")
	if err != nil {
		r.cleanupWorkingTree()
		if strings.Contains(err.Error(), "no changes to commit") {
			r.log.Println("claude applied CI fix but no file changes detected; continuing to poll")
			return true, nil
		}
		return true, fmt.Errorf("commit/push failed (will retry next cycle): %w", err)
	}
	r.log.Printf("CI fix pushed in %s — restarting loop", sha)
	r.postPRComment(ctx, pr, fmt.Sprintf("Applied CI pipeline fixes in %s", sha))

	return true, nil
}

func (r *Runner) cleanupWorkingTree() {
	cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cleanupCancel()
	if _, _, exit, err := r.runner.Run(cleanupCtx, r.cfg.Dir, nil, "git", "restore", "--staged", "."); err != nil || exit != 0 {
		r.log.Printf("warning: git restore --staged failed during cleanup")
	}
	if _, _, exit, err := r.runner.Run(cleanupCtx, r.cfg.Dir, nil, "git", "restore", "."); err != nil || exit != 0 {
		r.log.Printf("warning: git restore failed during cleanup")
	}
	if _, _, exit, err := r.runner.Run(cleanupCtx, r.cfg.Dir, nil, "git", "clean", "-fd"); err != nil || exit != 0 {
		r.log.Printf("warning: git clean failed during cleanup")
	}
}

func (r *Runner) gitPull(ctx context.Context) error {
	_, stderr, exitCode, err := r.runner.Run(ctx, r.cfg.Dir, nil, "git", "pull", "--rebase")
	if err != nil {
		return fmt.Errorf("git pull: %w", err)
	}
	if exitCode != 0 {
		return fmt.Errorf("git pull failed: %s", strings.TrimSpace(stderr))
	}
	return nil
}

func (r *Runner) verifyWithRetry(ctx context.Context, startAttempt int) error {
	for attempt := startAttempt + 1; ; attempt++ {
		passed, output, err := r.runTestsAndBuilds(ctx)
		if err != nil {
			return fmt.Errorf("verification error: %w", err)
		}
		if passed {
			r.log.Println("all tests and builds passed")
			return nil
		}

		if attempt > r.cfg.MaxRetries {
			return fmt.Errorf("tests/builds still failing after %d fix attempts; fix manually and re-run ralph", r.cfg.MaxRetries)
		}

		r.log.Printf("tests/builds failed (fix attempt %d/%d), asking claude to fix...", attempt, r.cfg.MaxRetries)
		_, err = r.claude.FixBuildErrors(ctx, output, attempt)
		if err != nil {
			return fmt.Errorf("claude fix attempt %d failed: %w", attempt, err)
		}
	}
}

func (r *Runner) runTestsAndBuilds(ctx context.Context) (bool, string, error) {
	var output strings.Builder

	// Run tests
	stdout, stderr, exitCode, err := r.runner.Run(ctx, r.cfg.Dir, nil, "go", "test", "./...")
	if err != nil {
		return false, "", fmt.Errorf("running tests: %w", err)
	}
	output.WriteString("=== go test ./... ===\n")
	output.WriteString(stdout)
	output.WriteString(stderr)
	if exitCode != 0 {
		return false, output.String(), nil
	}

	// Cross-platform builds
	targets := []struct{ goos, goarch string }{
		{"linux", "amd64"},
		{"darwin", "arm64"},
		{"windows", "amd64"},
	}
	for _, t := range targets {
		env := []string{
			"CGO_ENABLED=0",
			fmt.Sprintf("GOOS=%s", t.goos),
			fmt.Sprintf("GOARCH=%s", t.goarch),
		}
		for _, pkg := range []string{"./cmd/rcodbot", "./cmd/ralph"} {
			stdout, stderr, exitCode, err = r.runner.Run(ctx, r.cfg.Dir, env, "go", "build", pkg)
			output.WriteString(fmt.Sprintf("\n=== build %s %s/%s ===\n", pkg, t.goos, t.goarch))
			output.WriteString(stdout)
			output.WriteString(stderr)
			if err != nil {
				return false, "", fmt.Errorf("building %s %s/%s: %w", pkg, t.goos, t.goarch, err)
			}
			if exitCode != 0 {
				return false, output.String(), nil
			}
		}
	}

	return true, output.String(), nil
}

func (r *Runner) commitAndPush(ctx context.Context, msg string) (string, error) {
	// Stage modified tracked files
	diffOut, stderr, exitCode, err := r.runner.Run(ctx, r.cfg.Dir, nil, "git", "diff", "--name-only")
	if err != nil {
		return "", fmt.Errorf("git diff --name-only failed: %w", err)
	}
	if exitCode != 0 {
		return "", fmt.Errorf("git diff --name-only failed (exit %d): %s", exitCode, strings.TrimSpace(stderr))
	}

	var files []string
	for _, f := range strings.Split(strings.TrimSpace(diffOut), "\n") {
		if f == "" {
			continue
		}
		// Block modifications to files in sensitive directories.
		if strings.HasPrefix(f, ".github/") || strings.HasPrefix(f, ".git/") {
			r.log.Printf("warning: refusing to stage Claude-modified file in sensitive path: %s", f)
			continue
		}
		files = append(files, f)
	}

	// Also include untracked files created by Claude
	newOut, _, _, _ := r.runner.Run(ctx, r.cfg.Dir, nil, "git", "ls-files", "--others", "--exclude-standard")
	for _, f := range strings.Split(strings.TrimSpace(newOut), "\n") {
		if f == "" {
			continue
		}
		// Block creation of files in sensitive directories.
		if strings.HasPrefix(f, ".github/") || strings.HasPrefix(f, ".git/") {
			r.log.Printf("warning: refusing to stage Claude-created file in sensitive path: %s", f)
			continue
		}
		files = append(files, f)
	}

	if len(files) == 0 {
		// Log all untracked files (including gitignored ones) so the operator
		// can tell whether Claude wrote files that were silently dropped.
		allNew, _, _, _ := r.runner.Run(ctx, r.cfg.Dir, nil, "git", "ls-files", "--others")
		if trimmed := strings.TrimSpace(allNew); trimmed != "" {
			r.log.Printf("warning: no stageable changes, but these untracked (possibly gitignored) files exist:\n%s", trimmed)
		}
		return "", fmt.Errorf("no changes to commit")
	}

	addArgs := append([]string{"add", "--"}, files...)
	_, stderr, exitCode, err = r.runner.Run(ctx, r.cfg.Dir, nil, "git", addArgs...)
	if err != nil {
		return "", fmt.Errorf("git add failed: %w", err)
	}
	if exitCode != 0 {
		return "", fmt.Errorf("git add failed (exit %d): %s", exitCode, strings.TrimSpace(stderr))
	}

	// Log staged changes for operator visibility
	statOut, _, _, _ := r.runner.Run(ctx, r.cfg.Dir, nil, "git", "diff", "--stat", "--cached")
	if statOut != "" {
		r.log.Printf("staged changes:\n%s", statOut)
	}

	// Check if there are staged changes (--quiet exits 1 when changes exist)
	_, _, diffExitCode, diffErr := r.runner.Run(ctx, r.cfg.Dir, nil, "git", "diff", "--cached", "--quiet")
	if diffErr != nil {
		return "", fmt.Errorf("git diff --cached failed: %w", diffErr)
	}
	if diffExitCode == 0 {
		return "", fmt.Errorf("no changes to commit")
	}

	_, stderr, exitCode, err = r.runner.Run(ctx, r.cfg.Dir, nil, "git", "commit", "-m", msg)
	if err != nil || exitCode != 0 {
		return "", fmt.Errorf("git commit failed: %s", strings.TrimSpace(stderr))
	}

	// Push
	_, stderr, exitCode, err = r.runner.Run(ctx, r.cfg.Dir, nil, "git", "push")
	if err != nil || exitCode != 0 {
		// Try pull --rebase then push
		_, rebaseStderr, rebaseExit, rebaseErr := r.runner.Run(ctx, r.cfg.Dir, nil, "git", "pull", "--rebase")
		if rebaseErr != nil || rebaseExit != 0 {
			r.runner.Run(ctx, r.cfg.Dir, nil, "git", "rebase", "--abort") //nolint:errcheck
			return "", fmt.Errorf("git pull --rebase failed: %s", strings.TrimSpace(rebaseStderr))
		}
		_, stderr, exitCode, err = r.runner.Run(ctx, r.cfg.Dir, nil, "git", "push")
		if err != nil || exitCode != 0 {
			return "", fmt.Errorf("git push failed: %s", strings.TrimSpace(stderr))
		}
	}

	// Read SHA after push to capture the post-rebase SHA if the fallback path rewrote the commit
	shaOut, _, _, _ := r.runner.Run(ctx, r.cfg.Dir, nil, "git", "rev-parse", "--short", "HEAD")
	sha := strings.TrimSpace(shaOut)

	return sha, nil
}

var (
	confidenceHTMLRe = regexp.MustCompile(`(?i)<h3>\s*Confidence\s+Score:\s*(\d+)\s*/\s*5\s*</h3>`)
	confidenceMDRe   = regexp.MustCompile(`(?im)^#{1,4}\s*Confidence\s+Score:\s*(\d+)\s*/\s*5\s*$`)
)

func parseConfidenceScore(body string) int {
	// Try HTML format first (current Greptile output)
	if m := confidenceHTMLRe.FindStringSubmatch(body); len(m) >= 2 {
		score, _ := strconv.Atoi(m[1])
		return score
	}
	// Fallback: markdown heading format
	if m := confidenceMDRe.FindStringSubmatch(body); len(m) >= 2 {
		score, _ := strconv.Atoi(m[1])
		return score
	}
	return 0
}

func (r *Runner) postPRComment(ctx context.Context, pr *PRInfo, body string) {
	_, stderr, exitCode, err := r.runner.Run(ctx, r.cfg.Dir, nil,
		"gh", "pr", "comment", strconv.Itoa(pr.Number),
		"--repo", pr.Owner+"/"+pr.Repo,
		"--body", body)
	if err != nil || exitCode != 0 {
		r.log.Printf("warning: failed to post PR comment: %s", strings.TrimSpace(stderr))
	}
}

func (r *Runner) notifyTelegram(message string) {
	if r.cfg.TelegramToken == "" || r.cfg.TelegramUserID == 0 {
		return
	}

	payload, _ := json.Marshal(map[string]interface{}{
		"chat_id": r.cfg.TelegramUserID,
		"text":    message,
	})

	// Use an independent context so the notification is sent even if the parent context is cancelled
	notifyCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	url := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", r.cfg.TelegramToken)
	req, err := http.NewRequestWithContext(notifyCtx, "POST", url, bytes.NewReader(payload))
	if err != nil {
		r.log.Printf("telegram notification failed: %v", err)
		return
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		r.log.Printf("telegram notification failed: %v", err)
		return
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		r.log.Printf("telegram notification failed: status %d, body: %s", resp.StatusCode, string(body))
		return
	}
	r.log.Println("telegram notification sent")
}

// TelegramCreds holds Telegram bot credentials loaded from config.yaml.
type TelegramCreds struct {
	Token  string
	UserID int64
}

// LoadTelegramConfig reads telegram credentials from a config.yaml file.
// Returns an error if the file doesn't exist or has no telegram config.
func LoadTelegramConfig(path string) (*TelegramCreds, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var raw struct {
		Telegram struct {
			Token         string `yaml:"token"`
			AllowedUserID int64  `yaml:"allowed_user_id"`
		} `yaml:"telegram"`
	}
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return nil, err
	}
	if raw.Telegram.Token == "" || raw.Telegram.AllowedUserID == 0 {
		return nil, fmt.Errorf("no telegram credentials in config")
	}
	return &TelegramCreds{
		Token:  raw.Telegram.Token,
		UserID: raw.Telegram.AllowedUserID,
	}, nil
}

func truncate(s string, maxLen int) string {
	s = strings.TrimSpace(s)
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}
