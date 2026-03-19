import { describe, expect, it } from "vitest";

import type { Session } from "../api/types";
import { reduceSessionsState, resolveBootstrapResults, sessionsInitialState } from "./useSessions";

function makeSession(overrides: Partial<Session>): Session {
  return {
    id: "session-1",
    folder: "/tmp/repo",
    rel_name: "repo",
    status: "running",
    agent: "claude",
    started_at: "2026-03-17T12:00:00.000Z",
    restarts: 0,
    uptime: "1m",
    ...overrides,
  };
}

describe("reduceSessionsState", () => {
  it("updates only the restarted session and preserves newer session state", () => {
    const initial = {
      ...sessionsInitialState,
      sessions: [
        makeSession({ id: "session-1", status: "running", restarts: 0 }),
        makeSession({ id: "session-2", folder: "/tmp/other", rel_name: "other", status: "crashed" }),
      ],
    };

    const restarted = makeSession({
      id: "session-1",
      status: "running",
      restarts: 1,
      uptime: "0s",
    });

    const next = reduceSessionsState(initial, { type: "UPDATE_SESSION", session: restarted });

    expect(next.sessions).toHaveLength(2);
    expect(next.sessions[0]).toEqual(restarted);
    expect(next.sessions[1]).toEqual(initial.sessions[1]);
  });
});

describe("resolveBootstrapResults", () => {
  it("keeps fulfilled bootstrap data when one endpoint fails", () => {
    const results = resolveBootstrapResults([
      { status: "fulfilled", value: [makeSession({ id: "session-1" })] },
      { status: "fulfilled", value: [] },
      { status: "rejected", reason: new Error("codex unavailable") },
    ]);

    expect(results.sessions).toHaveLength(1);
    expect(results.claudeSessions).toEqual([]);
    expect(results.codexSessions).toEqual([]);
    expect(results.authRequired).toBe(false);
    expect(results.loadError).toBe("Some services failed to load: Codex: codex unavailable");
  });

  it("marks auth as required when all bootstrap requests fail with unauthorized", () => {
    const unauthorized = Object.assign(new Error("unauthorized"), { status: 401 });

    const results = resolveBootstrapResults([
      { status: "rejected", reason: unauthorized },
      { status: "rejected", reason: unauthorized },
      { status: "rejected", reason: unauthorized },
    ]);

    expect(results.authRequired).toBe(true);
    expect(results.loadError).toBeNull();
    expect(results.sessions).toEqual([]);
    expect(results.claudeSessions).toEqual([]);
    expect(results.codexSessions).toEqual([]);
  });

  it("returns a blocking load error when all bootstrap requests fail", () => {
    const results = resolveBootstrapResults([
      { status: "rejected", reason: new Error("sessions down") },
      { status: "rejected", reason: new Error("claude down") },
      { status: "rejected", reason: new Error("codex down") },
    ]);

    expect(results.authRequired).toBe(false);
    expect(results.loadError).toBe(
      "Some services failed to load: RC: sessions down; Claude: claude down; Codex: codex down"
    );
  });
});
