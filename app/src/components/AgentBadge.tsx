const styles: Record<string, string> = {
  claude: "border border-accent-blue/20 bg-accent-blue/10 text-accent-blue",
  codex: "border border-accent-orange/20 bg-accent-orange/10 text-accent-orange",
};

export function AgentBadge({ agent, label }: { agent: string; label?: string }) {
  return (
    <span
      className={`inline-flex items-center rounded-full px-2.5 py-0.5 text-xs font-medium ${
        styles[agent] || "border border-gray-500/20 bg-gray-500/10 text-gray-400"
      }`}
    >
      {label || agent}
    </span>
  );
}
