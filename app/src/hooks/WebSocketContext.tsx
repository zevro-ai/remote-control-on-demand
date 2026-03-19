import {
  createContext,
  useContext,
  useEffect,
  useRef,
  useState,
  useCallback,
  type ReactNode,
} from "react";
import { ReconnectingWebSocket } from "../api/ws";
import type { WsMessage } from "../api/types";

interface WebSocketContextValue {
  connected: boolean;
  subscribe: (sessionId: string) => void;
  unsubscribe: (sessionId: string) => void;
  onMessage: (type: string, fn: (msg: WsMessage) => void) => () => void;
}

const WebSocketCtx = createContext<WebSocketContextValue>({
  connected: false,
  subscribe: () => {},
  unsubscribe: () => {},
  onMessage: () => () => {},
});

export function WebSocketProvider({ children }: { children: ReactNode }) {
  const wsRef = useRef<ReconnectingWebSocket | null>(null);
  const activeSubsRef = useRef(new Set<string>());
  const listenersRef = useRef(new Map<string, Set<(msg: WsMessage) => void>>());
  const [connected, setConnected] = useState(false);

  useEffect(() => {
    const ws = new ReconnectingWebSocket();
    wsRef.current = ws;

    ws.on("connected", () => {
      setConnected(true);
      // Re-subscribe to all active sessions on reconnect
      for (const id of activeSubsRef.current) {
        ws.send({ type: "subscribe", session_id: id });
      }
      // Notify listeners of reconnect
      emitToListeners({ type: "connected" });
    });

    ws.on("disconnected", () => {
      setConnected(false);
      emitToListeners({ type: "disconnected" });
    });

    ws.on("*", (msg: WsMessage) => {
      if (msg.type !== "connected" && msg.type !== "disconnected") {
        emitToListeners(msg);
      }
    });

    ws.connect();
    return () => ws.close();
  }, []);

  function emitToListeners(msg: WsMessage) {
    listenersRef.current.get(msg.type)?.forEach((fn) => fn(msg));
    listenersRef.current.get("*")?.forEach((fn) => fn(msg));
  }

  const subscribe = useCallback((sessionId: string) => {
    activeSubsRef.current.add(sessionId);
    wsRef.current?.send({ type: "subscribe", session_id: sessionId });
  }, []);

  const unsubscribe = useCallback((sessionId: string) => {
    activeSubsRef.current.delete(sessionId);
    wsRef.current?.send({ type: "unsubscribe", session_id: sessionId });
  }, []);

  const onMessage = useCallback(
    (type: string, fn: (msg: WsMessage) => void) => {
      if (!listenersRef.current.has(type)) {
        listenersRef.current.set(type, new Set());
      }
      listenersRef.current.get(type)!.add(fn);
      return () => {
        listenersRef.current.get(type)?.delete(fn);
      };
    },
    []
  );

  return (
    <WebSocketCtx.Provider value={{ connected, subscribe, unsubscribe, onMessage }}>
      {children}
    </WebSocketCtx.Provider>
  );
}

export function useWs() {
  return useContext(WebSocketCtx);
}
