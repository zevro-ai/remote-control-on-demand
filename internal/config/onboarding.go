package config

import (
	"bufio"
	"crypto/rand"
	"encoding/hex"
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

const defaultDashboardPort = 3001

var (
	onboardingSelectOption  = selectFromList
	onboardingGetwd         = os.Getwd
	onboardingExit          = os.Exit
	onboardingGenerateToken = generateOnboardingToken
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
	fmt.Println(styled(fDim, "  Welcome! Let's configure RCOD in 6 quick steps."))
	fmt.Println()

	// ── 1/6 ── Telegram Bot Token ──────────────────────────────────
	fmt.Println(stepLabel(1, 6) + "  " + stepTitle("Telegram Bot Token"))
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

	// ── 2/6 ── Telegram User ID ────────────────────────────────────
	fmt.Println(stepLabel(2, 6) + "  " + stepTitle("Telegram User ID"))
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

	// ── 3/6 ── Projects folder ─────────────────────────────────────
	cwd, _ := onboardingGetwd()
	fmt.Println(stepLabel(3, 6) + "  " + stepTitle("Projects folder"))
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

	// ── 4/6 ── Auto-restart ────────────────────────────────────────
	fmt.Println(stepLabel(4, 6) + "  " + stepTitle("Auto-restart"))
	fmt.Println(stepHint("Restart crashed sessions automatically?"))
	fmt.Println()
	autoIdx, err := onboardingSelectOption([]string{"Yes", "No"}, 0)
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

	// ── 5/6 ── Progress updates ────────────────────────────────────
	fmt.Println(stepLabel(5, 6) + "  " + stepTitle("Progress updates"))
	fmt.Println(stepHint("Send Telegram progress updates for running sessions?"))
	fmt.Println()
	progressIdx, err := onboardingSelectOption([]string{"Yes", "No"}, 0)
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

	// ── 6/6 ── Built-in dashboard ──────────────────────────────────
	fmt.Println(stepLabel(6, 6) + "  " + stepTitle("Built-in dashboard"))
	fmt.Println(stepHint("RCOD can serve its dashboard directly from the same binary."))
	fmt.Println(stepHint("Enable it now to browse sessions from a web UI without editing config.yaml later."))
	fmt.Println()
	dashboardIdx, err := onboardingSelectOption([]string{"Yes", "No"}, 0)
	if err != nil {
		return nil, err
	}

	apiCfg := APIConfig{}
	if dashboardIdx == 0 {
		fmt.Println()
		fmt.Print(styled(fDim, fmt.Sprintf("       Dashboard port [%d]: ", defaultDashboardPort)))
		portStr, err := readLine(reader)
		if err != nil {
			return nil, err
		}

		apiCfg.Port = defaultDashboardPort
		if portStr != "" {
			apiCfg.Port, err = strconv.Atoi(portStr)
			if err != nil {
				return nil, fmt.Errorf("invalid dashboard port: %w", err)
			}
		}
		if apiCfg.Port <= 0 || apiCfg.Port > 65535 {
			return nil, fmt.Errorf("dashboard port must be between 1 and 65535")
		}

		fmt.Println()
		fmt.Println(stepHint("Protect the dashboard with a bearer token?"))
		fmt.Println()
		tokenIdx, err := onboardingSelectOption([]string{"Yes", "No"}, 0)
		if err != nil {
			return nil, err
		}

		if tokenIdx == 0 {
			fmt.Println()
			fmt.Println(stepHint("Press Enter to generate a token automatically."))
			fmt.Print(styled(fDim, "       Dashboard token [auto-generated]: "))
			apiToken, err := readLine(reader)
			if err != nil {
				return nil, err
			}
			if apiToken == "" {
				apiToken, err = onboardingGenerateToken()
				if err != nil {
					return nil, fmt.Errorf("generating dashboard token: %w", err)
				}
			}
			apiCfg.Token = apiToken
			fmt.Println(stepOK("Dashboard enabled at " + dashboardURL(apiCfg.Port) + " (token protected)"))
		} else {
			fmt.Println(stepOK("Dashboard enabled at " + dashboardURL(apiCfg.Port)))
		}
	} else {
		fmt.Println(stepOK("Dashboard disabled"))
	}
	fmt.Println()

	cfg := &Config{
		Telegram: TelegramConfig{
			Token:         token,
			AllowedUserID: userID,
		},
		API: apiCfg,
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
	if cfg.API.Port > 0 {
		fmt.Printf("    %s  %s\n", summaryLabel("Dashboard      "), dashboardURL(cfg.API.Port))
		if cfg.API.Token != "" {
			fmt.Printf("    %s  bearer token enabled (%s)\n", summaryLabel("Dashboard auth "), redactToken(cfg.API.Token))
		} else {
			fmt.Printf("    %s  disabled\n", summaryLabel("Dashboard auth "))
		}
	} else {
		fmt.Printf("    %s  disabled\n", summaryLabel("Dashboard      "))
	}
	fmt.Println()

	if err := cfg.Save(configPath); err != nil {
		return nil, fmt.Errorf("saving config: %w", err)
	}
	fmt.Println(stepOK("Config saved to " + configPath))
	if cfg.API.Port > 0 {
		fmt.Println(stepOK("Dashboard URL: " + dashboardURL(cfg.API.Port)))
		if cfg.API.Token != "" {
			fmt.Println(stepHint("Bearer token saved to config.yaml."))
		}
	}
	fmt.Println()

	// ── Start now? ─────────────────────────────────────────────────
	fmt.Println(styled(fBold, "  Start the bot now?"))
	fmt.Println()
	startIdx, err := onboardingSelectOption([]string{"Yes", "No"}, 0)
	if err != nil {
		return nil, err
	}
	if startIdx == 1 {
		fmt.Println()
		fmt.Println(styled(fDim, "  OK, start later with: go run ./cmd/codexbot"))
		fmt.Println()
		onboardingExit(0)
		return nil, nil
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

func dashboardURL(port int) string {
	return fmt.Sprintf("http://127.0.0.1:%d/", port)
}

func generateOnboardingToken() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}
