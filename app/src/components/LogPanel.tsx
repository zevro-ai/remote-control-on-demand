import { useEffect, useRef, useCallback } from "react";
import { useWs } from "../hooks/WebSocketContext";
import { ansiToSafeHtml } from "../lib/sanitize";
import type { WsMessage } from "../api/types";

const MAX_DOM_LINES = 500;

interface Props {
  sessionId: string;
}

export function LogPanel({ sessionId }: Props) {
  const containerRef = useRef<HTMLDivElement>(null);
  const lineCountRef = useRef(0);
  const autoScrollRef = useRef(true);
  const { onMessage } = useWs();

  const appendLine = useCallback((raw: string) => {
    const container = containerRef.current;
    if (!container) return;

    const div = document.createElement("div");
    div.className = "log-line font-mono text-xs leading-5 break-all text-text-secondary";
    // Safe: ansiToSafeHtml() passes through DOMPurify.sanitize() with allowlist
    // (only <span> tags with class attributes allowed)
    const sanitized = ansiToSafeHtml(raw);
    div.innerHTML = sanitized; // DOMPurify-sanitized content
    container.appendChild(div);
    lineCountRef.current++;

    // Enforce 500-line DOM cap
    while (lineCountRef.current > MAX_DOM_LINES && container.firstChild) {
      container.removeChild(container.firstChild);
      lineCountRef.current--;
    }

    // Auto-scroll if user hasn't scrolled up
    if (autoScrollRef.current) {
      container.scrollTop = container.scrollHeight;
    }
  }, []);

  // Track user scroll position for auto-scroll behavior
  useEffect(() => {
    const container = containerRef.current;
    if (!container) return;

    const handleScroll = () => {
      const { scrollTop, scrollHeight, clientHeight } = container;
      autoScrollRef.current = scrollHeight - scrollTop - clientHeight < 40;
    };

    container.addEventListener("scroll", handleScroll, { passive: true });
    return () => container.removeEventListener("scroll", handleScroll);
  }, []);

  // Subscribe to log messages for this session
  useEffect(() => {
    const unsub = onMessage("log", (msg: WsMessage) => {
      if (msg.session_id === sessionId && msg.line) {
        appendLine(msg.line);
      }
    });
    return unsub;
  }, [sessionId, onMessage, appendLine]);

  return (
    <div
      ref={containerRef}
      className="flex-1 overflow-y-auto px-4 py-2 bg-bg-primary/50"
    />
  );
}
