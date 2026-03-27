// @vitest-environment jsdom

import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { CreateSessionModal } from "./CreateSessionModal";

afterEach(cleanup);

describe("CreateSessionModal", () => {
  it("renders provider names separately from capability metadata", () => {
    render(
      <CreateSessionModal
        folders={["remote-control-on-demand"]}
        chatSessions={{ claude: [], codex: [] }}
        onClose={vi.fn()}
        onCreateSession={vi.fn().mockResolvedValue(undefined)}
      />
    );

    const claudeButton = screen.getByRole("button", { name: /Claude/i });
    const codexButton = screen.getByRole("button", { name: /Codex/i });

    expect(claudeButton.textContent).toBe("Claude bash • streaming");
    expect(codexButton.textContent).toBe("Codex images • bash • streaming");
    expect(claudeButton.textContent).not.toContain("Claudeimages");
    expect(codexButton.textContent).not.toContain("Codeximages");
  });
});
