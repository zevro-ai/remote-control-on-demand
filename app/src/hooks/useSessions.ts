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
  ChatSession,
  DraftAttachment,
  Message,
  MessageAttachment,
  ProviderMetadata,
  StreamBlock,
  WsMessage,
} from "../api/types";
import { api } from "../api/client";
import { useWs } from "./WebSocketContext";
import { mergeIncomingMessage, removeOptimisticMessage } from "../lib/realtimeMessages";
import { hasErrorStatus, isUnauthorizedError } from "../lib/requestErrors";

interface State {
  providers: Record<string, ProviderMetadata>;
  sessions: Session[];
  chatSessions: Record<string, ChatSession[]>; // provider -> sessions
  logs: Record<string, string[]>;
  streamBlocks: Record<string, StreamBlock[]>;
  loading: boolean;
  authRequired: boolean;
  loadError: string | null;
}

interface BootstrapData {
  providers: Record<string, ProviderMetadata>;
  sessions: Session[];
  chatSessions: Record<string, ChatSession[]>;
  authRequired: boolean;
  loadError: string | null;
}

type Action =
  | { type: "SET_PROVIDERS"; providers: Record<string, ProviderMetadata> }
  | { type: "SET_SESSIONS"; sessions: Session[] }
  | { type: "SET_CHAT_SESSIONS"; provider: string; sessions: ChatSession[] }
  | { type: "UPDATE_SESSION"; session: Session }
  | { type: "ADD_LOG"; sessionId: string; line: string }
  | { type: "UPDATE_STATUS"; sessionId: string; status: string; restarts: number }
  | { type: "ADD_SESSION"; session: Session }
  | { type: "REMOVE_SESSION"; sessionId: string }
  | { type: "ADD_CHAT_SESSION"; provider: string; session: ChatSession }
  | { type: "REMOVE_CHAT_SESSION"; provider: string; sessionId: string }
  | { type: "ADD_CHAT_MESSAGE"; provider: string; sessionId: string; message: Message }
  | { type: "REMOVE_OPTIMISTIC_MESSAGE"; provider: string; sessionId: string; optimisticId: string }
  | { type: "APPEND_STREAMING"; sessionId: string; delta: string }
  | { type: "CLEAR_STREAMING"; sessionId: string }
  | { type: "SET_CHAT_BUSY"; provider: string; sessionId: string; busy: boolean }
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
    case "SET_PROVIDERS":
      return { ...state, providers: action.providers };
    case "SET_SESSIONS":
      return { ...state, sessions: action.sessions };
    case "SET_CHAT_SESSIONS":
      return {
        ...state,
        chatSessions: { ...state.chatSessions, [action.provider]: action.sessions },
      };
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
    case "ADD_CHAT_SESSION": {
      const current = state.chatSessions[action.provider] || [];
      if (current.some((s) => s.id === action.session.id)) return state;
      return {
        ...state,
        chatSessions: { ...state.chatSessions, [action.provider]: [action.session, ...current] },
      };
    }
    case "REMOVE_CHAT_SESSION":
      return {
        ...state,
        chatSessions: {
          ...state.chatSessions,
          [action.provider]: (state.chatSessions[action.provider] || []).filter((s) => s.id !== action.sessionId),
        },
        streamBlocks: omitKey(state.streamBlocks, action.sessionId),
      };
    case "ADD_CHAT_MESSAGE": {
      const currentBlocks = state.streamBlocks[action.sessionId] || [];
      const enrichedMessage =
        action.message.role === "assistant" && currentBlocks.length > 0
          ? { ...action.message, blocks: [...currentBlocks] }
          : action.message;
      return {
        ...state,
        chatSessions: {
          ...state.chatSessions,
          [action.provider]: (state.chatSessions[action.provider] || []).map((s) =>
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
        },
        streamBlocks: omitKey(state.streamBlocks, action.sessionId),
      };
    }
    case "REMOVE_OPTIMISTIC_MESSAGE":
      return {
        ...state,
        chatSessions: {
          ...state.chatSessions,
          [action.provider]: (state.chatSessions[action.provider] || []).map((s) =>
            s.id === action.sessionId
              ? {
                  ...s,
                  messages: removeOptimisticMessage(s.messages || [], action.optimisticId),
                }
              : s
          ),
        },
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
    case "SET_CHAT_BUSY":
      return {
        ...state,
        chatSessions: {
          ...state.chatSessions,
          [action.provider]: (state.chatSessions[action.provider] || []).map((s) =>
            s.id === action.sessionId ? { ...s, busy: action.busy } : s
          ),
        },
        streamBlocks: action.busy ? state.streamBlocks : omitKey(state.streamBlocks, action.sessionId),
      };
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
  providers: {},
  sessions: [],
  chatSessions: {},
  logs: {},
  streamBlocks: {},
  loading: true,
  authRequired: false,
  loadError: null,
};

function resolveProviderLabel(provider: string) {
  return provider.charAt(0).toUpperCase() + provider.slice(1);
}

export function normalizeProviders(providers: Array<string | ProviderMetadata>) {
  return providers.map((provider) =>
    typeof provider === "string"
      ? { id: provider, display_name: resolveProviderLabel(provider) }
      : provider
  );
}

export function resolveBootstrapResults(
  providers: string[],
  results: PromiseSettledResult<any>[]
): BootstrapData {
  const [sessionsResult, ...chatResults] = results;
  const sessions = sessionsResult.status === "fulfilled" ? sessionsResult.value : [];
  const chatSessions: Record<string, ChatSession[]> = {};
  const errors: string[] = [];

  const anySuccess = results.some((res) => res.status === "fulfilled");

  if (sessionsResult.status === "rejected") {
    errors.push(`RC: ${sessionsResult.reason.message || sessionsResult.reason}`);
  }

  chatResults.forEach((res, idx) => {
    const provider = providers[idx];
    if (res.status === "fulfilled") {
      chatSessions[provider] = res.value.sessions;
    } else {
      errors.push(`${resolveProviderLabel(provider)}: ${res.reason.message || res.reason}`);
    }
  });

  const authRequired = !anySuccess && results.some((res) => res.status === "rejected" && isUnauthorizedError(res.reason));

  return {
    providers: {},
    sessions,
    chatSessions,
    authRequired,
    loadError: authRequired ? null : (errors.length > 0 ? `Some services failed to load: ${errors.join("; ")}` : null),
  };
}

export function resolveProviderBootstrapFailure(
  providerError: unknown,
  sessionsResult: PromiseSettledResult<Session[]>
): BootstrapData {
  const sessions = sessionsResult.status === "fulfilled" ? sessionsResult.value : [];
  const errors = [`Providers: ${providerError instanceof Error ? providerError.message : String(providerError)}`];

  if (sessionsResult.status === "rejected") {
    errors.push(`RC: ${sessionsResult.reason.message || sessionsResult.reason}`);
  }

  const authRequired =
    sessionsResult.status === "rejected" &&
    isUnauthorizedError(providerError) &&
    isUnauthorizedError(sessionsResult.reason);

  return {
    providers: {},
    sessions,
    chatSessions: {},
    authRequired,
    loadError: authRequired ? null : `Some services failed to load: ${errors.join("; ")}`,
  };
}

async function loadProviders() {
  try {
    return await api.get<Array<string | ProviderMetadata>>("/api/providers");
  } catch (error) {
    if (hasErrorStatus(error, 404)) {
      return api.get<Array<string | ProviderMetadata>>("/api/chat/providers");
    }
    throw error;
  }
}

async function fetchBootstrapData(): Promise<BootstrapData> {
  const [providerResult, sessionsResult] = await Promise.allSettled([
    loadProviders(),
    api.get<Session[]>("/api/sessions"),
  ]);

  if (providerResult.status === "rejected") {
    return resolveProviderBootstrapFailure(providerResult.reason, sessionsResult);
  }

  const providerResponse = providerResult.value;
  const providers = normalizeProviders(providerResponse);
  const providerIDs = providers.map((provider) => provider.id);
  const providerMap = Object.fromEntries(providers.map((provider) => [provider.id, provider])) as Record<string, ProviderMetadata>;

  const chatPromises = providerIDs.map(async (provider) => {
    const sessions = await api.get<ChatSession[]>(`/api/chat/${provider}/sessions`);
    return { provider, sessions };
  });

  const chatResults = await Promise.allSettled(chatPromises);

  const bootstrap = resolveBootstrapResults(providerIDs, [sessionsResult, ...chatResults]);
  return { ...bootstrap, providers: providerMap };
}

type Actions = {
  startSession: (folder: string) => Promise<Session>;
  killSession: (id: string) => Promise<void>;
  restartSession: (id: string) => Promise<void>;
  createChatSession: (provider: string, folder: string) => Promise<ChatSession>;
  closeChatSession: (provider: string, id: string) => Promise<void>;
  sendChatMessage: (provider: string, id: string, message: string, attachments?: DraftAttachment[]) => Promise<void>;
  runChatCommand: (provider: string, id: string, command: string) => Promise<void>;
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
    createChatSession: async () => Promise.reject(new Error("not ready")),
    closeChatSession: async () => {},
    sendChatMessage: async () => {},
    runChatCommand: async () => {},
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

export function useSessionsReducer() {
  const [state, dispatch] = useReducer(reduceSessionsState, sessionsInitialState);
  const { onMessage, subscribe, unsubscribe } = useWs();
  const subscribedSessionIdsRef = useRef(new Set<string>());

  const applyBootstrapData = (data: BootstrapData) => {
    dispatch({ type: "SET_PROVIDERS", providers: data.providers });
    dispatch({ type: "SET_SESSIONS", sessions: data.sessions });
    Object.entries(data.chatSessions).forEach(([provider, sessions]) => {
      dispatch({ type: "SET_CHAT_SESSIONS", provider, sessions });
    });
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

    const unsubChatAdded = onMessage("chat_session_added", (msg: WsMessage) => {
      if (msg.session && msg.provider) {
        dispatch({ type: "ADD_CHAT_SESSION", provider: msg.provider, session: msg.session as ChatSession });
      }
    });

    const unsubChatRemoved = onMessage("chat_session_removed", (msg: WsMessage) => {
      if (msg.session_id && msg.provider) {
        dispatch({ type: "REMOVE_CHAT_SESSION", provider: msg.provider, sessionId: msg.session_id });
      }
    });

    const unsubChatDelta = onMessage("chat_message_delta", (msg: WsMessage) => {
      if (msg.session_id && msg.delta) {
        dispatch({ type: "APPEND_STREAMING", sessionId: msg.session_id, delta: msg.delta });
      }
    });

    const unsubChatMsg = onMessage("chat_message", (msg: WsMessage) => {
      if (msg.session_id && msg.message && msg.provider) {
        dispatch({ type: "ADD_CHAT_MESSAGE", provider: msg.provider, sessionId: msg.session_id, message: msg.message });
      }
    });

    const unsubChatBusy = onMessage("chat_busy", (msg: WsMessage) => {
      if (msg.session_id && msg.busy !== undefined && msg.provider) {
        dispatch({ type: "SET_CHAT_BUSY", provider: msg.provider, sessionId: msg.session_id, busy: msg.busy });
      }
    });

    const unsubToolStart = onMessage("chat_tool_start", (msg: WsMessage) => {
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

    const unsubToolDelta = onMessage("chat_tool_delta", (msg: WsMessage) => {
      if (msg.session_id && msg.tool_call) {
        dispatch({
          type: "TOOL_DELTA",
          sessionId: msg.session_id,
          index: msg.tool_call.index,
          partialJSON: msg.tool_call.partial_json || "",
        });
      }
    });

    const unsubToolFinish = onMessage("chat_tool_finish", (msg: WsMessage) => {
      if (msg.session_id && msg.tool_call) {
        dispatch({
          type: "TOOL_FINISH",
          sessionId: msg.session_id,
          index: msg.tool_call.index,
        });
      }
    });

    return () => {
      unsubConnected();
      unsubLog();
      unsubStatus();
      unsubChatAdded();
      unsubChatRemoved();
      unsubChatDelta();
      unsubChatMsg();
      unsubChatBusy();
      unsubToolStart();
      unsubToolDelta();
      unsubToolFinish();
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
    createChatSession: async (provider: string, folder: string) => {
      const sess = await api.post<ChatSession>(`/api/chat/${provider}/sessions`, { folder });
      dispatch({ type: "ADD_CHAT_SESSION", provider, session: sess });
      return sess;
    },
    closeChatSession: async (provider: string, id: string) => {
      await api.del(`/api/chat/${provider}/sessions/${id}`);
      dispatch({ type: "REMOVE_CHAT_SESSION", provider, sessionId: id });
    },
    sendChatMessage: async (provider: string, id: string, message: string, attachments: DraftAttachment[] = []) => {
      const timestamp = new Date().toISOString();
      const optimisticId = createOptimisticMessageId();
      dispatch({ type: "SET_CHAT_BUSY", provider, sessionId: id, busy: true });
      dispatch({ type: "CLEAR_STREAMING", sessionId: id });
      dispatch({
        type: "ADD_CHAT_MESSAGE",
        provider,
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
        await sendPrompt(`/api/chat/${provider}/sessions/${id}/send`, message, attachments);
      } catch (error) {
        dispatch({
          type: "REMOVE_OPTIMISTIC_MESSAGE",
          provider,
          sessionId: id,
          optimisticId,
        });
        dispatch({ type: "SET_CHAT_BUSY", provider, sessionId: id, busy: false });
        throw error;
      }
    },
    runChatCommand: async (provider: string, id: string, command: string) => {
      const timestamp = new Date().toISOString();
      const optimisticId = createOptimisticMessageId();
      dispatch({ type: "SET_CHAT_BUSY", provider, sessionId: id, busy: true });
      dispatch({ type: "CLEAR_STREAMING", sessionId: id });
      dispatch({
        type: "ADD_CHAT_MESSAGE",
        provider,
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
        await runCommand(`/api/chat/${provider}/sessions/${id}/command`, command);
      } catch (error) {
        dispatch({
          type: "REMOVE_OPTIMISTIC_MESSAGE",
          provider,
          sessionId: id,
          optimisticId,
        });
        dispatch({ type: "SET_CHAT_BUSY", provider, sessionId: id, busy: false });
        throw error;
      }
    },
  };

  return { state, dispatch, actions };
}

export function useSessions() {
  return useContext(SessionsContext);
}
