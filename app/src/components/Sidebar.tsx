import type { ChatSession } from "../api/types";
import type { PanelState } from "../hooks/usePanelManager";

interface Props {
  chatSessions: Record<string, ChatSession[]>;
  connected: boolean;
  focusedPanel: PanelState | null;
  onNewSession: () => void;
  onSelectSession: (sessionId: string, type: string) => void;
}

export function Sidebar({
  chatSessions,
  connected,
  focusedPanel,
  onNewSession,
  onSelectSession,
}: Props) {
  const providers = Object.entries(chatSessions);
  const liveCount = providers.reduce(
    (acc, [_, sessions]) => acc + sessions.filter((s) => s.busy).length,
    0
  );
  const totalCount = providers.reduce((acc, [_, sessions]) => acc + sessions.length, 0);

  return (
    <aside className="tui-sidebar">
      <div className="sidebar-brand">
        <div className="sidebar-kicker">remote control on demand</div>
        <h1>Sessions</h1>
        <p>Manage AI sessions with live streaming.</p>
      </div>

      <div className="sidebar-stats">
        <div className="sidebar-stat">
          <span>Status</span>
          <strong className={connected ? "is-online" : "is-offline"}>
            {connected ? "Online" : "Offline"}
          </strong>
        </div>
        <div className="sidebar-stat">
          <span>Live</span>
          <strong>{liveCount}</strong>
        </div>
        <div className="sidebar-stat">
          <span>Total</span>
          <strong>{totalCount}</strong>
        </div>
      </div>

      {providers.map(([provider, sessions]) => (
        <SessionSection
          key={provider}
          title={provider.charAt(0).toUpperCase() + provider.slice(1)}
          sessions={sessions}
          type={provider}
          focusedPanel={focusedPanel}
          onSelectSession={onSelectSession}
        />
      ))}

      <button onClick={onNewSession} className="sidebar-new-button">
        + New session
      </button>
    </aside>
  );
}

function SessionSection({
  title,
  sessions,
  type,
  focusedPanel,
  onSelectSession,
}: {
  title: string;
  sessions: ChatSession[];
  type: string;
  focusedPanel: PanelState | null;
  onSelectSession: (sessionId: string, type: string) => void;
}) {
  return (
    <section className="sidebar-section">
      <div className="sidebar-section__header">
        <span>{title}</span>
        <span>{sessions.length}</span>
      </div>

      <div className="sidebar-section__list">
        {sessions.length === 0 ? (
          <div className="sidebar-empty">No active sessions</div>
        ) : (
          sessions.map((session) => {
            const isOpen =
              focusedPanel?.sessionId === session.id && focusedPanel.type === type;
            const messageCount = session.messages?.length || 0;
            return (
              <button
                key={session.id}
                onClick={() => onSelectSession(session.id, type)}
                className={`session-chip ${isOpen ? "is-open" : ""}`}
              >
                <div className="session-chip__title">
                  <span>{session.rel_name}</span>
                  {session.busy && <span className="session-chip__live">streaming</span>}
                </div>
                <div className="session-chip__meta">
                  <span>{messageCount} msgs</span>
                  <span>{session.id.slice(0, 8)}</span>
                </div>
              </button>
            );
          })
        )}
      </div>
    </section>
  );
}
