import { describe, expect, it } from "vitest";
import {
  buildWallSlots,
  buildSessionPreview,
  getPreviewLineCount,
  resolveFocusedSession,
  sortSessionsForWall,
  type ChatSession,
} from "./sessionWall";
import type { StreamBlock } from "../api/types";

describe("sortSessionsForWall", () => {
  it("puts live sessions ahead of idle sessions and sorts by recency inside each group", () => {
    const sessions: ChatSession[] = [
      createSession({ id: "older-idle", updated_at: "2026-03-17T11:00:00Z" }),
      createSession({ id: "live", busy: true, updated_at: "2026-03-17T09:00:00Z" }),
      createSession({ id: "newer-idle", updated_at: "2026-03-17T12:00:00Z" }),
    ];

    expect(sortSessionsForWall(sessions).map((session) => session.id)).toEqual([
      "live",
      "newer-idle",
      "older-idle",
    ]);
  });
});

describe("buildSessionPreview", () => {
  it("keeps the newest terminal lines from recent messages", () => {
    const preview = buildSessionPreview(
      createSession({
        messages: [
          { role: "user", content: "first prompt", timestamp: "2026-03-17T10:00:00Z" },
          {
            role: "assistant",
            content: "line one\nline two",
            timestamp: "2026-03-17T10:01:00Z",
          },
          { role: "user", content: "second prompt", timestamp: "2026-03-17T10:02:00Z" },
        ],
      }),
      [],
      3
    );

    expect(preview).toEqual([
      { tone: "assistant", text: "line one" },
      { tone: "assistant", text: "line two" },
      { tone: "user", text: "> second prompt" },
    ]);
  });

  it("shows tool activity and live streaming text when the agent is busy", () => {
    const streamBlocks: StreamBlock[] = [
      { type: "tool_use", index: 0, id: "tool-1", name: "search_query", inputJSON: "{}", done: false },
      { type: "text", content: "streamed answer" },
    ];

    const preview = buildSessionPreview(createSession({ busy: true }), streamBlocks, 4);

    expect(preview).toEqual([
      { tone: "tool", text: "$ search_query ..." },
      { tone: "assistant", text: "streamed answer" },
    ]);
  });

  it("skips todo progress blocks in the terminal preview", () => {
    const streamBlocks: StreamBlock[] = [
      {
        type: "tool_use",
        index: 0,
        id: "todo-1",
        name: "TodoWrite",
        inputJSON: `{"todos":[{"text":"Inspect config","completed":true},{"text":"Write tests","completed":false}]}`,
        done: false,
      },
      { type: "text", content: "streamed answer" },
    ];

    const preview = buildSessionPreview(createSession({ busy: true }), streamBlocks, 4);

    expect(preview).toEqual([
      { tone: "assistant", text: "streamed answer" },
    ]);
  });

  it("falls back to an empty terminal line when nothing happened yet", () => {
    expect(buildSessionPreview(createSession(), [], getPreviewLineCount("compact"))).toEqual([
      { tone: "system", text: "awaiting first prompt" },
    ]);
  });

  it("uses the provider display name in the busy fallback line", () => {
    expect(
      buildSessionPreview(
        createSession({
          busy: true,
          provider_meta: { id: "claude", display_name: "Claude" },
          provider: "claude",
          agent: "claude",
        }),
        [],
        getPreviewLineCount("compact")
      )
    ).toEqual([{ tone: "system", text: "Claude is thinking..." }]);
  });
});

describe("buildWallSlots", () => {
  it("fills empty panes up to the tmux wall minimum for the selected density", () => {
    const slots = buildWallSlots(
      [createSession({ id: "one" }), createSession({ id: "two" })],
      "comfortable"
    );

    expect(slots).toEqual([
      { paneLabel: "A1", session: expect.objectContaining({ id: "one" }) },
      { paneLabel: "A2", session: expect.objectContaining({ id: "two" }) },
      { paneLabel: "B1", session: null },
      { paneLabel: "B2", session: null },
    ]);
  });

  it("extends pane labels beyond the minimum when more sessions are open", () => {
    const slots = buildWallSlots(
      [
        createSession({ id: "one" }),
        createSession({ id: "two" }),
        createSession({ id: "three" }),
        createSession({ id: "four" }),
        createSession({ id: "five" }),
      ],
      "compact"
    );

    expect(slots.map((slot) => slot.paneLabel)).toEqual([
      "A1",
      "A2",
      "A3",
      "B1",
      "B2",
      "B3",
    ]);
    expect(slots[4]?.session).toEqual(expect.objectContaining({ id: "five" }));
    expect(slots[5]?.session).toBeNull();
  });
});

describe("resolveFocusedSession", () => {
  it("returns the selected provider session when it exists", () => {
    const claudeSession = createSession({ id: "claude-1", provider: "claude", agent: "claude" });
    const codexSession = createSession({ id: "codex-1", provider: "codex", agent: "codex" });

    expect(
      resolveFocusedSession(
        { sessionId: "claude-1", type: "claude" },
        [claudeSession, codexSession]
      )
    ).toEqual(claudeSession);

    expect(
      resolveFocusedSession(
        { sessionId: "codex-1", type: "codex" },
        [claudeSession, codexSession]
      )
    ).toEqual(codexSession);
  });

  it("returns null when the focused session is gone", () => {
    expect(resolveFocusedSession({ sessionId: "missing", type: "codex" }, [])).toBeNull();
  });
});

function createSession(overrides: Partial<ChatSession> = {}): ChatSession {
  return {
    id: "session-1",
    folder: "/tmp/repo",
    rel_name: "repo",
    provider: "codex",
    provider_meta: { id: "codex", display_name: "Codex" },
    agent: "codex",
    busy: false,
    created_at: "2026-03-17T09:00:00Z",
    updated_at: "2026-03-17T09:00:00Z",
    messages: [],
    ...overrides,
  };
}
