import { useState } from "react";
import type { AgentType } from "../api/types";
import { FolderPicker } from "./FolderPicker";

interface Props {
  folders: string[];
  onClose: () => void;
  onCreateClaude: (folder: string) => Promise<void>;
  onCreateCodex: (folder: string) => Promise<void>;
}

export function CreateSessionModal({
  folders,
  onClose,
  onCreateClaude,
  onCreateCodex,
}: Props) {
  const [agent, setAgent] = useState<AgentType>("claude");

  const handleSelect = async (folder: string) => {
    if (agent === "claude") {
      await onCreateClaude(folder);
    } else {
      await onCreateCodex(folder);
    }
    onClose();
  };

  return (
    <div className="modal-backdrop" onClick={onClose}>
      <div className="modal-shell" onClick={(e) => e.stopPropagation()}>
        <div className="modal-kicker">New session</div>
        <h2>Choose agent & repository</h2>
        <p>Select an AI agent and the repository to work in.</p>

        <div className="modal-agent-switch">
          <button
            onClick={() => setAgent("claude")}
            className={agent === "claude" ? "is-active" : ""}
          >
            Claude
          </button>
          <button
            onClick={() => setAgent("codex")}
            className={agent === "codex" ? "is-active" : ""}
          >
            Codex
          </button>
        </div>

        <FolderPicker folders={folders} onSelect={handleSelect} />

        <button onClick={onClose} className="modal-cancel-button">
          Cancel
        </button>
      </div>
    </div>
  );
}
