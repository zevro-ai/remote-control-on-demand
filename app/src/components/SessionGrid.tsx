import { useState } from "react";
import type { DraftAttachment, Session, ChatSession } from "../api/types";
import { SessionCard } from "./SessionCard";
import { ChatCard } from "./ChatCard";

interface Props {
  sessions: Session[];
  chatSessions: ChatSession[];
  logs: Record<string, string[]>;
  chatBusy: Record<string, boolean>;
  onKill: (id: string) => void;
  onRestart: (id: string) => void;
  onChatSend: (id: string, message: string, attachments?: DraftAttachment[]) => void | Promise<void>;
  onChatClose: (id: string) => void | Promise<void>;
}

export function SessionGrid({
  sessions,
  chatSessions,
  logs,
  chatBusy,
  onKill,
  onRestart,
  onChatSend,
  onChatClose,
}: Props) {
  const [expandedId, setExpandedId] = useState<string | null>(null);

  const toggle = (id: string) => {
    setExpandedId((prev) => (prev === id ? null : id));
  };

  if (sessions.length === 0 && chatSessions.length === 0) {
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
      {chatSessions.map((s) => (
        <ChatCard
          key={`${s.agent}:${s.id}`}
          session={s}
          busy={chatBusy[s.id] || false}
          expanded={expandedId === `${s.agent}:${s.id}`}
          onToggle={() => toggle(`${s.agent}:${s.id}`)}
          onSend={onChatSend}
          onClose={onChatClose}
        />
      ))}
    </div>
  );
}
