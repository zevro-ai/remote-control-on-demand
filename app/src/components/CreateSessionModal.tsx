import { useState, useEffect } from "react";
import type { ProviderMetadata } from "../api/types";
import { getProviderDisplayName, summarizeProviderCapabilities } from "../lib/providers";
import { FolderPicker } from "./FolderPicker";

interface Props {
  folders: string[];
  providers: Record<string, ProviderMetadata>;
  onClose: () => void;
  onCreateSession: (provider: string, folder: string) => Promise<void>;
}

export function CreateSessionModal({
  folders,
  providers,
  onClose,
  onCreateSession,
}: Props) {
  const providerList = Object.values(providers).sort((left, right) =>
    left.display_name.localeCompare(right.display_name)
  );
  const [providerID, setProviderID] = useState<string>("");

  useEffect(() => {
    if (providerList.length > 0 && !providerID) {
      setProviderID(providerList[0].id);
    }
  }, [providerList, providerID]);

  const handleSelect = async (folder: string) => {
    if (!providerID) return;
    await onCreateSession(providerID, folder);
    onClose();
  };

  return (
    <div className="modal-backdrop" onClick={onClose}>
      <div className="modal-shell" onClick={(e) => e.stopPropagation()}>
        <div className="modal-kicker">New session</div>
        <h2>Choose provider & repository</h2>
        <p>Select a chat provider and the repository to work in.</p>

        <div className="modal-agent-switch">
          {providerList.length === 0 ? (
            <div className="sidebar-empty">Loading providers...</div>
          ) : (
            providerList.map((provider) => (
              <button
                key={provider.id}
                onClick={() => setProviderID(provider.id)}
                className={providerID === provider.id ? "is-active" : ""}
              >
                <strong>{getProviderDisplayName(provider, providers)}</strong>
                <span>{summarizeProviderCapabilities(provider)}</span>
              </button>
            ))
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
