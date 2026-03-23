package ralph

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"strconv"
	"strings"
	"time"
)

// PRInfo holds PR identification extracted from branch or URL.
type PRInfo struct {
	Owner  string
	Repo   string
	Number int
}

// PRStatus holds the current state of a PR.
type PRStatus struct {
	State   string
	HeadSHA string
}

// GitHubClient wraps gh CLI calls for PR interactions.
type GitHubClient struct {
	runner         CmdRunner
	dir            string
	ciWorkflowName string // defaults to "build" if empty
	log            *log.Logger
}

// NewGitHubClient creates a new GitHubClient.
func NewGitHubClient(runner CmdRunner, dir string) *GitHubClient {
	return &GitHubClient{
		runner: runner,
		dir:    dir,
		log:    log.New(io.Discard, "", 0),
	}
}

// GetPRFromBranch auto-detects PR info from the current branch.
func (g *GitHubClient) GetPRFromBranch(ctx context.Context) (*PRInfo, error) {
	stdout, stderr, exitCode, err := g.runner.Run(ctx, g.dir, nil,
		"gh", "pr", "view", "--json", "number,url", "--jq", `{number,url}`)
	if err != nil {
		return nil, fmt.Errorf("running gh pr view: %w", err)
	}
	if exitCode != 0 {
		return nil, fmt.Errorf("gh pr view failed (exit %d): %s", exitCode, strings.TrimSpace(stderr))
	}

	var result struct {
		Number int    `json:"number"`
		URL    string `json:"url"`
	}
	if err := json.Unmarshal([]byte(stdout), &result); err != nil {
		return nil, fmt.Errorf("parsing gh pr view output: %w", err)
	}

	info, err := parsePRURL(result.URL)
	if err != nil {
		return nil, err
	}
	info.Number = result.Number
	return info, nil
}

// ParsePRRef parses a PR URL like https://github.com/owner/repo/pull/123
// into PRInfo.
func ParsePRRef(ref string) (*PRInfo, error) {
	return parsePRURL(ref)
}

func parsePRURL(url string) (*PRInfo, error) {
	// Expected: https://github.com/owner/repo/pull/123
	url = strings.TrimSuffix(url, "/")
	parts := strings.Split(url, "/")
	if len(parts) < 5 {
		return nil, fmt.Errorf("invalid PR URL: %s", url)
	}

	pullIdx := -1
	for i, p := range parts {
		if p == "pull" {
			pullIdx = i
			break
		}
	}
	if pullIdx < 0 || pullIdx+1 >= len(parts) || pullIdx < 2 {
		return nil, fmt.Errorf("invalid PR URL: %s", url)
	}

	number, err := strconv.Atoi(parts[pullIdx+1])
	if err != nil {
		return nil, fmt.Errorf("invalid PR number in URL: %s", url)
	}

	return &PRInfo{
		Owner:  parts[pullIdx-2],
		Repo:   parts[pullIdx-1],
		Number: number,
	}, nil
}

// GetPRStatus returns the current state and head SHA of a PR.
func (g *GitHubClient) GetPRStatus(ctx context.Context, pr *PRInfo) (*PRStatus, error) {
	stdout, stderr, exitCode, err := g.runner.Run(ctx, g.dir, nil,
		"gh", "pr", "view", strconv.Itoa(pr.Number),
		"--repo", pr.Owner+"/"+pr.Repo,
		"--json", "state,headRefOid",
		"--jq", `{state,headRefOid}`)
	if err != nil {
		return nil, fmt.Errorf("running gh pr view: %w", err)
	}
	if exitCode != 0 {
		return nil, fmt.Errorf("gh pr view failed (exit %d): %s", exitCode, strings.TrimSpace(stderr))
	}

	var result struct {
		State      string `json:"state"`
		HeadRefOid string `json:"headRefOid"`
	}
	if err := json.Unmarshal([]byte(stdout), &result); err != nil {
		return nil, fmt.Errorf("parsing PR status: %w", err)
	}

	return &PRStatus{
		State:   strings.ToUpper(result.State),
		HeadSHA: result.HeadRefOid,
	}, nil
}

// GetCommitDate returns the committer date for a given SHA.
func (g *GitHubClient) GetCommitDate(ctx context.Context, pr *PRInfo, sha string) (time.Time, error) {
	stdout, stderr, exitCode, err := g.runner.Run(ctx, g.dir, nil,
		"gh", "api", fmt.Sprintf("repos/%s/%s/commits/%s", pr.Owner, pr.Repo, sha),
		"--jq", `.commit.committer.date`)
	if err != nil {
		return time.Time{}, fmt.Errorf("fetching commit date: %w", err)
	}
	if exitCode != 0 {
		return time.Time{}, fmt.Errorf("gh api failed (exit %d): %s", exitCode, strings.TrimSpace(stderr))
	}

	t, err := time.Parse(time.RFC3339, strings.TrimSpace(stdout))
	if err != nil {
		return time.Time{}, fmt.Errorf("parsing commit date %q: %w", stdout, err)
	}
	return t, nil
}

// GetFailedCILogs checks CI runs for the PR branch. If the latest run matching
// the configured workflow name failed, it returns the failure logs. Returns
// empty string if CI is passing or still running.
func (g *GitHubClient) GetFailedCILogs(ctx context.Context, pr *PRInfo) (string, error) {
	branch, err := g.currentBranch(ctx)
	if err != nil {
		return "", fmt.Errorf("detecting branch: %w", err)
	}

	targetName := g.ciWorkflowName
	if targetName == "" {
		targetName = "build"
	}

	// Get the latest build run for the branch, scoped to the target workflow
	stdout, stderr, exitCode, err := g.runner.Run(ctx, g.dir, nil,
		"gh", "run", "list",
		"--repo", pr.Owner+"/"+pr.Repo,
		"--branch", branch,
		"--workflow", targetName,
		"--json", "databaseId,status,conclusion,name",
		"--limit", "5")
	if err != nil {
		return "", fmt.Errorf("listing CI runs: %w", err)
	}
	if exitCode != 0 {
		return "", fmt.Errorf("gh run list failed (exit %d): %s", exitCode, strings.TrimSpace(stderr))
	}

	var runs []struct {
		DatabaseID int64  `json:"databaseId"`
		Status     string `json:"status"`
		Conclusion string `json:"conclusion"`
		Name       string `json:"name"`
	}
	if err := json.Unmarshal([]byte(stdout), &runs); err != nil {
		return "", fmt.Errorf("parsing CI runs: %w", err)
	}

	// Normalize targetName by stripping file extension so that both
	// "build" and "build.yml" match the workflow's display name.
	normalizedTarget := strings.TrimSuffix(strings.TrimSuffix(targetName, ".yml"), ".yaml")

	// Find latest matching CI run
	for _, run := range runs {
		if !strings.EqualFold(run.Name, normalizedTarget) {
			continue
		}
		if run.Status != "completed" {
			return "", nil // still running, wait
		}
		if run.Conclusion == "success" {
			return "", nil // CI is green
		}

		// CI failed — fetch logs
		logOut, _, logExit, logErr := g.runner.Run(ctx, g.dir, nil,
			"gh", "run", "view", strconv.FormatInt(run.DatabaseID, 10),
			"--repo", pr.Owner+"/"+pr.Repo,
			"--log")
		if logErr != nil {
			return "", fmt.Errorf("fetching CI logs: %w", logErr)
		}
		if logExit != 0 {
			return "", fmt.Errorf("fetching CI logs: exit %d", logExit)
		}
		// Truncate to keep the log actionable without overwhelming Claude's context.
		const maxCILogBytes = 30_000
		if len(logOut) > maxCILogBytes {
			logOut = logOut[len(logOut)-maxCILogBytes:]
		}
		return logOut, nil
	}

	g.log.Printf("no CI workflow run named %q found in recent runs; treating as no known failures", normalizedTarget)
	return "", nil
}

func (g *GitHubClient) currentBranch(ctx context.Context) (string, error) {
	stdout, stderr, exitCode, err := g.runner.Run(ctx, g.dir, nil, "git", "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil || exitCode != 0 {
		return "", fmt.Errorf("git rev-parse HEAD: %s", strings.TrimSpace(stderr))
	}
	return strings.TrimSpace(stdout), nil
}

// FetchGreptileComment returns the body of the Greptile bot's issue comment
// if it was updated after since. Returns empty string if no comment found or
// it hasn't been updated since the last commit.
func (g *GitHubClient) FetchGreptileComment(ctx context.Context, pr *PRInfo, since time.Time) (string, error) {
	stdout, stderr, exitCode, err := g.runner.Run(ctx, g.dir, nil,
		"gh", "api",
		fmt.Sprintf("repos/%s/%s/issues/%d/comments?per_page=100&sort=updated&direction=desc", pr.Owner, pr.Repo, pr.Number),
		"--jq", `[.[] | select(.user.login | test("greptile"; "i"))] | sort_by(.updated_at) | last | {body, updated_at}`)
	if err != nil {
		return "", fmt.Errorf("fetching issue comments: %w", err)
	}
	if exitCode != 0 {
		return "", fmt.Errorf("gh api failed (exit %d): %s", exitCode, strings.TrimSpace(stderr))
	}

	stdout = strings.TrimSpace(stdout)
	if stdout == "" || stdout == "null" {
		return "", nil
	}

	var result struct {
		Body      string `json:"body"`
		UpdatedAt string `json:"updated_at"`
	}
	if err := json.Unmarshal([]byte(stdout), &result); err != nil {
		return "", fmt.Errorf("parsing greptile comment: %w", err)
	}

	if result.UpdatedAt == "" || result.Body == "" {
		return "", nil
	}

	updatedAt, err := time.Parse(time.RFC3339, result.UpdatedAt)
	if err != nil {
		return "", fmt.Errorf("parsing updated_at: %w", err)
	}

	if !updatedAt.After(since) {
		return "", nil
	}

	return result.Body, nil
}
