package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/zevro-ai/remote-control-on-demand/internal/bot"
	"github.com/zevro-ai/remote-control-on-demand/internal/config"
	"github.com/zevro-ai/remote-control-on-demand/internal/process"
	"github.com/zevro-ai/remote-control-on-demand/internal/runtimepaths"
	"github.com/zevro-ai/remote-control-on-demand/internal/session"
)

const (
	esc = "\033["
	r   = esc + "0m"
	b1  = esc + "1m"
	dim = esc + "2m"
	cyn = esc + "36m"
	grn = esc + "32m"
	whi = esc + "97m"
	bcn = esc + "96m"
)

func printBanner() {
	c := bcn
	w := b1 + whi
	d := dim
	g := grn

	fmt.Println()
	fmt.Println(c + "    ██████╗  ██████╗ ██████╗ ██████╗ " + r)
	fmt.Println(c + "    ██╔══██╗██╔════╝██╔═══██╗██╔══██╗" + r)
	fmt.Println(c + "    ██████╔╝██║     ██║   ██║██║  ██║" + r)
	fmt.Println(c + "    ██╔══██╗██║     ██║   ██║██║  ██║" + r)
	fmt.Println(c + "    ██║  ██║╚██████╗╚██████╔╝██████╔╝" + r)
	fmt.Println(c + "    ╚═╝  ╚═╝ ╚═════╝ ╚═════╝ ╚═════╝ " + r)
	fmt.Println()
	fmt.Println(w + "    Remote Control On Demand" + r)
	fmt.Println(d + "    Manage Claude sessions via Telegram" + r)
	fmt.Println()
	fmt.Println(d + "    ─────────────────────────────────────" + r)
	fmt.Println(d + "    Powered by " + r + w + "zevro.ai" + r)
	fmt.Println(d + "    ─────────────────────────────────────" + r)
	fmt.Println()
	fmt.Println(g + "    ● " + r + "Bot is running" + d + "              Ctrl+C to stop" + r)
	fmt.Println()
}

func main() {
	configPath := flag.String("config", "config.yaml", "path to config file")
	stateDir := flag.String("state-dir", "", "directory for runtime state files (defaults to the config directory)")
	flag.Parse()

	var cfg *config.Config
	var err error

	if !config.Exists(*configPath) {
		cfg, err = config.RunOnboarding(*configPath)
		if err != nil {
			log.Fatalf("Onboarding failed: %v", err)
		}
		if cfg == nil {
			return
		}
	} else {
		cfg, err = config.Load(*configPath)
		if err != nil {
			log.Fatalf("Loading config: %v", err)
		}
	}

	runner := process.NewRunner()
	resolvedStateDir := runtimepaths.ResolveStateDir(*configPath, *stateDir)
	if err := os.MkdirAll(resolvedStateDir, 0700); err != nil {
		log.Fatalf("Creating state directory: %v", err)
	}

	statePath := runtimepaths.ResolveStatePath(*configPath, *stateDir, "sessions.json")
	mgr := session.NewManager(
		runner,
		cfg.RC.BaseFolder,
		statePath,
		cfg.RC.AutoRestart,
		cfg.RC.MaxRestarts,
		time.Duration(cfg.RC.RestartDelaySeconds)*time.Second,
		cfg.RC.Notifications,
	)

	if err := mgr.Restore(); err != nil {
		log.Printf("Warning: Failed to restore sessions: %v", err)
	}

	bt, err := bot.New(cfg.Telegram.Token, cfg.Telegram.AllowedUserID, mgr)
	if err != nil {
		log.Fatalf("Creating bot: %v", err)
	}

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigCh
		fmt.Println()
		fmt.Println(dim + "    Shutting down..." + r)
		stopped := mgr.StopAll()
		bt.SendMessage(fmt.Sprintf("Bot stopped, %d sessions closed.", stopped))
		bt.Stop()
	}()

	printBanner()
	bt.Start()
}
