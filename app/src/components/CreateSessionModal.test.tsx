// @vitest-environment jsdom

import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { CreateSessionModal } from "./CreateSessionModal";

afterEach(cleanup);

describe("CreateSessionModal", () => {
  it("renders backend provider metadata without concatenating labels and creates a session for the selected provider", async () => {
    const onCreateSession = vi.fn().mockResolvedValue(undefined);
    const onLoadAdoptableSessions = vi.fn().mockResolvedValue([]);
    const onAdoptSession = vi.fn().mockResolvedValue(undefined);
    const onClose = vi.fn();

    render(
      <CreateSessionModal
        folders={["repo-a"]}
        chatSessions={{ codex: [], claude: [] }}
        providers={{
          codex: {
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
          },
          claude: {
            id: "claude",
            display_name: "Claude",
            chat: {
              streaming_deltas: true,
              tool_call_streaming: false,
              image_attachments: false,
              shell_command_exec: true,
              thread_resume: true,
              adopt_existing_sessions: false,
              external_url_detection: false,
            },
          },
        }}
        onClose={onClose}
        onCreateSession={onCreateSession}
        onLoadAdoptableSessions={onLoadAdoptableSessions}
        onAdoptSession={onAdoptSession}
      />
    );

    const claudeButton = screen.getByRole("button", { name: /Claude/i });
    const codexButton = screen.getByRole("button", { name: /Codex/i });

    expect(claudeButton.textContent).toBe("Claude bash · streaming");
    expect(codexButton.textContent).toBe("Codex images · bash · streaming");
    expect(claudeButton.textContent).not.toContain("Claudeimages");
    expect(codexButton.textContent).not.toContain("Codeximages");

    fireEvent.click(codexButton);
    fireEvent.click(screen.getByRole("button", { name: "repo-a" }));

    await waitFor(() => {
      expect(onCreateSession).toHaveBeenCalledWith("codex", "repo-a");
    });
    expect(onClose).toHaveBeenCalled();
  });

  it("falls back to known chat session providers when metadata is unavailable", async () => {
    const onCreateSession = vi.fn().mockResolvedValue(undefined);
    const onLoadAdoptableSessions = vi.fn().mockResolvedValue([]);
    const onAdoptSession = vi.fn().mockResolvedValue(undefined);
    const onClose = vi.fn();

    render(
      <CreateSessionModal
        folders={["repo-b"]}
        chatSessions={{
          claude: [
            {
              id: "session-1",
              folder: "/tmp/repo-b",
              rel_name: "repo-b",
              provider: "claude",
              agent: "claude",
              busy: false,
              created_at: "2026-03-27T16:00:00Z",
              updated_at: "2026-03-27T16:00:00Z",
              messages: [],
            },
          ],
        }}
        providers={{}}
        onClose={onClose}
        onCreateSession={onCreateSession}
        onLoadAdoptableSessions={onLoadAdoptableSessions}
        onAdoptSession={onAdoptSession}
      />
    );

    expect(screen.getByRole("button", { name: /Claude/i })).toBeTruthy();

    fireEvent.click(screen.getByRole("button", { name: /Claude/i }));
    fireEvent.click(screen.getByRole("button", { name: "repo-b" }));

    await waitFor(() => {
      expect(onCreateSession).toHaveBeenCalledWith("claude", "repo-b");
    });
    expect(onClose).toHaveBeenCalled();
  });

  it("loads and adopts existing sessions for providers that support adoption", async () => {
    const onCreateSession = vi.fn().mockResolvedValue(undefined);
    const onLoadAdoptableSessions = vi.fn().mockResolvedValue([
      {
        thread_id: "thread-1",
        rel_name: "repo-a",
        rel_cwd: "nested",
        title: "Existing Codex session",
        model: "gpt-5.4",
        updated_at: "2026-03-29T16:00:00Z",
      },
    ]);
    const onAdoptSession = vi.fn().mockResolvedValue(undefined);
    const onClose = vi.fn();

    render(
      <CreateSessionModal
        folders={["repo-a"]}
        chatSessions={{ codex: [] }}
        providers={{
          codex: {
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
          },
        }}
        onClose={onClose}
        onCreateSession={onCreateSession}
        onLoadAdoptableSessions={onLoadAdoptableSessions}
        onAdoptSession={onAdoptSession}
      />
    );

    await waitFor(() => {
      expect(onLoadAdoptableSessions).toHaveBeenCalledWith("codex");
    });

    fireEvent.click(screen.getByRole("button", { name: /Adopt existing/i }));
    fireEvent.click(screen.getByRole("button", { name: /Existing Codex session/i }));

    await waitFor(() => {
      expect(onAdoptSession).toHaveBeenCalledWith("codex", "thread-1");
    });
    expect(onClose).toHaveBeenCalled();
  });
});
