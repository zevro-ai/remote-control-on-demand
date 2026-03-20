export type SessionStatus = "running" | "stopped" | "crashed";
export type AgentType = "claude" | "codex";
export type MessageKind = "text" | "bash" | "bash_result";

export interface MessageAttachment {
  id: string;
  name: string;
  content_type?: string;
  size?: number;
  url?: string;
  preview_url?: string;
}

export interface DraftAttachment {
  file: File;
  name: string;
  content_type: string;
  size: number;
  preview_url: string;
}

export interface CommandMeta {
  command: string;
  exit_code?: number;
  duration_ms?: number;
  timed_out?: boolean;
  truncated?: boolean;
}

export interface Session {
  id: string;
  folder: string;
  rel_name: string;
  status: SessionStatus;
  agent: AgentType;
  url?: string;
  pid?: number;
  started_at: string;
  restarts: number;
  uptime: string;
}

export interface Message {
  role: "user" | "assistant";
  kind?: MessageKind;
  content: string;
  timestamp: string;
  blocks?: StreamBlock[];
  attachments?: MessageAttachment[];
  command?: CommandMeta;
  optimistic?: boolean;
  optimistic_id?: string;
}

export interface ClaudeSession {
  id: string;
  folder: string;
  rel_name: string;
  agent: "claude";
  thread_id?: string;
  busy: boolean;
  created_at: string;
  updated_at: string;
  messages?: Message[];
}

export interface CodexSession {
  id: string;
  folder: string;
  rel_name: string;
  agent: "codex";
  thread_id?: string;
  busy: boolean;
  created_at: string;
  updated_at: string;
  messages?: Message[];
}

export type StreamBlock =
  | { type: "text"; content: string }
  | { type: "tool_use"; index: number; id: string; name: string; inputJSON: string; outputText?: string; done: boolean };

export interface WsToolCall {
  index: number;
  id?: string;
  name?: string;
  command?: string;
  output_text?: string;
  partial_json?: string;
}

export interface WsMessage {
  type: string;
  session_id?: string;
  line?: string;
  status?: string;
  restarts?: number;
  session?: Session | ClaudeSession | CodexSession;
  message?: Message;
  delta?: string;
  busy?: boolean;
  tool_call?: WsToolCall;
}
