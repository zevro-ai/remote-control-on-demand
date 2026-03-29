import { useEffect, useState, type FormEvent } from "react";
import { api, clearToken, getToken, setToken } from "./api/client";
import type { AuthStatus, DeploymentInfo } from "./api/types";
import { SessionsContext, useSessionsReducer } from "./hooks/useSessions";
import { useWs } from "./hooks/WebSocketContext";
import { useFolders } from "./hooks/useFolders";
import { usePanelManager } from "./hooks/usePanelManager";
import { Sidebar } from "./components/Sidebar";
import { PanelLayout } from "./components/PanelLayout";
import { CreateSessionModal } from "./components/CreateSessionModal";
import type { OverviewDensity } from "./lib/sessionWall";

const DEPLOYMENT_POLL_INTERVAL_MS = 30_000;

export function buildExternalLoginURL(
  loginURL: string,
  locationLike: Pick<Location, "origin" | "pathname" | "search" | "hash"> = window.location,
) {
  const url = new URL(loginURL, locationLike.origin);
  const redirect = `${locationLike.pathname}${locationLike.search}${locationLike.hash}` || "/";
  url.searchParams.set("redirect", redirect);
  return url.toString();
}

export default function App() {
  const { state, dispatch, actions } = useSessionsReducer();
  const { connected } = useWs();
  const { folders } = useFolders();
  const { focusedPanel, focusPanel, clearFocus } = usePanelManager();
  const [showModal, setShowModal] = useState(false);
  const [density, setDensity] = useState<OverviewDensity>("comfortable");
  const [tokenDraft, setTokenDraft] = useState(() => getToken() || "");
  const [authStatus, setAuthStatus] = useState<AuthStatus | null>(null);
  const [initialDeployment, setInitialDeployment] = useState<DeploymentInfo | null>(null);
  const [latestDeployment, setLatestDeployment] = useState<DeploymentInfo | null>(null);
  const shouldPollDeployment = shouldPollDeploymentMeta(state.loading, state.authRequired);

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
    if (!shouldPollDeployment) {
      return;
    }

    let cancelled = false;

    const loadDeploymentInfo = async () => {
      try {
        const info = await api.get<DeploymentInfo>("/api/meta");
        if (cancelled) {
          return;
        }
        setLatestDeployment(info);
        setInitialDeployment((current) => current || info);
      } catch {
        // Ignore deployment metadata polling failures. The dashboard can keep
        // running even if this auxiliary request is temporarily unavailable.
      }
    };

    void loadDeploymentInfo();
    const intervalID = window.setInterval(() => {
      void loadDeploymentInfo();
    }, DEPLOYMENT_POLL_INTERVAL_MS);

    return () => {
      cancelled = true;
      window.clearInterval(intervalID);
    };
  }, [shouldPollDeployment]);

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

  const onAdoptSession = async (provider: string, threadID: string) => {
    const session = await actions.adoptChatSession(provider, threadID);
    focusPanel(session.id, provider);
  };

  const showDeploymentRefresh = hasDeploymentUpdate(initialDeployment, latestDeployment);

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
          <div className="dashboard-main__stack">
            {showDeploymentRefresh && latestDeployment && (
              <DeploymentRefreshBanner
                initialDeployment={initialDeployment}
                latestDeployment={latestDeployment}
                onRefresh={() => window.location.reload()}
              />
            )}

            <div className="dashboard-main__content">
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
            </div>
          </div>
        </main>

        {showModal && (
          <CreateSessionModal
            folders={folders}
            chatSessions={state.chatSessions}
            providers={state.providers}
            onClose={() => setShowModal(false)}
            onCreateSession={onCreateSession}
            onLoadAdoptableSessions={actions.loadAdoptableChatSessions}
            onAdoptSession={onAdoptSession}
          />
        )}
      </div>
    </SessionsContext.Provider>
  );
}

export function hasDeploymentUpdate(
  initialDeployment: DeploymentInfo | null,
  latestDeployment: DeploymentInfo | null,
) {
  if (!initialDeployment || !latestDeployment) {
    return false;
  }

  return (
    initialDeployment.build_id.trim() !== "" &&
    latestDeployment.build_id.trim() !== "" &&
    initialDeployment.build_id !== latestDeployment.build_id
  );
}

export function shouldPollDeploymentMeta(loading: boolean, authRequired: boolean) {
  return !loading && !authRequired;
}

export function DeploymentRefreshBanner({
  initialDeployment,
  latestDeployment,
  onRefresh,
}: {
  initialDeployment: DeploymentInfo | null;
  latestDeployment: DeploymentInfo;
  onRefresh: () => void;
}) {
  const versionChanged =
    initialDeployment &&
    initialDeployment.version.trim() !== "" &&
    latestDeployment.version.trim() !== "" &&
    initialDeployment.version !== latestDeployment.version;

  return (
    <section className="deployment-banner" role="status" aria-live="polite">
      <div className="deployment-banner__copy">
        <div className="deployment-banner__kicker">update available</div>
        <strong>Refresh to load the latest dashboard deployment</strong>
        <p>
          {versionChanged && initialDeployment
            ? `This tab is still running ${initialDeployment.version}. The backend is now on ${latestDeployment.version}.`
            : "This tab is running an older dashboard build. Refresh to load the current deployment."}
        </p>
      </div>
      <button type="button" onClick={onRefresh}>
        Refresh
      </button>
    </section>
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

  const externalAuthEnabled = authStatus?.mode === "external";
  const tokenFallbackEnabled = authStatus?.token_enabled ?? false;

  if (externalAuthEnabled && !tokenFallbackEnabled) {
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
              onClick={() =>
                window.location.assign(
                  buildExternalLoginURL(authStatus.login_url || "/api/auth/login"),
                )
              }
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

  if (externalAuthEnabled) {
    const providerName = authStatus.provider?.display_name || "identity provider";
    return (
      <div className="dashboard-empty">
        <div className="auth-prompt-stack">
          <div className="auth-prompt">
            <div className="auth-prompt__kicker">login required</div>
            <h2>Unlock dashboard access</h2>
            <p>
              This RCOD deployment uses external authentication. Continue with {providerName}
              to open the dashboard, or use the API token fallback below.
            </p>
            <div className="auth-prompt__actions">
              <button
                type="button"
                onClick={() =>
                  window.location.assign(
                    buildExternalLoginURL(authStatus.login_url || "/api/auth/login"),
                  )
                }
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

          <form className="auth-prompt" onSubmit={handleSubmit}>
            <div className="auth-prompt__kicker">token fallback</div>
            <h2>Use API token instead</h2>
            <p>
              This deployment also allows bearer-token access. Enter `api.token` to store it
              locally and retry the dashboard bootstrap.
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
