import type { CodexSession, DraftAttachment } from "../api/types";
import { StatusDot } from "./StatusDot";
import { AgentBadge } from "./AgentBadge";
import { MessageInput } from "./MessageInput";

interface Props {
  session: CodexSession;
  busy: boolean;
  expanded: boolean;
  onToggle: () => void;
  onSend: (id: string, message: string, attachments?: DraftAttachment[]) => void | Promise<void>;
  onRunCommand?: (id: string, command: string) => void | Promise<void>;
  onClose: (id: string) => void | Promise<void>;
}

export function CodexChatCard({
  session,
  busy,
  expanded,
  onToggle,
  onSend,
  onRunCommand,
  onClose,
}: Props) {
  const messages = session.messages || [];
  const visibleMessages = expanded ? messages : messages.slice(-2);

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
          <StatusDot status="running" />
          <span className="font-medium text-sm truncate">{session.rel_name}</span>
        </div>
        <div className="flex items-center gap-2 shrink-0">
          <AgentBadge agent="codex" />
          <span className="text-xs text-text-muted">
            {messages.length} msg{messages.length !== 1 ? "s" : ""}
          </span>
        </div>
      </div>

      {visibleMessages.length > 0 && (
        <div className="border-t border-border px-4 py-3 max-h-80 overflow-y-auto space-y-3">
          {visibleMessages.map((msg, i) => (
            <div
              key={i}
              className={`rounded-lg px-3 py-2 text-sm ${
                msg.role === "user"
                  ? "bg-accent-blue/10 text-text-primary ml-8"
                  : "bg-bg-hover text-text-secondary mr-8"
              }`}
            >
              <div className="text-xs text-text-muted mb-1 font-medium uppercase">
                {msg.role}
              </div>
              <div className="whitespace-pre-wrap break-words">{msg.content}</div>
            </div>
          ))}
        </div>
      )}

      {busy && (
        <div className="border-t border-border px-4 py-2">
          <span className="text-xs text-accent-orange animate-pulse">Thinking...</span>
        </div>
      )}

      {expanded && (
        <div className="border-t border-border p-4 space-y-3">
          <MessageInput
            onSendPrompt={(msg, attachments) => onSend(session.id, msg, attachments)}
            onSendCommand={onRunCommand ? (command) => onRunCommand(session.id, command) : undefined}
            disabled={busy}
            promptPlaceholder={busy ? "Codex is thinking..." : "Send a message to Codex..."}
            commandPlaceholder={busy ? "Command is running..." : "Run a bash command..."}
            supportsImages
            supportsBash={Boolean(onRunCommand)}
          />
          <button
            onClick={() => onClose(session.id)}
            className="rounded-md bg-accent-red/15 px-3 py-1.5 text-xs font-medium text-accent-red hover:bg-accent-red/25 transition-colors"
          >
            Close session
          </button>
        </div>
      )}
    </div>
  );
}
