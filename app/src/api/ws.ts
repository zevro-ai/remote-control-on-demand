import type { WsMessage } from "./types";

type Listener = (msg: WsMessage) => void;

export class ReconnectingWebSocket {
  private url: string;
  private ws: WebSocket | null = null;
  private listeners: Map<string, Set<Listener>> = new Map();
  private reconnectDelay = 1000;
  private maxDelay = 30000;
  private closed = false;

  constructor(url?: string) {
    const proto = location.protocol === "https:" ? "wss:" : "ws:";
    this.url = url || `${proto}//${location.host}/ws`;
  }

  connect() {
    if (this.closed) return;

    const token = localStorage.getItem("rcod_token");
    const wsUrl = new URL(this.url, window.location.href);
    if (token) {
      wsUrl.searchParams.set("access_token", token);
    }
    this.ws = new WebSocket(wsUrl.toString());

    this.ws.onopen = () => {
      this.reconnectDelay = 1000;
      this.emit({ type: "connected" });
    };

    this.ws.onmessage = (event) => {
      try {
        const msg: WsMessage = JSON.parse(event.data);
        this.emit(msg);
      } catch {
        // ignore parse errors
      }
    };

    this.ws.onclose = () => {
      this.emit({ type: "disconnected" });
      if (!this.closed) {
        setTimeout(() => this.connect(), this.reconnectDelay);
        this.reconnectDelay = Math.min(
          this.reconnectDelay * 2,
          this.maxDelay
        );
      }
    };

    this.ws.onerror = () => {
      this.ws?.close();
    };
  }

  send(msg: { type: string; session_id?: string }) {
    if (this.ws?.readyState === WebSocket.OPEN) {
      this.ws.send(JSON.stringify(msg));
    }
  }

  on(type: string, fn: Listener) {
    if (!this.listeners.has(type)) {
      this.listeners.set(type, new Set());
    }
    this.listeners.get(type)!.add(fn);
    return () => this.listeners.get(type)?.delete(fn);
  }

  private emit(msg: WsMessage) {
    this.listeners.get(msg.type)?.forEach((fn) => fn(msg));
    this.listeners.get("*")?.forEach((fn) => fn(msg));
  }

  close() {
    this.closed = true;
    this.ws?.close();
  }
}
