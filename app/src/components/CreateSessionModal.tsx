import { useState, useEffect } from "react";
import type { AdoptableSession, ChatSession, HistorySession, ProviderMetadata, ProviderModel } from "../api/types";
import {
  getProviderDisplayName,
  getProviderMetadata,
  listProviderIDs,
  summarizeProviderCapabilities,
} from "../lib/providers";
import { FolderPicker } from "./FolderPicker";

interface Props {
  folders: string[];
  chatSessions: Record<string, ChatSession[]>;
  providers: Record<string, ProviderMetadata>;
  onClose: () => void;
  onCreateSession: (provider: string, folder: string, options?: { model?: string; reasoning_effort?: string }) => Promise<void>;
  onLoadModels?: (provider: string) => Promise<ProviderModel[]>;
  onLoadFolders?: (provider: string) => Promise<string[]>;
  onLoadAdoptableSessions: (provider: string) => Promise<AdoptableSession[]>;
  onLoadHistory?: (provider: string) => Promise<HistorySession[]>;
  onAdoptSession: (provider: string, threadID: string) => Promise<void>;
}

export function CreateSessionModal({
  folders,
  chatSessions,
  providers,
  onClose,
  onCreateSession,
  onLoadModels,
  onLoadFolders,
  onLoadAdoptableSessions,
  onLoadHistory,
  onAdoptSession,
}: Props) {
  const providerList = listProviderIDs(providers, chatSessions).map((providerID) =>
    getProviderMetadata(providerID, providers)
  );
  const [providerID, setProviderID] = useState<string>("");
  const [mode, setMode] = useState<"new" | "adopt" | "history">("new");
  const [model, setModel] = useState("");
  const [reasoning, setReasoning] = useState("");
  const [models, setModels] = useState<ProviderModel[]>([]);
  const [providerFolders, setProviderFolders] = useState<string[]>([]);
  const [folderLoading, setFolderLoading] = useState(false);
  const [folderError, setFolderError] = useState<string | null>(null);
  const [modelLoading, setModelLoading] = useState(false);
  const [modelError, setModelError] = useState<string | null>(null);
  const [adoptFilter, setAdoptFilter] = useState("");
  const [adoptableSessions, setAdoptableSessions] = useState<AdoptableSession[]>([]);
  const [adoptLoading, setAdoptLoading] = useState(false);
  const [adoptError, setAdoptError] = useState<string | null>(null);
  const [adoptLoadedFor, setAdoptLoadedFor] = useState<string | null>(null);
  const [historyFilter, setHistoryFilter] = useState("");
  const [historySessions, setHistorySessions] = useState<HistorySession[]>([]);
  const [historyLoading, setHistoryLoading] = useState(false);
  const [historyError, setHistoryError] = useState<string | null>(null);
  const [historyLoadedFor, setHistoryLoadedFor] = useState<string | null>(null);

  useEffect(() => {
    if (providerList.length > 0 && !providerID) {
      setProviderID(providerList[0].id);
    }
  }, [providerList, providerID]);

  const selectedProvider =
    providerList.find((provider) => provider.id === providerID) || null;
  const canAdoptExistingSessions = Boolean(selectedProvider?.chat?.adopt_existing_sessions);
  const isRemoteProvider = providerID.startsWith("ssh-");
  const canViewHistory = Boolean(selectedProvider?.chat?.history);
  const canChooseModel = providerID === "codex" || providerID === "antigravity" || isRemoteProvider;

  useEffect(() => {
    if (!providerID || !onLoadModels) return;
    let cancelled = false;
    setModelLoading(true);
    setModelError(null);
    void onLoadModels(providerID)
      .then((available) => {
        if (!cancelled) {
          setModels(available);
          setModel((current) => current || available[0]?.slug || "");
          setReasoning((current) => current || available[0]?.default_reasoning || "");
        }
      })
      .catch((error: unknown) => {
        if (!cancelled) setModelError(error instanceof Error ? error.message : String(error));
      })
      .finally(() => {
        if (!cancelled) setModelLoading(false);
      });
    return () => { cancelled = true; };
  }, [providerID, onLoadModels]);

  useEffect(() => {
    if (!providerID || !isRemoteProvider || !onLoadFolders) return;
    let cancelled = false;
    setFolderLoading(true);
    setFolderError(null);
    void onLoadFolders(providerID)
      .then((available) => {
        if (!cancelled) setProviderFolders(available);
      })
      .catch((error: unknown) => {
        if (!cancelled) setFolderError(error instanceof Error ? error.message : String(error));
      })
      .finally(() => {
        if (!cancelled) setFolderLoading(false);
      });
    return () => { cancelled = true; };
  }, [providerID, isRemoteProvider, onLoadFolders]);

  useEffect(() => {
    setMode("new");
    setModel("");
    setReasoning("");
    setModels([]);
    setProviderFolders([]);
    setFolderError(null);
    setModelError(null);
    setAdoptFilter("");
    setAdoptableSessions([]);
    setAdoptError(null);
    setAdoptLoading(false);
    setAdoptLoadedFor(null);
    setHistoryFilter("");
    setHistorySessions([]);
    setHistoryError(null);
    setHistoryLoadedFor(null);
  }, [providerID]);

  useEffect(() => {
    if (!providerID || !canViewHistory || mode !== "history" || historyLoadedFor === providerID || !onLoadHistory) {
      return;
    }
    let cancelled = false;
    setHistoryLoading(true);
    setHistoryError(null);
    void onLoadHistory(providerID)
      .then((history) => {
        if (!cancelled) {
          setHistorySessions(history);
          setHistoryLoadedFor(providerID);
        }
      })
      .catch((error: unknown) => {
        if (!cancelled) setHistoryError(error instanceof Error ? error.message : String(error));
      })
      .finally(() => {
        if (!cancelled) setHistoryLoading(false);
      });
    return () => { cancelled = true; };
  }, [providerID, canViewHistory, mode, historyLoadedFor, onLoadHistory]);

  useEffect(() => {
    if (!providerID || !canAdoptExistingSessions || mode !== "adopt" || adoptLoadedFor === providerID) {
      return;
    }

    let cancelled = false;
    setAdoptLoading(true);
    setAdoptError(null);

    void onLoadAdoptableSessions(providerID)
      .then((sessions) => {
        if (!cancelled) {
          setAdoptableSessions(sessions);
          setAdoptLoadedFor(providerID);
        }
      })
      .catch((error: unknown) => {
        if (!cancelled) {
          const message = error instanceof Error ? error.message : String(error);
          setAdoptError(message);
          setAdoptableSessions([]);
        }
      })
      .finally(() => {
        if (!cancelled) {
          setAdoptLoading(false);
        }
      });

    return () => {
      cancelled = true;
    };
  }, [providerID, canAdoptExistingSessions, mode, adoptLoadedFor, onLoadAdoptableSessions]);

  const handleSelect = async (folder: string) => {
    if (!providerID) return;
    const options = {
      ...(model ? { model } : {}),
      ...(reasoning ? { reasoning_effort: reasoning } : {}),
    };
    if (Object.keys(options).length === 0) {
      await onCreateSession(providerID, folder);
    } else {
      await onCreateSession(providerID, folder, options);
    }
    onClose();
  };

  const handleAdopt = async (threadID: string) => {
    if (!providerID) return;
    await onAdoptSession(providerID, threadID);
    onClose();
  };

  const filteredAdoptableSessions = adoptableSessions.filter((session) => {
    const query = adoptFilter.trim().toLowerCase();
    if (!query) {
      return true;
    }

    return [
      session.rel_name,
      session.rel_cwd,
      session.title,
      session.model || "",
    ]
      .join(" ")
      .toLowerCase()
      .includes(query);
  });

  const filteredHistorySessions = historySessions.filter((session) => {
    const query = historyFilter.trim().toLowerCase();
    if (!query) return true;
    return [session.rel_name, session.rel_cwd || "", session.title || "", session.model || "", session.preview || ""]
      .join(" ")
      .toLowerCase()
      .includes(query);
  });

  return (
    <div className="modal-backdrop" onClick={onClose}>
      <div className="modal-shell" onClick={(e) => e.stopPropagation()}>
        <div className="modal-kicker">New session</div>
        <h2>Choose provider & repository</h2>
        <p>Pick a provider, model, and workspace — or reconnect to a conversation already running elsewhere.</p>

        <div className="modal-agent-switch">
          {providerList.length === 0 ? (
            <div className="sidebar-empty">No providers available yet.</div>
          ) : (
            providerList.map((provider) => (
              <button
                key={provider.id}
                onClick={() => setProviderID(provider.id)}
                className={providerID === provider.id ? "is-active" : ""}
              >
                <span className="modal-agent-switch__name">
                  {getProviderDisplayName(provider, providers)}
                </span>
                {" "}
                <span className="modal-agent-switch__meta">
                  {summarizeProviderCapabilities(provider)}
                </span>
              </button>
            ))
          )}
        </div>

        {mode === "new" && canChooseModel && (
          <div className="grid gap-2 sm:grid-cols-2">
            <label className="text-xs text-text-muted">
              Model
              <select
                value={model}
                onChange={(event) => {
                  const next = event.target.value;
                  setModel(next);
                  const selected = models.find((entry) => entry.slug === next);
                  setReasoning(selected?.default_reasoning || "");
                }}
                className="mt-1 w-full rounded-lg border border-border bg-bg-input px-3 py-2.5 text-sm text-text-primary outline-none focus:border-accent-blue"
                disabled={modelLoading || models.length === 0}
              >
                {models.length === 0 ? <option value="">{modelLoading ? "Loading models..." : "CLI default"}</option> : models.map((entry) => (
                  <option key={entry.slug} value={entry.slug}>{entry.display_name || entry.slug}</option>
                ))}
              </select>
            </label>
            <label className="text-xs text-text-muted">
              Reasoning
              <select
                value={reasoning}
                onChange={(event) => setReasoning(event.target.value)}
                className="mt-1 w-full rounded-lg border border-border bg-bg-input px-3 py-2.5 text-sm text-text-primary outline-none focus:border-accent-blue"
                disabled={!models.find((entry) => entry.slug === model)?.reasoning_levels?.length}
              >
                <option value="">Provider default</option>
                {(models.find((entry) => entry.slug === model)?.reasoning_levels || []).map((level) => (
                  <option key={level} value={level}>{level}</option>
                ))}
              </select>
            </label>
            {modelError && <div className="sm:col-span-2 text-xs text-red-300">Model list unavailable: {modelError}</div>}
          </div>
        )}

        {(canAdoptExistingSessions || canViewHistory) && (
          <div className="modal-mode-switch-shell">
            <div className="modal-mode-switch__label">Session mode</div>
            <div className="modal-mode-switch" role="group" aria-label="Session mode">
              <button
                type="button"
                className={mode === "new" ? "is-active" : ""}
                aria-pressed={mode === "new"}
                onClick={() => setMode("new")}
              >
                <span className="modal-mode-switch__title">New session</span>
                <span className="modal-mode-switch__hint">Start from a repository</span>
              </button>
              {canViewHistory && (
                <button
                  type="button"
                  className={mode === "history" ? "is-active" : ""}
                  aria-pressed={mode === "history"}
                  onClick={() => setMode("history")}
                >
                  <span className="modal-mode-switch__title">History</span>
                  <span className="modal-mode-switch__hint">Browse every saved thread</span>
                </button>
              )}
              <button
                type="button"
                className={mode === "adopt" ? "is-active" : ""}
                aria-pressed={mode === "adopt"}
                onClick={() => setMode("adopt")}
              >
                <span className="modal-mode-switch__title">Adopt existing</span>
                <span className="modal-mode-switch__hint">Attach a running thread</span>
              </button>
            </div>
          </div>
        )}

        {mode === "adopt" && canAdoptExistingSessions ? (
          <div className="space-y-2">
            <input
              type="text"
              value={adoptFilter}
              onChange={(e) => setAdoptFilter(e.target.value)}
              placeholder="Search existing sessions..."
              className="w-full rounded-lg border border-border bg-bg-input px-3 py-2.5 text-sm text-text-primary placeholder:text-text-muted outline-none focus:border-accent-blue focus:ring-2 focus:ring-accent-blue/10 transition-all"
              autoFocus
            />
            <div className="max-h-60 overflow-y-auto space-y-0.5">
              {adoptLoading ? (
                <div className="px-3 py-2.5 text-sm text-text-muted">Loading existing sessions...</div>
              ) : adoptError ? (
                <div className="px-3 py-2.5 text-sm text-red-300">{adoptError}</div>
              ) : filteredAdoptableSessions.length === 0 ? (
                <div className="px-3 py-2.5 text-sm text-text-muted">No adoptable sessions found</div>
              ) : (
                filteredAdoptableSessions.map((session) => (
                  <button
                    key={session.thread_id}
                    onClick={() => handleAdopt(session.thread_id)}
                    className="w-full text-left rounded-lg px-3 py-2.5 text-sm text-text-secondary hover:bg-bg-hover hover:text-text-primary transition-colors"
                  >
                    <div className="font-medium text-text-primary">
                      {session.title || session.rel_name}
                    </div>
                    <div className="mt-1 text-xs text-text-muted">
                      {session.rel_name}
                      {session.rel_cwd ? ` • ${session.rel_cwd}` : ""}
                      {session.model ? ` • ${session.model}` : ""}
                    </div>
                  </button>
                ))
              )}
            </div>
          </div>
        ) : mode === "history" && canViewHistory ? (
          <div className="space-y-2">
            <input
              type="text"
              value={historyFilter}
              onChange={(event) => setHistoryFilter(event.target.value)}
              placeholder="Search history by project, title, or message..."
              className="w-full rounded-lg border border-border bg-bg-input px-3 py-2.5 text-sm text-text-primary placeholder:text-text-muted outline-none focus:border-accent-blue focus:ring-2 focus:ring-accent-blue/10 transition-all"
              autoFocus
            />
            <div className="max-h-72 overflow-y-auto space-y-0.5">
              {historyLoading ? (
                <div className="px-3 py-2.5 text-sm text-text-muted">Loading conversation history...</div>
              ) : historyError ? (
                <div className="px-3 py-2.5 text-sm text-red-300">{historyError}</div>
              ) : filteredHistorySessions.length === 0 ? (
                <div className="px-3 py-2.5 text-sm text-text-muted">No saved history found.</div>
              ) : filteredHistorySessions.map((session) => (
                <button
                  key={session.thread_id}
                  onClick={() => handleAdopt(session.thread_id)}
                  className="w-full text-left rounded-lg px-3 py-2.5 text-sm text-text-secondary hover:bg-bg-hover hover:text-text-primary transition-colors"
                >
                  <div className="flex items-center justify-between gap-2">
                    <span className="font-medium text-text-primary truncate">{session.title || session.rel_name}</span>
                    <span className="text-[10px] uppercase text-text-muted">{session.archived ? "archived" : "saved"}</span>
                  </div>
                  <div className="mt-1 text-xs text-text-muted">
                    {session.rel_name}{session.rel_cwd ? ` • ${session.rel_cwd}` : ""}{session.model ? ` • ${session.model}` : ""}
                  </div>
                  {session.preview && <div className="mt-1 truncate text-xs text-text-secondary">{session.preview}</div>}
                </button>
              ))}
            </div>
          </div>
        ) : (
          isRemoteProvider && folderLoading ? (
            <div className="sidebar-empty">Loading repositories from the SSH host...</div>
          ) : isRemoteProvider && folderError ? (
            <div className="text-xs text-red-300">Remote repositories unavailable: {folderError}</div>
          ) : (
            <FolderPicker folders={isRemoteProvider ? providerFolders : folders} onSelect={handleSelect} />
          )
        )}

        <button onClick={onClose} className="modal-cancel-button">
          Cancel
        </button>
      </div>
    </div>
  );
}
