package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/zevro-ai/remote-control-on-demand/internal/ralph"
)

func main() {
	prURL := flag.String("pr", "", "PR URL (e.g. https://github.com/owner/repo/pull/123); auto-detects from current branch if omitted")
	poll := flag.Duration("poll", 3*time.Minute, "interval between polling for new review comments")
	retries := flag.Int("max-fix-attempts", 20, "max fix attempts per iteration when tests/builds fail")
	configPath := flag.String("config", "", "path to config.yaml (auto-detects in current directory if omitted)")
	ciWorkflow := flag.String("ci-workflow", "build", "name of the CI workflow to monitor for failures")
	flag.Parse()

	dir, err := os.Getwd()
	if err != nil {
		log.Fatalf("getting working directory: %v", err)
	}

	logger := log.New(os.Stderr, "[ralph] ", log.LstdFlags)

	cfg := ralph.Config{
		PollInterval:   *poll,
		MaxRetries:     *retries,
		Dir:            dir,
		CIWorkflowName: *ciWorkflow,
	}

	// Load Telegram credentials from config.yaml if available
	cfgFile := *configPath
	if cfgFile == "" {
		cfgFile = filepath.Join(dir, "config.yaml")
	}
	if tg, err := ralph.LoadTelegramConfig(cfgFile); err == nil {
		cfg.TelegramToken = tg.Token
		cfg.TelegramUserID = tg.UserID
		logger.Printf("telegram notifications enabled (user %d)", tg.UserID)
	}

	if *prURL != "" {
		pr, err := ralph.ParsePRRef(*prURL)
		if err != nil {
			logger.Fatalf("invalid PR URL: %v", err)
		}
		cfg.PROwner = pr.Owner
		cfg.PRRepo = pr.Repo
		cfg.PRNumber = pr.Number
	}
	runner := ralph.NewRunner(cfg, &ralph.DefaultRunner{}, logger)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		sig := <-sigCh
		logger.Printf("received %s, shutting down...", sig)
		cancel()
	}()

	fmt.Fprintln(os.Stderr, "ralph — automated code review fix loop")
	fmt.Fprintln(os.Stderr, "press Ctrl+C to stop")
	fmt.Fprintln(os.Stderr)

	if err := runner.Run(ctx); err != nil {
		if ctx.Err() != nil {
			logger.Println("stopped by user")
			os.Exit(0)
		}
		logger.Fatalf("ralph exited with error: %v", err)
	}
}
