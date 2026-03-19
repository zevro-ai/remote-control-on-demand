import type { SessionStatus } from "../api/types";

const colors: Record<SessionStatus | "default", string> = {
  running: "bg-accent-green",
  stopped: "bg-text-muted",
  crashed: "bg-accent-red",
  default: "bg-text-muted",
};

export function StatusDot({ status }: { status: SessionStatus }) {
  const color = colors[status] || colors.default;
  return (
    <span className="relative inline-flex h-2.5 w-2.5">
      {status === "running" && (
        <span
          className={`absolute inline-flex h-full w-full animate-ping rounded-full ${color} opacity-75`}
        />
      )}
      <span className={`relative inline-flex h-2.5 w-2.5 rounded-full ${color}`} />
    </span>
  );
}
