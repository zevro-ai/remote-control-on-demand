import type { AgentType } from "../api/types";

const styles: Record<AgentType, string> = {
  claude: "border border-accent-blue/20 bg-accent-blue/10 text-accent-blue",
  codex: "border border-accent-orange/20 bg-accent-orange/10 text-accent-orange",
};

export function AgentBadge({ agent }: { agent: AgentType }) {
  return (
    <span
      className={`inline-flex items-center rounded-full px-2.5 py-0.5 text-xs font-medium ${styles[agent]}`}
    >
      {agent}
    </span>
  );
}
