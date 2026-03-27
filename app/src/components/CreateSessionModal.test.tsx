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
});
