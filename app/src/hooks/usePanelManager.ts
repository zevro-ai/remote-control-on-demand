import { useCallback, useState } from "react";

export interface PanelState {
  sessionId: string;
  type: "claude" | "codex";
}

export function usePanelManager() {
  const [focusedPanel, setFocusedPanel] = useState<PanelState | null>(null);

  const focusPanel = useCallback((sessionId: string, type: "claude" | "codex") => {
    setFocusedPanel({ sessionId, type });
  }, []);

  const clearFocus = useCallback(() => {
    setFocusedPanel(null);
  }, []);

  return { focusedPanel, focusPanel, clearFocus };
}
