import { useState } from "react";
import type { DraftAttachment, Session, CodexSession } from "../api/types";
import { SessionCard } from "./SessionCard";
import { CodexChatCard } from "./CodexChatCard";

interface Props {
  sessions: Session[];
  codexSessions: CodexSession[];
  logs: Record<string, string[]>;
  codexBusy: Record<string, boolean>;
  onKill: (id: string) => void;
  onRestart: (id: string) => void;
  onCodexSend: (id: string, message: string, attachments?: DraftAttachment[]) => void | Promise<void>;
  onCodexClose: (id: string) => void | Promise<void>;
}

export function SessionGrid({
  sessions,
  codexSessions,
  logs,
  codexBusy,
  onKill,
  onRestart,
  onCodexSend,
  onCodexClose,
}: Props) {
  const [expandedId, setExpandedId] = useState<string | null>(null);

  const toggle = (id: string) => {
    setExpandedId((prev) => (prev === id ? null : id));
  };

  if (sessions.length === 0 && codexSessions.length === 0) {
    return (
      <div className="flex items-center justify-center h-64 text-text-muted text-sm">
        No sessions yet. Create one to get started.
      </div>
    );
  }

  return (
    <div className="grid grid-cols-[repeat(auto-fill,minmax(380px,1fr))] gap-4">
      {sessions.map((s) => (
        <SessionCard
          key={s.id}
          session={s}
          logs={logs[s.id] || []}
          expanded={expandedId === s.id}
          onToggle={() => toggle(s.id)}
          onKill={onKill}
          onRestart={onRestart}
        />
      ))}
      {codexSessions.map((s) => (
        <CodexChatCard
          key={s.id}
          session={s}
          busy={codexBusy[s.id] || false}
          expanded={expandedId === s.id}
          onToggle={() => toggle(s.id)}
          onSend={onCodexSend}
          onClose={onCodexClose}
        />
      ))}
    </div>
  );
}
