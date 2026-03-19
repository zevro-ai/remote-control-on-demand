import type { ClaudeSession, CodexSession } from "../api/types";
import type { PanelState } from "../hooks/usePanelManager";
import { SessionPanel } from "./SessionPanel";
import { useSessions } from "../hooks/useSessions";
import {
  buildWallSlots,
  buildSessionPreview,
  getPreviewLineCount,
  resolveFocusedSession,
  sortSessionsForWall,
  type OverviewDensity,
} from "../lib/sessionWall";
import { SessionTile } from "./SessionTile";

interface Props {
  claudeSessions: ClaudeSession[];
  codexSessions: CodexSession[];
  focusedPanel: PanelState | null;
  density: OverviewDensity;
  onDensityChange: (density: OverviewDensity) => void;
  onFocusSession: (sessionId: string, type: "claude" | "codex") => void;
  onClearFocus: () => void;
}

const DENSITY_OPTIONS: OverviewDensity[] = ["compact", "comfortable", "focus"];

export function PanelLayout({
  claudeSessions,
  codexSessions,
  focusedPanel,
  density,
  onDensityChange,
  onFocusSession,
  onClearFocus,
}: Props) {
  const { state, actions } = useSessions();
  const sessions = sortSessionsForWall([...claudeSessions, ...codexSessions]);
  const slots = buildWallSlots(sessions, density);
  const focusedSession = resolveFocusedSession(
    focusedPanel,
    state.claudeSessions,
    state.codexSessions
  );

  const liveCount = sessions.filter((session) => session.busy).length;

  if (sessions.length === 0) {
    return (
      <div className="dashboard-empty">
        <div style={{ textAlign: "center" }}>
          <svg
            width="48"
            height="48"
            viewBox="0 0 24 24"
            fill="none"
            stroke="currentColor"
            strokeWidth="1.5"
            strokeLinecap="round"
            strokeLinejoin="round"
            style={{ color: "var(--color-text-muted)", margin: "0 auto 16px" }}
          >
            <rect x="3" y="3" width="18" height="18" rx="2" />
            <line x1="9" y1="3" x2="9" y2="21" />
            <line x1="15" y1="3" x2="15" y2="21" />
          </svg>
          <div className="boot-message">No sessions on the wall</div>
          <p>Create a Claude or Codex chat to populate the terminal grid.</p>
        </div>
      </div>
    );
  }

  if (focusedSession && focusedPanel) {
    return (
      <div className="focus-stage">
        <header className="focus-stage__toolbar">
          <button type="button" className="focus-stage__back" onClick={onClearFocus}>
            Back to wall
          </button>

          <div className="focus-stage__meta">
            <span className="focus-stage__label">Focused session</span>
            <strong>{focusedSession.rel_name}</strong>
            <span>
              {focusedSession.agent} · {focusedSession.messages?.length || 0} msgs ·{" "}
              {focusedSession.busy ? "streaming" : "idle"}
            </span>
          </div>
        </header>

        <div className="focus-stage__panel">
          {focusedPanel.type === "claude" && focusedSession.agent === "claude" ? (
            <SessionPanel
              type="claude"
              session={focusedSession}
              streamBlocks={state.streamBlocks[focusedSession.id] || []}
              onClose={onClearFocus}
              onSend={actions.sendClaudeMessage}
              onRunCommand={actions.sendClaudeCommand}
              onSessionClose={actions.closeClaudeSession}
            />
          ) : focusedPanel.type === "codex" && focusedSession.agent === "codex" ? (
            <SessionPanel
              type="codex"
              session={focusedSession}
              onClose={onClearFocus}
              onSend={actions.sendCodexMessage}
              onRunCommand={actions.sendCodexCommand}
              onSessionClose={actions.closeCodexSession}
            />
          ) : null}
        </div>
      </div>
    );
  }

  return (
    <section className="overview-shell overview-shell--full">
      <div className="tmux-shell">
        <header className="tmux-shell__header">
          <div className="tmux-shell__title-block">
            <div className="overview-toolbar__kicker">wall mode</div>
            <h2>Session wall</h2>
            <p>One operator canvas, split into tmux-like panes for every live repo session.</p>
          </div>

          <div className="tmux-shell__header-right">
            <div className="overview-stats">
              <div className="overview-stat">
                <span>Total</span>
                <strong>{sessions.length}</strong>
              </div>
              <div className="overview-stat">
                <span>Live</span>
                <strong>{liveCount}</strong>
              </div>
              <div className="overview-stat">
                <span>Ghost</span>
                <strong>{slots.filter((slot) => !slot.session).length}</strong>
              </div>
            </div>

            <div className="density-switch" role="tablist" aria-label="Grid density">
              {DENSITY_OPTIONS.map((option) => (
                <button
                  key={option}
                  type="button"
                  className={option === density ? "is-active" : ""}
                  onClick={() => onDensityChange(option)}
                >
                  {option}
                </button>
              ))}
            </div>
          </div>
        </header>

        <div className="tmux-shell__statusbar">
          <span>SESSION WALL :: {sessions.length} panes attached</span>
          <span>{liveCount} streaming</span>
          <span>{density} density</span>
          <span>click any pane to focus</span>
        </div>

        <div className="tmux-shell__body">
          <div className="session-wall" data-density={density}>
            {slots.map((slot) => (
              <SessionTile
                key={slot.paneLabel}
                paneLabel={slot.paneLabel}
                session={slot.session}
                density={density}
                focused={false}
                previewLines={
                  slot.session
                    ? buildSessionPreview(
                        slot.session,
                        slot.session.agent === "claude"
                          ? state.streamBlocks[slot.session.id] || []
                          : [],
                        getPreviewLineCount(density)
                      )
                    : []
                }
                onSelect={
                  slot.session
                    ? () => onFocusSession(slot.session!.id, slot.session!.agent)
                    : undefined
                }
              />
            ))}
          </div>
        </div>

        <footer className="tmux-shell__footer">
          <span>[mouse] focus pane</span>
          <span>[ghost pane] reserved slot</span>
          <span>status line mirrors tmux semantics, not dashboard cards</span>
        </footer>
      </div>
    </section>
  );
}
