import { useState } from "react";

interface Props {
  folders: string[];
  onSelect: (folder: string) => void;
}

export function FolderPicker({ folders, onSelect }: Props) {
  const [filter, setFilter] = useState("");

  const filtered = folders.filter((f) =>
    f.toLowerCase().includes(filter.toLowerCase())
  );

  return (
    <div className="space-y-2">
      <input
        type="text"
        value={filter}
        onChange={(e) => setFilter(e.target.value)}
        placeholder="Search repositories..."
        className="w-full rounded-lg border border-border bg-bg-input px-3 py-2.5 text-sm text-text-primary placeholder:text-text-muted outline-none focus:border-accent-blue focus:ring-2 focus:ring-accent-blue/10 transition-all"
        autoFocus
      />
      <div className="max-h-60 overflow-y-auto space-y-0.5">
        {filtered.map((folder) => (
          <button
            key={folder}
            onClick={() => onSelect(folder)}
            className="w-full text-left rounded-lg px-3 py-2.5 text-sm text-text-secondary hover:bg-bg-hover hover:text-text-primary transition-colors"
          >
            {folder}
          </button>
        ))}
        {filtered.length === 0 && (
          <div className="px-3 py-2.5 text-sm text-text-muted">No matching repositories</div>
        )}
      </div>
    </div>
  );
}
