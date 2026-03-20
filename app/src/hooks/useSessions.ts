import {
  createContext,
  useContext,
  useReducer,
  useEffect,
  useRef,
  type Dispatch,
} from "react";
import type {
  Session,
  ClaudeSession,
  CodexSession,
  DraftAttachment,
  Message,
  MessageAttachment,
  StreamBlock,
  WsMessage,
} from "../api/types";
import { api } from "../api/client";
import { useWs } from "./WebSocketContext";
import { mergeIncomingMessage, removeOptimisticMessage } from "../lib/realtimeMessages";
import { isUnauthorizedError } from "../lib/requestErrors";

interface State {
  sessions: Session[];
  claudeSessions: ClaudeSession[];
  codexSessions: CodexSession[];
  logs: Record<string, string[]>;
  streamBlocks: Record<string, StreamBlock[]>;
  loading: boolean;
  authRequired: boolean;
  loadError: string | null;
}

interface BootstrapData {
  sessions: Session[];
  claudeSessions: ClaudeSession[];
  codexSessions: CodexSession[];
  authRequired: boolean;
  loadError: string | null;
}

type Action =
  | { type: "SET_SESSIONS"; sessions: Session[] }
  | { type: "SET_CLAUDE_SESSIONS"; sessions: ClaudeSession[] }
  | { type: "SET_CODEX_SESSIONS"; sessions: CodexSession[] }
  | { type: "UPDATE_SESSION"; session: Session }
  | { type: "ADD_LOG"; sessionId: string; line: string }
  | { type: "UPDATE_STATUS"; sessionId: string; status: string; restarts: number }
  | { type: "ADD_SESSION"; session: Session }
  | { type: "REMOVE_SESSION"; sessionId: string }
  | { type: "ADD_CLAUDE_SESSION"; session: ClaudeSession }
  | { type: "REMOVE_CLAUDE_SESSION"; sessionId: string }
  | { type: "ADD_CODEX_SESSION"; session: CodexSession }
  | { type: "REMOVE_CODEX_SESSION"; sessionId: string }
  | { type: "ADD_CLAUDE_MESSAGE"; sessionId: string; message: Message }
  | { type: "ADD_CODEX_MESSAGE"; sessionId: string; message: Message }
  | { type: "REMOVE_CLAUDE_OPTIMISTIC_MESSAGE"; sessionId: string; optimisticId: string }
  | { type: "REMOVE_CODEX_OPTIMISTIC_MESSAGE"; sessionId: string; optimisticId: string }
  | { type: "APPEND_STREAMING"; sessionId: string; delta: string }
  | { type: "CLEAR_STREAMING"; sessionId: string }
  | { type: "SET_CLAUDE_BUSY"; sessionId: string; busy: boolean }
  | { type: "SET_CODEX_BUSY"; sessionId: string; busy: boolean }
  | { type: "CODEX_ITEM_STARTED"; sessionId: string; index: number; id: string; name: string; command: string }
  | { type: "CODEX_ITEM_COMPLETED"; sessionId: string; index: number; text: string }
  | { type: "CODEX_ERROR"; sessionId: string; error: string }
  | { type: "SET_LOADING"; loading: boolean }
  | { type: "SET_AUTH_REQUIRED"; authRequired: boolean }
  | { type: "SET_LOAD_ERROR"; error: string | null }
  | { type: "RECONCILE_ON_RECONNECT" }
  | { type: "TOOL_START"; sessionId: string; index: number; id: string; name: string }
  | { type: "TOOL_DELTA"; sessionId: string; index: number; partialJSON: string }
  | { type: "TOOL_FINISH"; sessionId: string; index: number };

const MAX_LOG_LINES = 100;

export function reduceSessionsState(state: State, action: Action): State {
  switch (action.type) {
    case "SET_SESSIONS":
      return { ...state, sessions: action.sessions };
    case "SET_CLAUDE_SESSIONS":
      return { ...state, claudeSessions: action.sessions };
    case "SET_CODEX_SESSIONS":
      return { ...state, codexSessions: action.sessions };
    case "UPDATE_SESSION":
      return {
        ...state,
        sessions: state.sessions.map((s) => (s.id === action.session.id ? action.session : s)),
      };
    case "ADD_LOG": {
      const lines = [...(state.logs[action.sessionId] || []), action.line];
      if (lines.length > MAX_LOG_LINES) lines.splice(0, lines.length - MAX_LOG_LINES);
      return { ...state, logs: { ...state.logs, [action.sessionId]: lines } };
    }
    case "UPDATE_STATUS":
      return {
        ...state,
        sessions: state.sessions.map((s) =>
          s.id === action.sessionId
            ? { ...s, status: action.status as Session["status"], restarts: action.restarts }
            : s
        ),
      };
    case "ADD_SESSION":
      if (state.sessions.some((s) => s.id === action.session.id)) return state;
      return { ...state, sessions: [...state.sessions, action.session] };
    case "REMOVE_SESSION":
      return { ...state, sessions: state.sessions.filter((s) => s.id !== action.sessionId) };
    case "ADD_CLAUDE_SESSION":
      if (state.claudeSessions.some((s) => s.id === action.session.id)) return state;
      return { ...state, claudeSessions: [action.session, ...state.claudeSessions] };
    case "REMOVE_CLAUDE_SESSION":
      return {
        ...state,
        claudeSessions: state.claudeSessions.filter((s) => s.id !== action.sessionId),
        streamBlocks: omitKey(state.streamBlocks, action.sessionId),
      };
    case "ADD_CODEX_SESSION":
      if (state.codexSessions.some((s) => s.id === action.session.id)) return state;
      return { ...state, codexSessions: [action.session, ...state.codexSessions] };
    case "REMOVE_CODEX_SESSION":
      return {
        ...state,
        codexSessions: state.codexSessions.filter((s) => s.id !== action.sessionId),
        streamBlocks: omitKey(state.streamBlocks, action.sessionId),
      };
    case "ADD_CLAUDE_MESSAGE": {
      const currentBlocks = state.streamBlocks[action.sessionId] || [];
      const enrichedMessage =
        action.message.role === "assistant" && currentBlocks.length > 0
          ? { ...action.message, blocks: [...currentBlocks] }
          : action.message;
      return {
        ...state,
        claudeSessions: state.claudeSessions.map((s) =>
          s.id === action.sessionId
            ? {
                ...s,
                busy: enrichedMessage.role === "assistant" ? false : s.busy,
                updated_at: enrichedMessage.timestamp,
                messages: action.message.optimistic
                  ? [...(s.messages || []), enrichedMessage]
                  : mergeIncomingMessage(s.messages || [], enrichedMessage),
              }
            : s
        ),
        streamBlocks: omitKey(state.streamBlocks, action.sessionId),
      };
    }
    case "ADD_CODEX_MESSAGE": {
      const currentBlocks = state.streamBlocks[action.sessionId] || [];
      const enrichedMessage =
        action.message.role === "assistant" && currentBlocks.length > 0
          ? { ...action.message, blocks: [...currentBlocks] }
          : action.message;
      return {
        ...state,
        codexSessions: state.codexSessions.map((s) =>
          s.id === action.sessionId
            ? {
                ...s,
                busy: enrichedMessage.role === "assistant" ? false : s.busy,
                updated_at: enrichedMessage.timestamp,
                messages: action.message.optimistic
                  ? [...(s.messages || []), enrichedMessage]
                  : mergeIncomingMessage(s.messages || [], enrichedMessage),
              }
            : s
        ),
        streamBlocks:
          action.message.role === "assistant"
            ? omitKey(state.streamBlocks, action.sessionId)
            : state.streamBlocks,
      };
    }
    case "REMOVE_CLAUDE_OPTIMISTIC_MESSAGE":
      return {
        ...state,
        claudeSessions: state.claudeSessions.map((s) =>
          s.id === action.sessionId
            ? {
                ...s,
                messages: removeOptimisticMessage(s.messages || [], action.optimisticId),
              }
            : s
        ),
      };
    case "REMOVE_CODEX_OPTIMISTIC_MESSAGE":
      return {
        ...state,
        codexSessions: state.codexSessions.map((s) =>
          s.id === action.sessionId
            ? {
                ...s,
                messages: removeOptimisticMessage(s.messages || [], action.optimisticId),
              }
            : s
        ),
      };
    case "APPEND_STREAMING": {
      const blocks = [...(state.streamBlocks[action.sessionId] || [])];
      const last = blocks[blocks.length - 1];
      if (last && last.type === "text") {
        blocks[blocks.length - 1] = { ...last, content: last.content + action.delta };
      } else {
        blocks.push({ type: "text", content: action.delta });
      }
      return { ...state, streamBlocks: { ...state.streamBlocks, [action.sessionId]: blocks } };
    }
    case "CLEAR_STREAMING":
      return { ...state, streamBlocks: omitKey(state.streamBlocks, action.sessionId) };
    case "SET_CLAUDE_BUSY":
      return {
        ...state,
        claudeSessions: state.claudeSessions.map((s) =>
          s.id === action.sessionId ? { ...s, busy: action.busy } : s
        ),
        streamBlocks: action.busy ? state.streamBlocks : omitKey(state.streamBlocks, action.sessionId),
      };
    case "SET_CODEX_BUSY":
      return {
        ...state,
        codexSessions: state.codexSessions.map((s) =>
          s.id === action.sessionId ? { ...s, busy: action.busy } : s
        ),
        streamBlocks: action.busy ? state.streamBlocks : omitKey(state.streamBlocks, action.sessionId),
      };
    case "CODEX_ERROR": {
      const errorMessage: Message = {
        role: "assistant",
        kind: "text",
        content: `Error: ${action.error}`,
        timestamp: new Date().toISOString(),
      };
      return {
        ...state,
        codexSessions: state.codexSessions.map((s) =>
          s.id === action.sessionId
            ? {
                ...s,
                busy: false,
                messages: [...(s.messages || []), errorMessage],
              }
            : s
        ),
        streamBlocks: omitKey(state.streamBlocks, action.sessionId),
      };
    }
    case "CODEX_ITEM_STARTED": {
      const blocks = [...(state.streamBlocks[action.sessionId] || [])];
      blocks.push({
        type: "tool_use",
        index: action.index,
        id: action.id,
        name: action.name,
        inputJSON: action.command,
        done: false,
      });
      return { ...state, streamBlocks: { ...state.streamBlocks, [action.sessionId]: blocks } };
    }
    case "CODEX_ITEM_COMPLETED": {
      const blocks = (state.streamBlocks[action.sessionId] || []).map((b) =>
        b.type === "tool_use" && b.index === action.index
          ? { ...b, done: true, inputJSON: action.text || b.inputJSON }
          : b
      );
      return { ...state, streamBlocks: { ...state.streamBlocks, [action.sessionId]: blocks } };
    }
    case "SET_LOADING":
      return { ...state, loading: action.loading };
    case "SET_AUTH_REQUIRED":
      return { ...state, authRequired: action.authRequired };
    case "SET_LOAD_ERROR":
      return { ...state, loadError: action.error };
    case "RECONCILE_ON_RECONNECT":
      return { ...state, streamBlocks: {} };
    case "TOOL_START": {
      const blocks = [...(state.streamBlocks[action.sessionId] || [])];
      blocks.push({
        type: "tool_use",
        index: action.index,
        id: action.id,
        name: action.name,
        inputJSON: "",
        done: false,
      });
      return { ...state, streamBlocks: { ...state.streamBlocks, [action.sessionId]: blocks } };
    }
    case "TOOL_DELTA": {
      const blocks = (state.streamBlocks[action.sessionId] || []).map((b) =>
        b.type === "tool_use" && b.index === action.index
          ? { ...b, inputJSON: b.inputJSON + action.partialJSON }
          : b
      );
      return { ...state, streamBlocks: { ...state.streamBlocks, [action.sessionId]: blocks } };
    }
    case "TOOL_FINISH": {
      const blocks = (state.streamBlocks[action.sessionId] || []).map((b) =>
        b.type === "tool_use" && b.index === action.index ? { ...b, done: true } : b
      );
      return { ...state, streamBlocks: { ...state.streamBlocks, [action.sessionId]: blocks } };
    }
    default:
      return state;
  }
}

function omitKey<T>(input: Record<string, T>, key: string) {
  const next = { ...input };
  delete next[key];
  return next;
}

export const sessionsInitialState: State = {
  sessions: [],
  claudeSessions: [],
  codexSessions: [],
  logs: {},
  streamBlocks: {},
  loading: true,
  authRequired: false,
  loadError: null,
};

type BootstrapSettledResults = [
  PromiseSettledResult<Session[]>,
  PromiseSettledResult<ClaudeSession[]>,
  PromiseSettledResult<CodexSession[]>,
];

function toBootstrapErrorMessage(reason: unknown) {
  if (reason instanceof Error && reason.message.trim() !== "") {
    return reason.message;
  }

  return "unknown error";
}

export function resolveBootstrapResults(results: BootstrapSettledResults): BootstrapData {
  const [sessionsResult, claudeResult, codexResult] = results;
  const anySuccess = results.some((result) => result.status === "fulfilled");
  const partialFailures = [
    { label: "RC", result: sessionsResult },
    { label: "Claude", result: claudeResult },
    { label: "Codex", result: codexResult },
  ]
    .filter(
      (
        entry
      ): entry is {
        label: string;
        result: PromiseRejectedResult;
      } => entry.result.status === "rejected"
    )
    .map(({ label, result }) => `${label}: ${toBootstrapErrorMessage(result.reason)}`);

  const loadError =
    partialFailures.length > 0
      ? `Some services failed to load: ${partialFailures.join("; ")}`
      : null;

  if (anySuccess) {
    return {
      sessions: sessionsResult.status === "fulfilled" ? sessionsResult.value : [],
      claudeSessions: claudeResult.status === "fulfilled" ? claudeResult.value : [],
      codexSessions: codexResult.status === "fulfilled" ? codexResult.value : [],
      authRequired: false,
      loadError,
    };
  }

  const firstError = results.find((result) => result.status === "rejected");
  if (firstError?.status === "rejected" && isUnauthorizedError(firstError.reason)) {
    return {
      sessions: [],
      claudeSessions: [],
      codexSessions: [],
      authRequired: true,
      loadError: null,
    };
  }

  return {
    sessions: [],
    claudeSessions: [],
    codexSessions: [],
    authRequired: false,
    loadError: loadError || "Failed to load dashboard data.",
  };
}

type Actions = {
  startSession: (folder: string) => Promise<Session>;
  killSession: (id: string) => Promise<void>;
  restartSession: (id: string) => Promise<void>;
  createClaudeSession: (folder: string) => Promise<ClaudeSession>;
  closeClaudeSession: (id: string) => Promise<void>;
  sendClaudeMessage: (id: string, message: string, attachments?: DraftAttachment[]) => Promise<void>;
  sendClaudeCommand: (id: string, command: string) => Promise<void>;
  createCodexSession: (folder: string) => Promise<CodexSession>;
  closeCodexSession: (id: string) => Promise<void>;
  cancelCodexSession: (id: string) => Promise<void>;
  sendCodexMessage: (id: string, message: string, attachments?: DraftAttachment[]) => Promise<void>;
  sendCodexCommand: (id: string, command: string) => Promise<void>;
};

export const SessionsContext = createContext<{
  state: State;
  dispatch: Dispatch<Action>;
  actions: Actions;
}>({
  state: sessionsInitialState,
  dispatch: () => {},
  actions: {
    startSession: async () => Promise.reject(new Error("not ready")),
    killSession: async () => {},
    restartSession: async () => {},
    createClaudeSession: async () => Promise.reject(new Error("not ready")),
    closeClaudeSession: async () => {},
    sendClaudeMessage: async () => {},
    sendClaudeCommand: async () => {},
    createCodexSession: async () => Promise.reject(new Error("not ready")),
    closeCodexSession: async () => {},
    cancelCodexSession: async () => {},
    sendCodexMessage: async () => {},
    sendCodexCommand: async () => {},
  },
});

function toOptimisticAttachments(attachments: DraftAttachment[] = []): MessageAttachment[] {
  return attachments.map((attachment, index) => ({
    id: `pending-${Date.now()}-${index}`,
    name: attachment.name,
    content_type: attachment.content_type,
    size: attachment.size,
  }));
}

function createOptimisticMessageId() {
  return `optimistic-${Date.now()}-${Math.random().toString(36).slice(2, 8)}`;
}

async function sendPrompt(
  path: string,
  message: string,
  attachments: DraftAttachment[] = []
) {
  if (attachments.length === 0) {
    await api.post(path, { message });
    return;
  }

  const form = new FormData();
  form.append("message", message);
  attachments.forEach((attachment) => form.append("images", attachment.file, attachment.name));
  await api.postForm(path, form);
}

async function runCommand(path: string, command: string) {
  await api.post(path, { command });
}

async function fetchBootstrapData() {
  const results = (await Promise.allSettled([
    api.get<Session[]>("/api/sessions"),
    api.get<ClaudeSession[]>("/api/claude/sessions"),
    api.get<CodexSession[]>("/api/codex/sessions"),
  ])) as BootstrapSettledResults;

  return resolveBootstrapResults(results);
}

export function useSessionsReducer() {
  const [state, dispatch] = useReducer(reduceSessionsState, sessionsInitialState);
  const { onMessage, subscribe, unsubscribe } = useWs();
  const subscribedSessionIdsRef = useRef(new Set<string>());

  const applyBootstrapData = (data: BootstrapData) => {
    dispatch({ type: "SET_SESSIONS", sessions: data.sessions });
    dispatch({ type: "SET_CLAUDE_SESSIONS", sessions: data.claudeSessions });
    dispatch({ type: "SET_CODEX_SESSIONS", sessions: data.codexSessions });
    dispatch({ type: "SET_AUTH_REQUIRED", authRequired: data.authRequired });
    dispatch({ type: "SET_LOAD_ERROR", error: data.loadError });
  };

  useEffect(() => {
    fetchBootstrapData()
      .then(applyBootstrapData)
      .finally(() => dispatch({ type: "SET_LOADING", loading: false }));
  }, []);

  useEffect(() => {
    const nextSessionIds = new Set(state.sessions.map((session) => session.id));

    for (const sessionId of nextSessionIds) {
      if (!subscribedSessionIdsRef.current.has(sessionId)) {
        subscribe(sessionId);
      }
    }

    for (const sessionId of subscribedSessionIdsRef.current) {
      if (!nextSessionIds.has(sessionId)) {
        unsubscribe(sessionId);
      }
    }

    subscribedSessionIdsRef.current = nextSessionIds;
  }, [state.sessions, subscribe, unsubscribe]);

  useEffect(
    () => () => {
      for (const sessionId of subscribedSessionIdsRef.current) {
        unsubscribe(sessionId);
      }
      subscribedSessionIdsRef.current.clear();
    },
    [unsubscribe]
  );

  useEffect(() => {
    const refetch = () => fetchBootstrapData().then(applyBootstrapData);

    const unsubConnected = onMessage("connected", () => {
      dispatch({ type: "RECONCILE_ON_RECONNECT" });
      void refetch();
    });

    const unsubLog = onMessage("log", (msg: WsMessage) => {
      if (msg.session_id && msg.line) {
        dispatch({ type: "ADD_LOG", sessionId: msg.session_id, line: msg.line });
      }
    });

    const unsubStatus = onMessage("status", (msg: WsMessage) => {
      if (msg.session_id) {
        dispatch({
          type: "UPDATE_STATUS",
          sessionId: msg.session_id,
          status: msg.status || "unknown",
          restarts: msg.restarts || 0,
        });
      }
    });

    const unsubSessionAdded = onMessage("session_added", (msg: WsMessage) => {
      if (msg.session) {
        dispatch({ type: "ADD_SESSION", session: msg.session as Session });
      }
    });

    const unsubSessionRemoved = onMessage("session_removed", (msg: WsMessage) => {
      if (msg.session_id) {
        dispatch({ type: "REMOVE_SESSION", sessionId: msg.session_id });
      }
    });

    const unsubClaudeAdded = onMessage("claude_session_added", (msg: WsMessage) => {
      if (msg.session) {
        dispatch({ type: "ADD_CLAUDE_SESSION", session: msg.session as ClaudeSession });
      }
    });

    const unsubClaudeRemoved = onMessage("claude_session_removed", (msg: WsMessage) => {
      if (msg.session_id) {
        dispatch({ type: "REMOVE_CLAUDE_SESSION", sessionId: msg.session_id });
      }
    });

    const unsubClaudeDelta = onMessage("claude_message_delta", (msg: WsMessage) => {
      if (msg.session_id && msg.delta) {
        dispatch({ type: "APPEND_STREAMING", sessionId: msg.session_id, delta: msg.delta });
      }
    });

    const unsubClaudeMsg = onMessage("claude_message", (msg: WsMessage) => {
      if (msg.session_id && msg.message) {
        dispatch({ type: "ADD_CLAUDE_MESSAGE", sessionId: msg.session_id, message: msg.message });
      }
    });

    const unsubClaudeBusy = onMessage("claude_busy", (msg: WsMessage) => {
      if (msg.session_id && msg.busy !== undefined) {
        dispatch({ type: "SET_CLAUDE_BUSY", sessionId: msg.session_id, busy: msg.busy });
      }
    });

    const unsubToolStart = onMessage("claude_tool_start", (msg: WsMessage) => {
      if (msg.session_id && msg.tool_call) {
        dispatch({
          type: "TOOL_START",
          sessionId: msg.session_id,
          index: msg.tool_call.index,
          id: msg.tool_call.id || "",
          name: msg.tool_call.name || "",
        });
      }
    });

    const unsubToolDelta = onMessage("claude_tool_delta", (msg: WsMessage) => {
      if (msg.session_id && msg.tool_call) {
        dispatch({
          type: "TOOL_DELTA",
          sessionId: msg.session_id,
          index: msg.tool_call.index,
          partialJSON: msg.tool_call.partial_json || "",
        });
      }
    });

    const unsubToolFinish = onMessage("claude_tool_finish", (msg: WsMessage) => {
      if (msg.session_id && msg.tool_call) {
        dispatch({
          type: "TOOL_FINISH",
          sessionId: msg.session_id,
          index: msg.tool_call.index,
        });
      }
    });

    const unsubCodexAdded = onMessage("codex_session_added", (msg: WsMessage) => {
      if (msg.session) {
        dispatch({ type: "ADD_CODEX_SESSION", session: msg.session as CodexSession });
      }
    });

    const unsubCodexRemoved = onMessage("codex_session_removed", (msg: WsMessage) => {
      if (msg.session_id) {
        dispatch({ type: "REMOVE_CODEX_SESSION", sessionId: msg.session_id });
      }
    });

    const unsubCodexMsg = onMessage("codex_message", (msg: WsMessage) => {
      if (msg.session_id && msg.message) {
        dispatch({ type: "ADD_CODEX_MESSAGE", sessionId: msg.session_id, message: msg.message });
      }
    });

    const unsubCodexDelta = onMessage("codex_message_delta", (msg: WsMessage) => {
      if (msg.session_id && msg.delta) {
        dispatch({ type: "APPEND_STREAMING", sessionId: msg.session_id, delta: msg.delta });
      }
    });

    const unsubCodexBusy = onMessage("codex_busy", (msg: WsMessage) => {
      if (msg.session_id && msg.busy !== undefined) {
        dispatch({ type: "SET_CODEX_BUSY", sessionId: msg.session_id, busy: msg.busy });
      }
    });

    const unsubCodexError = onMessage("codex_error", (msg: WsMessage) => {
      if (msg.session_id && msg.line) {
        dispatch({ type: "CODEX_ERROR", sessionId: msg.session_id, error: msg.line });
      }
    });

    const unsubCodexItemStarted = onMessage("codex_item_started", (msg: WsMessage) => {
      if (msg.session_id && msg.tool_call) {
        dispatch({
          type: "CODEX_ITEM_STARTED",
          sessionId: msg.session_id,
          index: msg.tool_call.index,
          id: msg.tool_call.id || "",
          name: msg.tool_call.name || "",
          command: msg.delta || "",
        });
      }
    });

    const unsubCodexItemCompleted = onMessage("codex_item_completed", (msg: WsMessage) => {
      if (msg.session_id && msg.tool_call) {
        dispatch({
          type: "CODEX_ITEM_COMPLETED",
          sessionId: msg.session_id,
          index: msg.tool_call.index,
          text: msg.delta || "",
        });
      }
    });

    return () => {
      unsubConnected();
      unsubLog();
      unsubStatus();
      unsubSessionAdded();
      unsubSessionRemoved();
      unsubClaudeAdded();
      unsubClaudeRemoved();
      unsubClaudeDelta();
      unsubClaudeMsg();
      unsubClaudeBusy();
      unsubToolStart();
      unsubToolDelta();
      unsubToolFinish();
      unsubCodexAdded();
      unsubCodexRemoved();
      unsubCodexMsg();
      unsubCodexDelta();
      unsubCodexBusy();
      unsubCodexError();
      unsubCodexItemStarted();
      unsubCodexItemCompleted();
    };
  }, [onMessage]);

  const actions: Actions = {
    startSession: async (folder: string) => {
      const sess = await api.post<Session>("/api/sessions", { folder });
      dispatch({ type: "ADD_SESSION", session: sess });
      return sess;
    },
    killSession: async (id: string) => {
      await api.del(`/api/sessions/${id}`);
      dispatch({
        type: "UPDATE_STATUS",
        sessionId: id,
        status: "stopped",
        restarts: 0,
      });
    },
    restartSession: async (id: string) => {
      const sess = await api.post<Session>(`/api/sessions/${id}/restart`);
      dispatch({ type: "UPDATE_SESSION", session: sess });
    },
    createClaudeSession: async (folder: string) => {
      const sess = await api.post<ClaudeSession>("/api/claude/sessions", { folder });
      dispatch({ type: "ADD_CLAUDE_SESSION", session: sess });
      return sess;
    },
    closeClaudeSession: async (id: string) => {
      await api.del(`/api/claude/sessions/${id}`);
      dispatch({ type: "REMOVE_CLAUDE_SESSION", sessionId: id });
    },
    sendClaudeMessage: async (id: string, message: string, attachments: DraftAttachment[] = []) => {
      const timestamp = new Date().toISOString();
      const optimisticId = createOptimisticMessageId();
      dispatch({ type: "SET_CLAUDE_BUSY", sessionId: id, busy: true });
      dispatch({ type: "CLEAR_STREAMING", sessionId: id });
      dispatch({
        type: "ADD_CLAUDE_MESSAGE",
        sessionId: id,
        message: {
          role: "user",
          kind: "text",
          content: message,
          timestamp,
          attachments: toOptimisticAttachments(attachments),
          optimistic: true,
          optimistic_id: optimisticId,
        },
      });
      try {
        await sendPrompt(`/api/claude/sessions/${id}/send`, message, attachments);
      } catch (error) {
        dispatch({
          type: "REMOVE_CLAUDE_OPTIMISTIC_MESSAGE",
          sessionId: id,
          optimisticId,
        });
        dispatch({ type: "SET_CLAUDE_BUSY", sessionId: id, busy: false });
        throw error;
      }
    },
    sendClaudeCommand: async (id: string, command: string) => {
      const timestamp = new Date().toISOString();
      const optimisticId = createOptimisticMessageId();
      dispatch({ type: "SET_CLAUDE_BUSY", sessionId: id, busy: true });
      dispatch({ type: "CLEAR_STREAMING", sessionId: id });
      dispatch({
        type: "ADD_CLAUDE_MESSAGE",
        sessionId: id,
        message: {
          role: "user",
          kind: "bash",
          content: command,
          timestamp,
          command: {
            command,
          },
          optimistic: true,
          optimistic_id: optimisticId,
        },
      });
      try {
        await runCommand(`/api/claude/sessions/${id}/command`, command);
      } catch (error) {
        dispatch({
          type: "REMOVE_CLAUDE_OPTIMISTIC_MESSAGE",
          sessionId: id,
          optimisticId,
        });
        dispatch({ type: "SET_CLAUDE_BUSY", sessionId: id, busy: false });
        throw error;
      }
    },
    createCodexSession: async (folder: string) => {
      const sess = await api.post<CodexSession>("/api/codex/sessions", { folder });
      dispatch({ type: "ADD_CODEX_SESSION", session: sess });
      return sess;
    },
    closeCodexSession: async (id: string) => {
      await api.del(`/api/codex/sessions/${id}`);
      dispatch({ type: "REMOVE_CODEX_SESSION", sessionId: id });
    },
    cancelCodexSession: async (id: string) => {
      await api.post(`/api/codex/sessions/${id}/cancel`);
    },
    sendCodexMessage: async (id: string, message: string, attachments: DraftAttachment[] = []) => {
      const timestamp = new Date().toISOString();
      const optimisticId = createOptimisticMessageId();
      dispatch({ type: "SET_CODEX_BUSY", sessionId: id, busy: true });
      dispatch({ type: "CLEAR_STREAMING", sessionId: id });
      dispatch({
        type: "ADD_CODEX_MESSAGE",
        sessionId: id,
        message: {
          role: "user",
          kind: "text",
          content: message,
          timestamp,
          attachments: toOptimisticAttachments(attachments),
          optimistic: true,
          optimistic_id: optimisticId,
        },
      });
      try {
        await sendPrompt(`/api/codex/sessions/${id}/send`, message, attachments);
      } catch (error) {
        dispatch({
          type: "REMOVE_CODEX_OPTIMISTIC_MESSAGE",
          sessionId: id,
          optimisticId,
        });
        dispatch({ type: "SET_CODEX_BUSY", sessionId: id, busy: false });
        throw error;
      }
    },
    sendCodexCommand: async (id: string, command: string) => {
      const timestamp = new Date().toISOString();
      const optimisticId = createOptimisticMessageId();
      dispatch({ type: "SET_CODEX_BUSY", sessionId: id, busy: true });
      dispatch({ type: "CLEAR_STREAMING", sessionId: id });
      dispatch({
        type: "ADD_CODEX_MESSAGE",
        sessionId: id,
        message: {
          role: "user",
          kind: "bash",
          content: command,
          timestamp,
          command: {
            command,
          },
          optimistic: true,
          optimistic_id: optimisticId,
        },
      });
      try {
        await runCommand(`/api/codex/sessions/${id}/command`, command);
      } catch (error) {
        dispatch({
          type: "REMOVE_CODEX_OPTIMISTIC_MESSAGE",
          sessionId: id,
          optimisticId,
        });
        dispatch({ type: "SET_CODEX_BUSY", sessionId: id, busy: false });
        throw error;
      }
    },
  };

  return { state, dispatch, actions };
}

export function useSessions() {
  return useContext(SessionsContext);
}
