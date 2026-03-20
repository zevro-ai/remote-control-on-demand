import { useEffect, useRef, useMemo } from "react";
import ReactMarkdown from "react-markdown";
import type { Message, StreamBlock } from "../api/types";
import { getAuthenticatedAssetURL } from "../api/client";
import { ToolCallBlock } from "./ToolCallBlock";

interface Props {
  messages: Message[];
  streamBlocks: StreamBlock[];
  busy: boolean;
}

export function partitionStreamBlocks(streamBlocks: StreamBlock[]) {
  const completedItems: (StreamBlock & { type: "tool_use" })[] = [];
  const activeBlocks: StreamBlock[] = [];

  for (const block of streamBlocks) {
    if (block.type === "tool_use" && block.done) {
      completedItems.push(block);
      continue;
    }
    activeBlocks.push(block);
  }

  return { completedItems, activeBlocks };
}

export function AgentActivityFeed({ messages, streamBlocks, busy }: Props) {
  const containerRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    const el = containerRef.current;
    if (el) el.scrollTop = el.scrollHeight;
  }, [messages.length, streamBlocks.length, streamBlocks]);

  const { completedItems, activeBlocks } = useMemo(() => partitionStreamBlocks(streamBlocks), [streamBlocks]);

  return (
    <div ref={containerRef} className="activity-feed">
      {messages.length === 0 && streamBlocks.length === 0 && (
        <div className="stream-placeholder">No messages yet. Send the first prompt.</div>
      )}

      {messages.map((msg, i) => (
        <div key={`msg-${i}`}>
          {msg.kind === "bash" && msg.command ? (
            <BashCommandRequest command={msg.command.command} timestamp={msg.timestamp} />
          ) : msg.kind === "bash_result" && msg.command ? (
            <BashCommandResult content={msg.content} command={msg.command} timestamp={msg.timestamp} />
          ) : msg.role === "user" ? (
            <UserPrompt
              content={msg.content}
              timestamp={msg.timestamp}
              attachments={msg.attachments}
            />
          ) : (
            <>
              {msg.blocks?.map((block, j) =>
                block.type === "tool_use" ? (
                  <ToolCallBlock
                    key={`msg-${i}-tool-${j}`}
                    name={block.name}
                    id={block.id}
                    inputJSON={block.inputJSON}
                    outputText={block.outputText}
                    done={block.done}
                  />
                ) : null
              )}
              <AssistantText content={msg.content} />
            </>
          )}
        </div>
      ))}

      {completedItems.length > 0 && (
        <div className="completed-items-summary">
          {completedItems.map((block) => (
            <CompactToolItem key={`done-${block.index}`} name={block.name} inputJSON={block.inputJSON} />
          ))}
        </div>
      )}

      {activeBlocks.map((block, index) =>
        block.type === "text" ? (
          <div key={`active-text-${index}`} className="text-stream-block">
            {block.content}
            {busy && index === activeBlocks.length - 1 && <span className="cursor-blink" />}
          </div>
        ) : (
          <ToolCallBlock
            key={`tool-${block.index}`}
            name={block.name}
            id={block.id}
            inputJSON={block.inputJSON}
            outputText={block.outputText}
            done={block.done}
            live
          />
        )
      )}

      {busy && activeBlocks.length === 0 && (
        <div className="thinking-indicator">
          <span className="tool-spinner" />
          <span style={{ color: "var(--color-text-muted)", fontSize: "0.82rem", marginLeft: "8px" }}>
            Thinking...
          </span>
        </div>
      )}
    </div>
  );
}

function CompactToolItem({ name, inputJSON }: { name: string; inputJSON: string }) {
  const target = useMemo(() => {
    if (!inputJSON) return "";
    try {
      const parsed = JSON.parse(inputJSON);
      return parsed.file_path || parsed.command || parsed.pattern || parsed.path || parsed.description || "";
    } catch {
      return inputJSON;
    }
  }, [inputJSON]);

  return (
    <div className="compact-tool-item">
      <span style={{ color: "var(--color-accent-green)" }}>&#10003;</span>
      <span className="compact-tool-item__name">{name}</span>
      {target && <span className="compact-tool-item__target">{target}</span>}
    </div>
  );
}

function UserPrompt({
  content,
  timestamp,
  attachments = [],
}: {
  content: string;
  timestamp: string;
  attachments?: Message["attachments"];
}) {
  return (
    <div className="user-prompt-block user-prompt-block--stacked">
      <div className="user-prompt-block__row">
        <span className="user-prompt-block__prefix">&gt;</span>
        <span>{content || "Attached image prompt"}</span>
        <span className="user-prompt-block__timestamp">{formatTime(timestamp)}</span>
      </div>
      {attachments.length > 0 && <MessageAttachments attachments={attachments} />}
    </div>
  );
}

function AssistantText({ content }: { content: string }) {
  return (
    <div className="text-stream-block markdown-body">
      <ReactMarkdown>{content}</ReactMarkdown>
    </div>
  );
}

function BashCommandRequest({ command, timestamp }: { command: string; timestamp: string }) {
  return (
    <div className="bash-command-request">
      <div className="bash-command-request__row">
        <span className="bash-command-request__prompt">$</span>
        <span className="bash-command-request__command">{command}</span>
        <span className="user-prompt-block__timestamp">{formatTime(timestamp)}</span>
      </div>
    </div>
  );
}

function BashCommandResult({
  content,
  command,
  timestamp,
}: {
  content: string;
  command: NonNullable<Message["command"]>;
  timestamp: string;
}) {
  const isError = (command.exit_code ?? 0) !== 0 || command.timed_out;

  return (
    <div className={`bash-result-block ${isError ? "is-error" : ""}`}>
      <div className="bash-result-block__header">
        <strong>{command.command}</strong>
        <span>
          exit {command.exit_code ?? 0} · {formatDuration(command.duration_ms)}
          {command.timed_out ? " · timed out" : ""}
          {command.truncated ? " · truncated" : ""}
          {timestamp ? ` · ${formatTime(timestamp)}` : ""}
        </span>
      </div>
      <pre>{content || "(no output)"}</pre>
    </div>
  );
}

function MessageAttachments({ attachments }: { attachments: NonNullable<Message["attachments"]> }) {
  return (
    <div className="message-attachments">
      {attachments.map((attachment) => {
        const src = attachment.preview_url || getAuthenticatedAssetURL(attachment.url);
        if (src) {
          return (
            <a
              key={attachment.id}
              href={src}
              target="_blank"
              rel="noreferrer"
              className="message-attachment-preview"
            >
              <img src={src} alt={attachment.name} />
              <span>{attachment.name}</span>
            </a>
          );
        }

        return (
          <div key={attachment.id} className="message-attachment-file">
            <strong>IMG</strong>
            <span>{attachment.name}</span>
          </div>
        );
      })}
    </div>
  );
}

function formatTime(value: string) {
  if (!value) return "";
  const date = new Date(value);
  return Number.isNaN(date.getTime())
    ? ""
    : date.toLocaleTimeString([], { hour: "2-digit", minute: "2-digit", second: "2-digit" });
}

function formatDuration(value?: number) {
  if (!value || value < 1000) {
    return `${value || 0} ms`;
  }
  return `${(value / 1000).toFixed(1)} s`;
}
