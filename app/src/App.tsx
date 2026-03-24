import { useEffect, useState, type FormEvent } from "react";
import { clearToken, getToken, setToken } from "./api/client";
import { SessionsContext, useSessionsReducer } from "./hooks/useSessions";
import { useWs } from "./hooks/WebSocketContext";
import { useFolders } from "./hooks/useFolders";
import { usePanelManager } from "./hooks/usePanelManager";
import { Sidebar } from "./components/Sidebar";
import { PanelLayout } from "./components/PanelLayout";
import { CreateSessionModal } from "./components/CreateSessionModal";
import type { OverviewDensity } from "./lib/sessionWall";

export default function App() {
  const { state, dispatch, actions } = useSessionsReducer();
  const { connected } = useWs();
  const { folders } = useFolders();
  const { focusedPanel, focusPanel, clearFocus } = usePanelManager();
  const [showModal, setShowModal] = useState(false);
  const [density, setDensity] = useState<OverviewDensity>("comfortable");
  const [tokenDraft, setTokenDraft] = useState(() => getToken() || "");

  useEffect(() => {
    if (!focusedPanel) {
      return;
    }

    const sessions = state.chatSessions[focusedPanel.type] || [];
    const exists = sessions.some((session) => session.id === focusedPanel.sessionId);
    if (!exists) {
      clearFocus();
    }
  }, [clearFocus, focusedPanel, state.chatSessions]);

  const onCreateSession = async (provider: string, folder: string) => {
    const session = await actions.createChatSession(provider, folder);
    focusPanel(session.id, provider);
  };

  return (
    <SessionsContext.Provider value={{ state, dispatch, actions }}>
      <div className="app-shell">
        <Sidebar
          chatSessions={state.chatSessions}
          connected={connected}
          focusedPanel={focusedPanel}
          onNewSession={() => setShowModal(true)}
          onSelectSession={focusPanel}
        />

        <main className="dashboard-main">
          {state.loading ? (
            <div className="dashboard-empty">
              <div className="boot-message">Booting operator grid...</div>
            </div>
          ) : state.authRequired ? (
            <AuthPrompt
              value={tokenDraft}
              hasStoredToken={Boolean(getToken())}
              onChange={setTokenDraft}
            />
          ) : state.loadError ? (
            <div className="dashboard-empty">
              <div className="dashboard-error">
                <strong>Dashboard bootstrap failed</strong>
                <p>{state.loadError}</p>
              </div>
            </div>
          ) : (
            <PanelLayout
              chatSessions={state.chatSessions}
              focusedPanel={focusedPanel}
              density={density}
              onDensityChange={setDensity}
              onFocusSession={focusPanel}
              onClearFocus={clearFocus}
            />
          )}
        </main>

        {showModal && (
          <CreateSessionModal
            folders={folders}
            chatSessions={state.chatSessions}
            onClose={() => setShowModal(false)}
            onCreateSession={onCreateSession}
          />
        )}
      </div>
    </SessionsContext.Provider>
  );
}

function AuthPrompt({
  value,
  hasStoredToken,
  onChange,
}: {
  value: string;
  hasStoredToken: boolean;
  onChange: (value: string) => void;
}) {
  const handleSubmit = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    const trimmed = value.trim();
    if (!trimmed) {
      return;
    }

    setToken(trimmed);
    window.location.reload();
  };

  const handleClear = () => {
    clearToken();
    onChange("");
  };

  return (
    <div className="dashboard-empty">
      <form className="auth-prompt" onSubmit={handleSubmit}>
        <div className="auth-prompt__kicker">token required</div>
        <h2>Unlock dashboard access</h2>
        <p>
          This RCOD deployment is protected by `api.token`. Enter the bearer token to
          store it in local storage and retry the dashboard bootstrap.
        </p>
        <input
          type="password"
          autoFocus
          value={value}
          onChange={(event) => onChange(event.target.value)}
          placeholder="Paste API token"
        />
        <div className="auth-prompt__actions">
          <button type="submit" disabled={!value.trim()}>
            Save token
          </button>
          {hasStoredToken && (
            <button type="button" className="is-secondary" onClick={handleClear}>
              Clear stored token
            </button>
          )}
        </div>
      </form>
    </div>
  );
}
