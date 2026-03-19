package config

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"golang.org/x/term"
)

// ANSI escape codes
const (
	esc      = "\033["
	reset    = esc + "0m"
	fBold    = esc + "1m"
	fDim     = esc + "2m"
	fCyan    = esc + "36m"
	fGreen   = esc + "32m"
	fBrCyan  = esc + "96m"
	fBrWhite = esc + "97m"

	cursorHide = esc + "?25l"
	cursorShow = esc + "?25h"
	clearLine  = esc + "2K"
)

func styled(codes, s string) string { return codes + s + reset }

func printLogo() {
	c := fCyan
	b := fBold + fBrWhite
	d := fDim
	r := reset

	fmt.Println()
	fmt.Println(c + "  ╭──────────────────────────────────────────────────╮" + r)
	fmt.Println(c + "  │" + r + "                                                  " + c + "│" + r)
	fmt.Println(c + "  │" + r + "   " + b + "Remote Control On Demand" + r + "              " + d + "zevro.ai" + r + " " + c + "│" + r)
	fmt.Println(c + "  │" + r + "                                                  " + c + "│" + r)
	fmt.Println(c + "  ╰──────────────────────────────────────────────────╯" + r)
	fmt.Println()
}

func printSummaryHeader() {
	c := fCyan
	b := fBold + fBrWhite
	r := reset

	fmt.Println(c + "  ╭──────────────────────────────────────────────────╮" + r)
	fmt.Println(c + "  │" + r + "   " + b + "Configuration Summary" + r + "                          " + c + "│" + r)
	fmt.Println(c + "  ╰──────────────────────────────────────────────────╯" + r)
}

func stepLabel(n, total int) string {
	return styled(fBold+fBrCyan, fmt.Sprintf("  %d/%d", n, total))
}

func stepTitle(s string) string    { return styled(fBold+fBrWhite, s) }
func stepHint(s string) string     { return styled(fDim, "       "+s) }
func stepOK(s string) string       { return styled(fBold+fGreen, "  ✓ ") + s }
func promptArrow() string          { return styled(fBrCyan, "  › ") }
func summaryLabel(s string) string { return styled(fDim, s) }

// selectFromList renders an interactive menu navigable with arrow keys.
// Returns the index of the selected option.
func selectFromList(labels []string, defaultIdx int) (int, error) {
	fd := int(os.Stdin.Fd())
	oldState, err := term.MakeRaw(fd)
	if err != nil {
		return defaultIdx, err
	}
	defer term.Restore(fd, oldState)

	selected := defaultIdx
	total := len(labels)

	render := func() {
		for i, label := range labels {
			fmt.Print("\r" + clearLine)
			if i == selected {
				fmt.Print("       " + styled(fBrCyan, "›") + "  " + styled(fBold+fBrWhite, label))
			} else {
				fmt.Print("          " + styled(fDim, label))
			}
			fmt.Print("\r\n")
		}
	}

	fmt.Print(cursorHide)
	render()

	buf := make([]byte, 3)
	for {
		n, err := os.Stdin.Read(buf)
		if err != nil {
			fmt.Print(cursorShow)
			return 0, err
		}

		// Ctrl+C
		if n >= 1 && buf[0] == 3 {
			fmt.Print(cursorShow)
			term.Restore(fd, oldState)
			fmt.Println()
			os.Exit(1)
		}

		// Enter
		if n >= 1 && (buf[0] == '\r' || buf[0] == '\n') {
			fmt.Print(cursorShow)
			return selected, nil
		}

		// Arrow keys: ESC [ A/B
		if n >= 3 && buf[0] == '\033' && buf[1] == '[' {
			switch buf[2] {
			case 'A': // Up
				if selected > 0 {
					selected--
				}
			case 'B': // Down
				if selected < total-1 {
					selected++
				}
			}
		}

		// j/k vim-style
		if n == 1 {
			switch buf[0] {
			case 'k':
				if selected > 0 {
					selected--
				}
			case 'j':
				if selected < total-1 {
					selected++
				}
			}
		}

		// Move cursor up to beginning of list, then redraw
		fmt.Printf("\033[%dA", total)
		render()
	}
}

func RunOnboarding(configPath string) (*Config, error) {
	reader := bufio.NewReader(os.Stdin)

	printLogo()
	fmt.Println(styled(fDim, "  Welcome! Let's configure your bot in 5 quick steps."))
	fmt.Println()

	// ── 1/5 ── Telegram Bot Token ──────────────────────────────────
	fmt.Println(stepLabel(1, 4) + "  " + stepTitle("Telegram Bot Token"))
	fmt.Println(stepHint("Create a bot via @BotFather and paste the token."))
	fmt.Println()
	token, err := styledPrompt(reader)
	if err != nil {
		return nil, err
	}
	if token == "" {
		return nil, fmt.Errorf("token is required")
	}
	fmt.Println(stepOK("Token saved"))
	fmt.Println()

	// ── 2/5 ── Telegram User ID ────────────────────────────────────
	fmt.Println(stepLabel(2, 4) + "  " + stepTitle("Telegram User ID"))
	fmt.Println(stepHint("Message @userinfobot on Telegram to get your ID."))
	fmt.Println()
	userIDStr, err := styledPrompt(reader)
	if err != nil {
		return nil, err
	}
	userID, err := strconv.ParseInt(userIDStr, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("invalid user ID: %w", err)
	}
	fmt.Println(stepOK("User ID saved"))
	fmt.Println()

	// ── 3/5 ── Projects folder ─────────────────────────────────────
	cwd, _ := os.Getwd()
	fmt.Println(stepLabel(3, 4) + "  " + stepTitle("Projects folder"))
	fmt.Println(stepHint("Base directory containing your git repos."))
	fmt.Println(stepHint("Press Enter to use " + styled(fBrWhite, cwd)))
	fmt.Println()
	baseFolderInput, err := styledPrompt(reader)
	if err != nil {
		return nil, err
	}
	baseFolder := baseFolderInput
	if baseFolder == "" {
		baseFolder = cwd
	}
	info, err := os.Stat(baseFolder)
	if err != nil || !info.IsDir() {
		return nil, fmt.Errorf("folder %q does not exist or is not a directory", baseFolder)
	}
	fmt.Println(stepOK("Folder verified"))
	fmt.Println()

	// ── 4/5 ── Auto-restart ────────────────────────────────────────
	fmt.Println(stepLabel(4, 5) + "  " + stepTitle("Auto-restart"))
	fmt.Println(stepHint("Restart crashed sessions automatically?"))
	fmt.Println()
	autoIdx, err := selectFromList([]string{"Yes", "No"}, 0)
	if err != nil {
		return nil, err
	}
	autoRestart := autoIdx == 0

	maxRestarts := 3
	restartDelay := 5
	if autoRestart {
		fmt.Println()
		fmt.Print(styled(fDim, "       Max restarts [3]: "))
		maxRestartsStr, err := readLine(reader)
		if err != nil {
			return nil, err
		}
		if maxRestartsStr != "" {
			maxRestarts, err = strconv.Atoi(maxRestartsStr)
			if err != nil {
				return nil, fmt.Errorf("invalid max restarts: %w", err)
			}
		}

		fmt.Print(styled(fDim, "       Restart delay in seconds [5]: "))
		delayStr, err := readLine(reader)
		if err != nil {
			return nil, err
		}
		if delayStr != "" {
			restartDelay, err = strconv.Atoi(delayStr)
			if err != nil {
				return nil, fmt.Errorf("invalid delay: %w", err)
			}
		}
	}

	if autoRestart {
		fmt.Println(stepOK(fmt.Sprintf("Auto-restart enabled (%d max, %ds delay)", maxRestarts, restartDelay)))
	} else {
		fmt.Println(stepOK("Auto-restart disabled"))
	}
	fmt.Println()

	// ── 5/5 ── Progress updates ────────────────────────────────────
	fmt.Println(stepLabel(5, 5) + "  " + stepTitle("Progress updates"))
	fmt.Println(stepHint("Send Telegram progress updates for running sessions?"))
	fmt.Println()
	progressIdx, err := selectFromList([]string{"Yes", "No"}, 0)
	if err != nil {
		return nil, err
	}

	var notifications *NotificationsConfig
	if progressIdx == 0 {
		progressIntervalMinutes := 10
		fmt.Println()
		fmt.Print(styled(fDim, "       Update every how many minutes? [10]: "))
		intervalStr, err := readLine(reader)
		if err != nil {
			return nil, err
		}
		if intervalStr != "" {
			progressIntervalMinutes, err = strconv.Atoi(intervalStr)
			if err != nil {
				return nil, fmt.Errorf("invalid progress update interval: %w", err)
			}
		}
		if progressIntervalMinutes <= 0 {
			return nil, fmt.Errorf("progress update interval must be > 0")
		}

		notifications = &NotificationsConfig{
			ProgressUpdateInterval: Duration(time.Duration(progressIntervalMinutes) * time.Minute),
		}
		fmt.Println(stepOK(fmt.Sprintf("Progress updates enabled (%dm interval)", progressIntervalMinutes)))
	} else {
		fmt.Println(stepOK("Progress updates disabled"))
	}
	fmt.Println()

	cfg := &Config{
		Telegram: TelegramConfig{
			Token:         token,
			AllowedUserID: userID,
		},
		RC: RCConfig{
			BaseFolder:          baseFolder,
			PermissionMode:      DefaultCodexPermissionMode,
			AutoRestart:         autoRestart,
			MaxRestarts:         maxRestarts,
			RestartDelaySeconds: restartDelay,
			Notifications:       notifications,
		},
	}

	// ── Summary ────────────────────────────────────────────────────
	printSummaryHeader()
	fmt.Println()
	tokenPreview := redactToken(token)
	fmt.Printf("    %s  %s\n", summaryLabel("Token          "), tokenPreview)
	fmt.Printf("    %s  %d\n", summaryLabel("User ID        "), userID)
	fmt.Printf("    %s  %s\n", summaryLabel("Projects folder"), baseFolder)
	fmt.Printf("    %s  %s\n", summaryLabel("Codex access   "), cfg.RC.PermissionMode)
	if autoRestart {
		fmt.Printf("    %s  enabled (%d max, %ds delay)\n", summaryLabel("Auto-restart   "), maxRestarts, restartDelay)
	} else {
		fmt.Printf("    %s  disabled\n", summaryLabel("Auto-restart   "))
	}
	if notifications != nil && notifications.ProgressUpdateInterval > 0 {
		fmt.Printf("    %s  every %s\n", summaryLabel("Progress       "), time.Duration(notifications.ProgressUpdateInterval))
	} else {
		fmt.Printf("    %s  disabled\n", summaryLabel("Progress       "))
	}
	fmt.Println()

	if err := cfg.Save(configPath); err != nil {
		return nil, fmt.Errorf("saving config: %w", err)
	}
	fmt.Println(stepOK("Config saved to " + configPath))
	fmt.Println()

	// ── Start now? ─────────────────────────────────────────────────
	fmt.Println(styled(fBold, "  Start the bot now?"))
	fmt.Println()
	startIdx, err := selectFromList([]string{"Yes", "No"}, 0)
	if err != nil {
		return nil, err
	}
	if startIdx == 1 {
		fmt.Println()
		fmt.Println(styled(fDim, "  OK, start later with: go run ./cmd/codexbot"))
		fmt.Println()
		os.Exit(0)
	}

	fmt.Println()
	return cfg, nil
}

func styledPrompt(reader *bufio.Reader) (string, error) {
	fmt.Print(promptArrow())
	return readLine(reader)
}

func readLine(reader *bufio.Reader) (string, error) {
	text, err := reader.ReadString('\n')
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(text), nil
}

func redactToken(token string) string {
	if len(token) <= 8 {
		return strings.Repeat("*", len(token))
	}
	return token[:4] + "..." + token[len(token)-4:]
}
