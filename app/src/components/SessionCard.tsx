import type { Session } from "../api/types";
import { StatusDot } from "./StatusDot";
import { AgentBadge } from "./AgentBadge";
import { LogLine } from "./LogLine";
import { SessionActions } from "./SessionActions";

interface Props {
  session: Session;
  logs: string[];
  expanded: boolean;
  onToggle: () => void;
  onKill: (id: string) => void;
  onRestart: (id: string) => void;
}

export function SessionCard({
  session,
  logs,
  expanded,
  onToggle,
  onKill,
  onRestart,
}: Props) {
  const visibleLogs = expanded ? logs.slice(-20) : logs.slice(-5);

  return (
    <div
      className={`rounded-lg border border-border bg-bg-card overflow-hidden transition-all ${
        expanded ? "col-span-full" : ""
      }`}
    >
      <div
        className="flex items-center justify-between gap-3 p-4 cursor-pointer hover:bg-bg-hover transition-colors"
        onClick={onToggle}
      >
        <div className="flex items-center gap-3 min-w-0">
          <StatusDot status={session.status} />
          <span className="font-medium text-sm truncate">{session.rel_name}</span>
        </div>
        <div className="flex items-center gap-2 shrink-0">
          <AgentBadge agent="claude" />
          <span className="text-xs text-text-muted">{session.uptime}</span>
        </div>
      </div>

      {visibleLogs.length > 0 && (
        <div className="border-t border-border px-4 py-2 max-h-64 overflow-y-auto bg-bg-primary/50">
          {visibleLogs.map((line, i) => (
            <LogLine key={i} line={line} />
          ))}
        </div>
      )}

      {session.status === "running" && !expanded && (
        <div className="border-t border-border px-4 py-2">
          <span className="text-xs text-accent-green animate-pulse">Working...</span>
        </div>
      )}

      {expanded && (
        <div className="border-t border-border p-4">
          <SessionActions session={session} onKill={onKill} onRestart={onRestart} />
        </div>
      )}
    </div>
  );
}
