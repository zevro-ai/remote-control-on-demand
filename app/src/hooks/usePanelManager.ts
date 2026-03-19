import { useCallback, useState } from "react";

export interface PanelState {
  sessionId: string;
  type: string;
}

export function usePanelManager() {
  const [focusedPanel, setFocusedPanel] = useState<PanelState | null>(null);

  const focusPanel = useCallback((sessionId: string, type: string) => {
    setFocusedPanel({ sessionId, type });
  }, []);

  const clearFocus = useCallback(() => {
    setFocusedPanel(null);
  }, []);

  return { focusedPanel, focusPanel, clearFocus };
}
