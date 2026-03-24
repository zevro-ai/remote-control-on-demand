import { useState } from "react";
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
  const [agent, setAgent] = useState<string>(providers[0] || "claude");

  const handleSelect = async (folder: string) => {
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
          {providers.map((p) => (
            <button
              key={p}
              onClick={() => setAgent(p)}
              className={agent === p ? "is-active" : ""}
            >
              {p.charAt(0).toUpperCase() + p.slice(1)}
            </button>
          ))}
        </div>

        <FolderPicker folders={folders} onSelect={handleSelect} />

        <button onClick={onClose} className="modal-cancel-button">
          Cancel
        </button>
      </div>
    </div>
  );
}
