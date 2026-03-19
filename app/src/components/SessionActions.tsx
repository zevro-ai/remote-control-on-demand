import type { Session } from "../api/types";

interface Props {
  session: Session;
  onKill: (id: string) => void;
  onRestart: (id: string) => void;
}

export function SessionActions({ session, onKill, onRestart }: Props) {
  return (
    <div className="flex gap-2">
      {session.status === "running" && (
        <>
          <button
            onClick={() => onRestart(session.id)}
            className="rounded-md bg-accent-orange/15 px-3 py-1.5 text-xs font-medium text-accent-orange hover:bg-accent-orange/25 transition-colors"
          >
            Restart
          </button>
          <button
            onClick={() => onKill(session.id)}
            className="rounded-md bg-accent-red/15 px-3 py-1.5 text-xs font-medium text-accent-red hover:bg-accent-red/25 transition-colors"
          >
            Stop
          </button>
        </>
      )}
      {session.status !== "running" && (
        <button
          onClick={() => onRestart(session.id)}
          className="rounded-md bg-accent-green/15 px-3 py-1.5 text-xs font-medium text-accent-green hover:bg-accent-green/25 transition-colors"
        >
          Start
        </button>
      )}
      {session.url && (
        <a
          href={session.url}
          target="_blank"
          rel="noopener noreferrer"
          className="rounded-md bg-accent-blue/15 px-3 py-1.5 text-xs font-medium text-accent-blue hover:bg-accent-blue/25 transition-colors"
        >
          Open
        </a>
      )}
    </div>
  );
}
