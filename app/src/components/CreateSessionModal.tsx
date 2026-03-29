import { useState, useEffect } from "react";
import type { AdoptableSession, ChatSession, ProviderMetadata } from "../api/types";
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
  onCreateSession: (provider: string, folder: string) => Promise<void>;
  onLoadAdoptableSessions: (provider: string) => Promise<AdoptableSession[]>;
  onAdoptSession: (provider: string, threadID: string) => Promise<void>;
}

export function CreateSessionModal({
  folders,
  chatSessions,
  providers,
  onClose,
  onCreateSession,
  onLoadAdoptableSessions,
  onAdoptSession,
}: Props) {
  const providerList = listProviderIDs(providers, chatSessions).map((providerID) =>
    getProviderMetadata(providerID, providers)
  );
  const [providerID, setProviderID] = useState<string>("");
  const [mode, setMode] = useState<"new" | "adopt">("new");
  const [adoptFilter, setAdoptFilter] = useState("");
  const [adoptableSessions, setAdoptableSessions] = useState<AdoptableSession[]>([]);
  const [adoptLoading, setAdoptLoading] = useState(false);
  const [adoptError, setAdoptError] = useState<string | null>(null);

  useEffect(() => {
    if (providerList.length > 0 && !providerID) {
      setProviderID(providerList[0].id);
    }
  }, [providerList, providerID]);

  const selectedProvider =
    providerList.find((provider) => provider.id === providerID) || null;
  const canAdoptExistingSessions = Boolean(selectedProvider?.chat?.adopt_existing_sessions);

  useEffect(() => {
    setMode("new");
    setAdoptFilter("");
  }, [providerID]);

  useEffect(() => {
    if (!providerID || !canAdoptExistingSessions) {
      setAdoptableSessions([]);
      setAdoptError(null);
      setAdoptLoading(false);
      return;
    }

    let cancelled = false;
    setAdoptLoading(true);
    setAdoptError(null);

    void onLoadAdoptableSessions(providerID)
      .then((sessions) => {
        if (!cancelled) {
          setAdoptableSessions(sessions);
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
  }, [providerID, canAdoptExistingSessions]);

  const handleSelect = async (folder: string) => {
    if (!providerID) return;
    await onCreateSession(providerID, folder);
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

  return (
    <div className="modal-backdrop" onClick={onClose}>
      <div className="modal-shell" onClick={(e) => e.stopPropagation()}>
        <div className="modal-kicker">New session</div>
        <h2>Choose provider & repository</h2>
        <p>Select a chat provider and the repository to work in.</p>

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

        {canAdoptExistingSessions && (
          <div className="flex gap-2 mb-4">
            <button
              className={mode === "new" ? "is-active" : ""}
              onClick={() => setMode("new")}
            >
              New session
            </button>
            <button
              className={mode === "adopt" ? "is-active" : ""}
              onClick={() => setMode("adopt")}
            >
              Adopt existing
            </button>
          </div>
        )}

        {mode === "adopt" && canAdoptExistingSessions ? (
          <div className="space-y-2">
            <input
              type="text"
              value={adoptFilter}
              onChange={(e) => setAdoptFilter(e.target.value)}
              placeholder="Search existing Codex sessions..."
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
        ) : (
          <FolderPicker folders={folders} onSelect={handleSelect} />
        )}

        <button onClick={onClose} className="modal-cancel-button">
          Cancel
        </button>
      </div>
    </div>
  );
}
