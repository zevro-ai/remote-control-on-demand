import { useEffect, useRef, useState } from "react";
import { ReconnectingWebSocket } from "../api/ws";
import type { WsMessage } from "../api/types";

export function useWebSocket(onMessage: (msg: WsMessage) => void) {
  const wsRef = useRef<ReconnectingWebSocket | null>(null);
  const [connected, setConnected] = useState(false);

  useEffect(() => {
    const ws = new ReconnectingWebSocket();
    wsRef.current = ws;

    ws.on("connected", () => setConnected(true));
    ws.on("disconnected", () => setConnected(false));
    ws.on("*", onMessage);

    ws.connect();
    return () => ws.close();
  }, []); // eslint-disable-line react-hooks/exhaustive-deps

  const subscribe = (sessionId: string) => {
    wsRef.current?.send({ type: "subscribe", session_id: sessionId });
  };

  const unsubscribe = (sessionId: string) => {
    wsRef.current?.send({ type: "unsubscribe", session_id: sessionId });
  };

  return { connected, subscribe, unsubscribe };
}
