import { useState, useEffect } from "react";
import { FolderPicker } from "./FolderPicker";

interface Props {
  folders: string[];
  chatSessions: Record<string, any>;
  onClose: () => void;
  onCreateSession: (provider: string, folder: string) => Promise<void>;
}

export function CreateSessionModal({
  folders,
  chatSessions,
  onClose,
  onCreateSession,
}: Props) {
  const providers = Object.keys(chatSessions).sort();
  const [agent, setAgent] = useState<string>("");

  useEffect(() => {
    if (providers.length > 0 && !agent) {
      setAgent(providers[0]);
    }
  }, [providers, agent]);

  const handleSelect = async (folder: string) => {
    if (!agent) return;
    await onCreateSession(agent, folder);
    onClose();
  };

  return (
    <div className="modal-backdrop" onClick={onClose}>
      <div className="modal-shell" onClick={(e) => e.stopPropagation()}>
        <div className="modal-kicker">New session</div>
        <h2>Choose agent & repository</h2>
        <p>Select an AI agent and the repository to work in.</p>

        <div className="modal-agent-switch">
          {providers.length === 0 ? (
            <div className="sidebar-empty">Loading agents...</div>
          ) : (
            providers.map((p) => {
              const providerMeta = getProviderMeta(p);
              return (
                <button
                  key={p}
                  onClick={() => setAgent(p)}
                  className={agent === p ? "is-active" : ""}
                >
                  <span className="modal-agent-switch__name">{providerMeta.name}</span>
                  {providerMeta.capabilities.length > 0 && (
                    <>
                      {" "}
                      <span className="modal-agent-switch__meta">
                        {providerMeta.capabilities.join(" • ")}
                      </span>
                    </>
                  )}
                </button>
              );
            })
          )}
        </div>

        <FolderPicker folders={folders} onSelect={handleSelect} />

        <button onClick={onClose} className="modal-cancel-button">
          Cancel
        </button>
      </div>
    </div>
  );
}

function getProviderMeta(provider: string) {
  switch (provider) {
    case "claude":
      return {
        name: "Claude",
        capabilities: ["bash", "streaming"],
      };
    case "codex":
      return {
        name: "Codex",
        capabilities: ["images", "bash", "streaming"],
      };
    case "gemini":
      return {
        name: "Gemini",
        capabilities: ["streaming"],
      };
    default:
      return {
        name: provider.charAt(0).toUpperCase() + provider.slice(1),
        capabilities: [],
      };
  }
}
