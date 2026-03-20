import { useState, useMemo } from "react";

const TOOL_COLORS: Record<string, string> = {
  Write: "var(--color-accent-cyan)",
  Edit: "var(--color-accent-purple)",
  Read: "var(--color-accent-blue)",
  Bash: "var(--color-accent-orange)",
  Glob: "var(--color-accent-green)",
  Grep: "var(--color-accent-green)",
};

interface Props {
  name: string;
  id: string;
  inputJSON: string;
  done: boolean;
  live?: boolean;
}

export function ToolCallBlock({ name, inputJSON, done, live }: Props) {
  const [expanded, setExpanded] = useState(false);
  const color = TOOL_COLORS[name] || "var(--color-text-secondary)";

  const parsed = useMemo(() => {
    try {
      return JSON.parse(inputJSON);
    } catch {
      return null;
    }
  }, [inputJSON]);

  const target = useMemo(() => {
    if (!parsed) return inputJSON || "";
    if (parsed.file_path) return parsed.file_path;
    if (parsed.command) return parsed.command;
    if (parsed.pattern) return parsed.pattern;
    if (parsed.path) return parsed.path;
    if (parsed.description) return parsed.description;
    return "";
  }, [parsed, inputJSON]);

  const isActive = live && !done;

  return (
    <div className={`tool-call-block ${isActive ? "is-active" : ""}`}>
      <button
        className="tool-call-block__header"
        onClick={() => setExpanded(!expanded)}
      >
        <span
          className="tool-call-block__name"
          style={{ color, borderColor: color, background: `color-mix(in srgb, ${color} 10%, transparent)` }}
        >
          {name}
        </span>
        <span className="tool-call-block__target">{target}</span>
        <span className="tool-call-block__status">
          {isActive ? (
            <span className="tool-spinner" />
          ) : done ? (
            <span style={{ color: "var(--color-accent-green)" }}>&#10003;</span>
          ) : null}
        </span>
        <span className="tool-call-block__chevron" style={{ color: "var(--color-text-muted)", fontSize: "0.7rem" }}>
          {expanded ? "\u25BC" : "\u25B6"}
        </span>
      </button>
      {expanded && inputJSON && (
        <div className="tool-call-block__body">
          <pre>{parsed ? JSON.stringify(parsed, null, 2) : inputJSON}</pre>
        </div>
      )}
    </div>
  );
}
