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

	"github.com/zevro-ai/remote-control-on-demand/internal/claudechat"
	"github.com/zevro-ai/remote-control-on-demand/internal/codex"
	"github.com/zevro-ai/remote-control-on-demand/internal/codexbot"
	"github.com/zevro-ai/remote-control-on-demand/internal/config"
	"github.com/zevro-ai/remote-control-on-demand/internal/httpapi"
	"github.com/zevro-ai/remote-control-on-demand/internal/process"
	"github.com/zevro-ai/remote-control-on-demand/internal/session"
)

const (
	esc = "\033["
	r   = esc + "0m"
	b1  = esc + "1m"
	dim = esc + "2m"
	ylw = esc + "33m"
	whi = esc + "97m"
	byl = esc + "93m"
)

func printBanner() {
	c := byl
	w := b1 + whi
	d := dim

	fmt.Println()
	fmt.Println(c + "    ██████╗ ██████╗ ██████╗ ███████╗██╗  ██╗" + r)
	fmt.Println(c + "   ██╔════╝██╔═══██╗██╔══██╗██╔════╝╚██╗██╔╝" + r)
	fmt.Println(c + "   ██║     ██║   ██║██║  ██║█████╗   ╚███╔╝ " + r)
	fmt.Println(c + "   ██║     ██║   ██║██║  ██║██╔══╝   ██╔██╗ " + r)
	fmt.Println(c + "   ╚██████╗╚██████╔╝██████╔╝███████╗██╔╝ ██╗" + r)
	fmt.Println(c + "    ╚═════╝ ╚═════╝ ╚═════╝ ╚══════╝╚═╝  ╚═╝" + r)
	fmt.Println()
	fmt.Println(w + "    Codex Telegram Bot" + r)
	fmt.Println(d + "    Chat with Codex inside your repositories" + r)
	fmt.Println()
	fmt.Println(ylw + "    ● " + r + "Bot is running" + d + "              Ctrl+C to stop" + r)
	fmt.Println()
}

func main() {
	configPath := flag.String("config", "config.yaml", "path to config file")
	flag.Parse()

	var cfg *config.Config
	var err error

	if !config.Exists(*configPath) {
		cfg, err = config.RunOnboarding(*configPath)
		if err != nil {
			log.Fatalf("Onboarding failed: %v", err)
		}
	} else {
		cfg, err = config.Load(*configPath)
		if err != nil {
			log.Fatalf("Loading config: %v", err)
		}
	}

	runner := process.NewRunner()
	sessionStatePath := filepath.Join(filepath.Dir(*configPath), "sessions.json")
	sessionMgr := session.NewManager(
		runner,
		cfg.RC.BaseFolder,
		sessionStatePath,
		cfg.RC.AutoRestart,
		cfg.RC.MaxRestarts,
		time.Duration(cfg.RC.RestartDelaySeconds)*time.Second,
		cfg.RC.Notifications,
	)
	if err := sessionMgr.Restore(); err != nil {
		log.Printf("Warning: Failed to restore Claude sessions: %v", err)
	}

	codexStatePath := filepath.Join(filepath.Dir(*configPath), "codex_sessions.json")
	codexMgr := codex.NewManager(cfg.RC.BaseFolder, codexStatePath)
	codexMgr.ConfigurePermissionMode(cfg.RC.PermissionMode)
	if err := codexMgr.Restore(); err != nil {
		log.Fatalf("Restoring Codex sessions: %v", err)
	}

	claudeStatePath := filepath.Join(filepath.Dir(*configPath), "claude_sessions.json")
	claudeMgr := claudechat.NewManager(cfg.RC.BaseFolder, claudeStatePath)
	claudeMgr.ConfigurePermissionMode(cfg.RC.PermissionMode)
	if err := claudeMgr.Restore(); err != nil {
		log.Fatalf("Restoring Claude chat sessions: %v", err)
	}

	var notifier codexbot.Notifier
	if cfg.Telegram.Token != "" {
		bt, err := codexbot.New(cfg.Telegram.Token, cfg.Telegram.AllowedUserID, sessionMgr, codexMgr)
		if err != nil {
			log.Fatalf("Creating bot: %v", err)
		}
		notifier = bt
	} else {
		notifier = codexbot.NopBot()
	}

	var httpSrv *httpapi.Server
	if cfg.API.Port > 0 {
		httpSrv = httpapi.NewServer(cfg.API, sessionMgr, claudeMgr, codexMgr)
		go httpSrv.Start()
	}

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigCh
		fmt.Println()
		fmt.Println(dim + "    Shutting down..." + r)
		if httpSrv != nil {
			httpSrv.Shutdown(context.Background())
		}
		claudeMgr.Shutdown()
		codexMgr.Shutdown()
		stopped := sessionMgr.StopAll()
		notifier.SendMessage(fmt.Sprintf("<b>RCOD + Codex bot stopped.</b>\nClosed Claude sessions: <code>%d</code>", stopped))
		notifier.Stop()
	}()

	printBanner()
	notifier.Start()
}
