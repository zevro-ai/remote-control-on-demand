export type SessionStatus = "running" | "stopped" | "crashed";
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

export interface ChatCapabilities {
  streaming_deltas: boolean;
  tool_call_streaming: boolean;
  image_attachments: boolean;
  shell_command_exec: boolean;
  thread_resume: boolean;
  adopt_existing_sessions: boolean;
  external_url_detection: boolean;
}

export interface RuntimeCapabilities {
  long_running_processes: boolean;
  auto_restart: boolean;
  external_url_detection: boolean;
}

export interface ProviderMetadata {
  id: string;
  display_name: string;
  chat?: ChatCapabilities;
  runtime?: RuntimeCapabilities;
}

export interface DeploymentInfo {
  version: string;
  commit?: string;
  build_id: string;
  started_at: string;
}

export interface AuthProvider {
  id: string;
  display_name: string;
}

export interface AuthUser {
  provider: string;
  subject: string;
  login?: string;
  name?: string;
  email?: string;
}

export interface AuthStatus {
  mode: "none" | "token" | "external";
  token_enabled: boolean;
  provider?: AuthProvider;
  authenticated: boolean;
  user?: AuthUser;
  login_url?: string;
  logout_url?: string;
}

export interface Session {
  id: string;
  folder: string;
  rel_name: string;
  status: SessionStatus;
  provider?: string;
  provider_meta?: ProviderMetadata;
  agent: string; // Provider ID (e.g., "claude", "codex")
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

export interface ChatSession {
  id: string;
  folder: string;
  rel_name: string;
  provider?: string;
  provider_meta?: ProviderMetadata;
  agent: string; // Provider ID
  thread_id?: string;
  busy: boolean;
  created_at: string;
  updated_at: string;
  messages?: Message[];
}

// Deprecated aliases for backward compatibility during refactoring if needed
export type ClaudeSession = ChatSession;
export type CodexSession = ChatSession;

export interface AdoptableSession {
  thread_id: string;
  rel_name: string;
  rel_cwd: string;
  title: string;
  model?: string;
  updated_at: string;
}

export type StreamBlock =
  | { type: "text"; content: string }
  | { type: "tool_use"; index: number; id: string; name: string; inputJSON: string; done: boolean };

export interface WsToolCall {
  index: number;
  id?: string;
  name?: string;
  partial_json?: string;
}

export interface WsMessage {
  type: string;
  provider?: string;
  provider_meta?: ProviderMetadata;
  session_id?: string;
  line?: string;
  status?: string;
  restarts?: number;
  session?: Session | ChatSession;
  message?: Message;
  delta?: string;
  busy?: boolean;
  tool_call?: WsToolCall;
}
