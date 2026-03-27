import { useEffect, useState, type FormEvent } from "react";
import { api, clearToken, getToken, setToken } from "./api/client";
import type { AuthStatus } from "./api/types";
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
  const [authStatus, setAuthStatus] = useState<AuthStatus | null>(null);

  useEffect(() => {
    let cancelled = false;

    api.get<AuthStatus>("/api/auth/status")
      .then((status) => {
        if (!cancelled) {
          setAuthStatus(status);
        }
      })
      .catch(() => {
        if (!cancelled) {
          setAuthStatus(null);
        }
      });

    return () => {
      cancelled = true;
    };
  }, []);

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
          authStatus={authStatus}
          providers={state.providers}
          chatSessions={state.chatSessions}
          connected={connected}
          focusedPanel={focusedPanel}
          onLogout={async () => {
            clearToken();
            await api.post("/api/auth/logout");
            window.location.reload();
          }}
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
              authStatus={authStatus}
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
              providers={state.providers}
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
            providers={state.providers}
            onClose={() => setShowModal(false)}
            onCreateSession={onCreateSession}
          />
        )}
      </div>
    </SessionsContext.Provider>
  );
}

export function AuthPrompt({
  authStatus,
  value,
  hasStoredToken,
  onChange,
}: {
  authStatus: AuthStatus | null;
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

  if (authStatus?.mode === "external") {
    const providerName = authStatus.provider?.display_name || "identity provider";
    return (
      <div className="dashboard-empty">
        <div className="auth-prompt">
          <div className="auth-prompt__kicker">login required</div>
          <h2>Unlock dashboard access</h2>
          <p>
            This RCOD deployment uses external authentication. Continue with {providerName}
            to open the dashboard.
          </p>
          <div className="auth-prompt__actions">
            <button
              type="button"
              onClick={() => window.location.assign(authStatus.login_url || "/api/auth/login")}
            >
              Sign in with {providerName}
            </button>
            {hasStoredToken && (
              <button type="button" className="is-secondary" onClick={handleClear}>
                Clear stored token
              </button>
            )}
          </div>
        </div>
      </div>
    );
  }

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
