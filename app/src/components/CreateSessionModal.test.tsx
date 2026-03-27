// @vitest-environment jsdom

import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { CreateSessionModal } from "./CreateSessionModal";

afterEach(cleanup);

describe("CreateSessionModal", () => {
  it("renders backend provider metadata and creates a session for the selected provider", async () => {
    const onCreateSession = vi.fn().mockResolvedValue(undefined);
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
              external_url_detection: false,
            },
          },
        }}
        onClose={onClose}
        onCreateSession={onCreateSession}
      />
    );

    expect(screen.getByRole("button", { name: /Claude/i })).toBeTruthy();
    expect(screen.getByRole("button", { name: /Codex/i })).toBeTruthy();

    fireEvent.click(screen.getByRole("button", { name: /Codex/i }));
    fireEvent.click(screen.getByRole("button", { name: "repo-a" }));

    await waitFor(() => {
      expect(onCreateSession).toHaveBeenCalledWith("codex", "repo-a");
    });
    expect(onClose).toHaveBeenCalled();
  });

  it("falls back to known chat session providers when metadata is unavailable", async () => {
    const onCreateSession = vi.fn().mockResolvedValue(undefined);
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
});
