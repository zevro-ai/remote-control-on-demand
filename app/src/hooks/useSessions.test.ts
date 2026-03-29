import { describe, expect, it } from "vitest";

import type { ProviderMetadata, Session } from "../api/types";
import {
  normalizeProviders,
  reduceSessionsState,
  resolveBootstrapResults,
  resolveProviderBootstrapFailure,
  sessionsInitialState,
} from "./useSessions";

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
  const providers = ["claude", "codex"];

  it("keeps fulfilled bootstrap data when one endpoint fails", () => {
    const results = resolveBootstrapResults(providers, [
      { status: "fulfilled", value: [makeSession({ id: "session-1" })] },
      { status: "fulfilled", value: { provider: "claude", sessions: [] } },
      { status: "rejected", reason: new Error("codex unavailable") },
    ]);

    expect(results.sessions).toHaveLength(1);
    expect(results.chatSessions["claude"]).toEqual([]);
    expect(results.chatSessions["codex"]).toBeUndefined();
    expect(results.authRequired).toBe(false);
    expect(results.loadError).toBe("Some services failed to load: Codex: codex unavailable");
  });

  it("marks auth as required when all bootstrap requests fail with unauthorized", () => {
    const unauthorized = Object.assign(new Error("unauthorized"), { status: 401 });

    const results = resolveBootstrapResults(providers, [
      { status: "rejected", reason: unauthorized },
      { status: "rejected", reason: unauthorized },
      { status: "rejected", reason: unauthorized },
    ]);

    expect(results.authRequired).toBe(true);
    expect(results.loadError).toBeNull();
    expect(results.sessions).toEqual([]);
    expect(results.chatSessions).toEqual({});
  });

  it("returns a blocking load error when all bootstrap requests fail", () => {
    const results = resolveBootstrapResults(providers, [
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

describe("normalizeProviders", () => {
  it("preserves provider metadata objects from the backend", () => {
    const metadata: ProviderMetadata = {
      id: "codex",
      display_name: "Codex",
      chat: {
        streaming_deltas: true,
        tool_call_streaming: true,
        image_attachments: true,
        shell_command_exec: true,
        thread_resume: true,
        adopt_existing_sessions: true,
        external_url_detection: false,
      },
    };

    expect(normalizeProviders([metadata])).toEqual([metadata]);
  });

  it("normalizes legacy provider id arrays into metadata objects", () => {
    expect(normalizeProviders(["claude", "codex"])).toEqual([
      { id: "claude", display_name: "Claude" },
      { id: "codex", display_name: "Codex" },
    ]);
  });
});

describe("resolveProviderBootstrapFailure", () => {
  it("preserves runtime sessions and surfaces a load error for provider bootstrap failures", () => {
    const result = resolveProviderBootstrapFailure(
      Object.assign(new Error("not found"), { status: 404 }),
      { status: "fulfilled", value: [makeSession({ id: "session-1" })] }
    );

    expect(result.providers).toEqual({});
    expect(result.sessions).toHaveLength(1);
    expect(result.chatSessions).toEqual({});
    expect(result.authRequired).toBe(false);
    expect(result.loadError).toBe("Some services failed to load: Providers: not found");
  });

  it("marks auth as required when provider bootstrap and runtime sessions both fail unauthorized", () => {
    const unauthorized = Object.assign(new Error("unauthorized"), { status: 401 });

    const result = resolveProviderBootstrapFailure(unauthorized, {
      status: "rejected",
      reason: unauthorized,
    });

    expect(result.authRequired).toBe(true);
    expect(result.loadError).toBeNull();
    expect(result.sessions).toEqual([]);
    expect(result.chatSessions).toEqual({});
  });
});
