import { useMemo } from "react";
import type { ClaudeSession, CodexSession, DraftAttachment, StreamBlock } from "../api/types";
import { AgentBadge } from "./AgentBadge";
import { AgentActivityFeed } from "./AgentActivityFeed";
import { MessageInput } from "./MessageInput";

interface ClaudeProps {
  type: "claude";
  session: ClaudeSession;
  streamBlocks: StreamBlock[];
  onClose: () => void;
  onSend: (id: string, message: string, attachments?: DraftAttachment[]) => Promise<void>;
  onRunCommand: (id: string, command: string) => Promise<void>;
  onSessionClose: (id: string) => Promise<void>;
}

interface CodexProps {
  type: "codex";
  session: CodexSession;
  streamBlocks: StreamBlock[];
  onClose: () => void;
  onSend: (id: string, message: string, attachments?: DraftAttachment[]) => Promise<void>;
  onRunCommand: (id: string, command: string) => Promise<void>;
  onSessionClose: (id: string) => Promise<void>;
  onCancel?: (id: string) => Promise<void>;
}

type Props = ClaudeProps | CodexProps;

export function SessionPanel(props: Props) {
  const { session, type } = props;
  const messages = session.messages || [];
  const streamBlocks = props.streamBlocks;

  const telemetry = useMemo(
    () => [
      { label: "State", value: session.busy ? "Streaming" : "Idle" },
      { label: "Messages", value: String(messages.length) },
      {
        label: "Updated",
        value: formatClock((session as ClaudeSession | CodexSession).updated_at),
      },
      {
        label: "Thread",
        value: shorten((session as ClaudeSession | CodexSession).thread_id || session.id),
      },
    ],
    [messages.length, session]
  );

  return (
    <article className="session-panel">
      <header className="session-panel__header">
        <div>
          <div className="session-panel__kicker">{type === "claude" ? "Anthropic" : "OpenAI"}</div>
          <h2>{session.rel_name}</h2>
        </div>

        <div className="session-panel__header-actions">
          <div className={`live-pill ${session.busy ? "is-live" : ""}`}>
            {session.busy ? "Streaming" : "Standby"}
          </div>
          <AgentBadge agent={type} />
          <button onClick={props.onClose} className="panel-icon-button" title="Close panel">
            &times;
          </button>
        </div>
      </header>

      <div className="session-panel__body">
        <section className="session-window session-window--conversation">
          <div className="session-window__title">
            {type === "claude" ? "Agent Activity" : "Conversation"}
          </div>
          <AgentActivityFeed
            messages={messages}
            streamBlocks={streamBlocks}
            busy={session.busy}
          />
        </section>

        <aside className="session-window session-window--telemetry">
          <div className="session-window__title">Telemetry</div>
          <div className="telemetry-grid">
            {telemetry.map((item) => (
              <div key={item.label} className="telemetry-cell">
                <span>{item.label}</span>
                <strong>{item.value}</strong>
              </div>
            ))}
          </div>

          <div className="wiretap">
            <div className="wiretap__header">Wire Tap</div>
            <div className="wiretap__body">
              {messages.slice(-4).map((message, index) => (
                <div key={`${message.timestamp}-${index}`} className="wiretap__line">
                  <span>{formatClock(message.timestamp)}</span>
                  <strong>{message.role}</strong>
                  <span>{trimMessage(message.content)}</span>
                </div>
              ))}
            </div>
          </div>
        </aside>
      </div>

      <footer className="session-panel__footer">
        <MessageInput
          onSendPrompt={(message, attachments) => props.onSend(session.id, message, attachments)}
          onSendCommand={(command) => props.onRunCommand(session.id, command)}
          disabled={session.busy}
          promptPlaceholder={
            session.busy
              ? `${type} is responding...`
              : `Send a message to ${type === "claude" ? "Claude" : "Codex"}...`
          }
          commandPlaceholder={
            session.busy
              ? "Command is running..."
              : `Run a bash command in ${session.rel_name}...`
          }
          supportsImages={type === "codex"}
          supportsBash
        />
        <div className="session-panel__footer-actions">
          {props.type === "codex" && session.busy && props.onCancel && (
            <button
              onClick={() => props.onCancel!(session.id)}
              className="session-stop-button"
            >
              Stop
            </button>
          )}
          <button
            onClick={() => props.onSessionClose(session.id)}
            className="session-close-button"
          >
            End session
          </button>
        </div>
      </footer>
    </article>
  );
}

function trimMessage(content: string) {
  return content.replace(/\s+/g, " ").trim().slice(0, 84) || "...";
}

function shorten(value: string) {
  return value.length > 14 ? `${value.slice(0, 6)}...${value.slice(-4)}` : value;
}

function formatClock(value: string) {
  if (!value) return "--:--:--";
  const date = new Date(value);
  return Number.isNaN(date.getTime())
    ? "--:--:--"
    : date.toLocaleTimeString([], { hour: "2-digit", minute: "2-digit", second: "2-digit" });
}
