// Command rcod-agent exposes the local Codex and Antigravity managers over a
// JSON-lines stdio protocol. It is intended to be launched by RCOD through
// SSH, not exposed as a network service.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/zevro-ai/remote-control-on-demand/internal/antigravity"
	"github.com/zevro-ai/remote-control-on-demand/internal/codex"
	"github.com/zevro-ai/remote-control-on-demand/internal/remote"
)

func main() {
	baseFolder := flag.String("base-folder", "", "remote workspace containing git repositories")
	stateDir := flag.String("state-dir", ".rcod-agent", "directory for agent state")
	codexModel := flag.String("codex-model", "", "default Codex model")
	codexReasoning := flag.String("codex-reasoning", "", "default Codex reasoning effort")
	antigravityModel := flag.String("antigravity-model", "", "default Antigravity model")
	antigravityReasoning := flag.String("antigravity-reasoning", "", "default Antigravity effort")
	flag.Parse()

	if *baseFolder == "" {
		fatal("--base-folder is required")
	}
	if err := os.MkdirAll(*stateDir, 0700); err != nil {
		fatal("creating state directory: %v", err)
	}

	codexMgr := codex.NewManager(*baseFolder, filepath.Join(*stateDir, "codex_sessions.json"))
	codexMgr.SetModel(*codexModel)
	codexMgr.SetReasoningEffort(*codexReasoning)
	if err := codexMgr.Restore(); err != nil {
		fatal("restoring Codex sessions: %v", err)
	}

	antigravityMgr := antigravity.NewManager(*baseFolder, filepath.Join(*stateDir, "antigravity_sessions.json"))
	antigravityMgr.SetModel(*antigravityModel)
	antigravityMgr.SetReasoningEffort(*antigravityReasoning)
	if err := antigravityMgr.Restore(); err != nil {
		fatal("restoring Antigravity sessions: %v", err)
	}

	server := remote.AgentServer{Codex: codexMgr, Antigravity: antigravityMgr}
	if err := server.Serve(context.Background(), os.Stdin, os.Stdout); err != nil {
		fatal("serving agent protocol: %v", err)
	}
	codexMgr.Shutdown()
	antigravityMgr.Shutdown()
}

func fatal(format string, args ...any) {
	_, _ = fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
