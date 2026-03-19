import type { ChatSession, OverviewDensity, PreviewLine } from "../lib/sessionWall";

interface Props {
  paneLabel: string;
  density: OverviewDensity;
  focused: boolean;
  session?: ChatSession | null;
  previewLines?: PreviewLine[];
  onSelect?: () => void;
}

export function SessionTile({
  paneLabel,
  density,
  focused,
  session,
  previewLines = [],
  onSelect,
}: Props) {
  if (!session) {
    return (
      <div className="session-tile session-tile--ghost" data-density={density}>
        <div className="session-tile__head">
          <div className="session-tile__pane-meta">
            <span className="session-tile__pane-label">[{paneLabel}]</span>
            <span className="session-tile__agent-tag is-ghost">empty pane</span>
          </div>
        </div>

        <div className="session-tile__terminal session-tile__terminal--ghost">
          <div className="session-tile__line is-system">waiting for session attach</div>
          <div className="session-tile__line is-system">click + New session to populate this slot</div>
          <div className="session-tile__line is-system">grid keeps ghost panes so the wall feels stable</div>
        </div>

        <div className="session-tile__foot">
          <span>idle slot</span>
          <span>available</span>
        </div>
      </div>
    );
  }

  const messageCount = session.messages?.length || 0;
  const threadRef = session.thread_id || session.id;

  return (
    <button
      type="button"
      className={`session-tile ${focused ? "is-focused" : ""} ${session.busy ? "is-live" : ""}`}
      data-density={density}
      onClick={onSelect}
    >
      <div className="session-tile__head">
        <div className="session-tile__title-block">
          <div className="session-tile__pane-meta">
            <span className="session-tile__pane-label">[{paneLabel}]</span>
            <span
              className={`session-tile__agent-tag ${session.busy ? "is-live" : ""}`}
            >
              {session.agent}
            </span>
          </div>
          <div className="session-tile__title-row">
            <span className="session-tile__repo">{session.rel_name}</span>
            <span className={`session-tile__state ${session.busy ? "is-live" : ""}`}>
              {session.busy ? "streaming" : "idle"}
            </span>
          </div>
          <div className="session-tile__meta">
            <span>{formatClock(session.updated_at)}</span>
            <span>{messageCount} msgs</span>
            <span>{shorten(threadRef)}</span>
          </div>
        </div>
      </div>

      <div className="session-tile__terminal">
        {previewLines.map((line, index) => (
          <div
            key={`${session.id}-${index}-${line.text}`}
            className={`session-tile__line is-${line.tone}`}
          >
            {line.text}
          </div>
        ))}
      </div>

      <div className="session-tile__foot">
        <span>{session.busy ? "attached to live output" : "ready for focus"}</span>
        <span>open pane</span>
      </div>
    </button>
  );
}

function shorten(value: string) {
  return value.length > 18 ? `${value.slice(0, 6)}...${value.slice(-4)}` : value;
}

function formatClock(value: string) {
  if (!value) return "--:--";
  const parsed = new Date(value);
  return Number.isNaN(parsed.getTime())
    ? "--:--"
    : parsed.toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" });
}
