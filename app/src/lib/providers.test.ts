import { describe, expect, it } from "vitest";

import type { ChatSession, ProviderMetadata } from "../api/types";
import {
  getProviderDisplayName,
  getProviderId,
  getProviderMetadata,
  getSessionProviderID,
  listProviderIDs,
  providerSupportsBash,
  providerSupportsImages,
  summarizeProviderCapabilities,
} from "./providers";

const codexMetadata: ProviderMetadata = {
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
};

describe("providers helpers", () => {
  it("reads direct provider metadata objects", () => {
    expect(getProviderId(codexMetadata)).toBe("codex");
    expect(getProviderMetadata(codexMetadata, {})).toEqual(codexMetadata);
    expect(getProviderDisplayName(codexMetadata, {})).toBe("Codex");
  });

  it("prefers inline session metadata when present", () => {
    const session: ChatSession = {
      id: "session-1",
      folder: "/tmp/repo",
      rel_name: "repo",
      provider: "codex",
      provider_meta: codexMetadata,
      agent: "codex",
      busy: false,
      created_at: "2026-03-17T09:00:00Z",
      updated_at: "2026-03-17T09:00:00Z",
      messages: [],
    };

    expect(getProviderId(session)).toBe("codex");
    expect(getSessionProviderID(session)).toBe("codex");
    expect(getProviderMetadata(session, {})).toEqual(codexMetadata);
    expect(getProviderDisplayName(session, {})).toBe("Codex");
    expect(providerSupportsImages(session, {})).toBe(true);
    expect(providerSupportsBash(session, {})).toBe(true);
  });

  it("falls back to registry metadata and formatted ids", () => {
    expect(getProviderDisplayName("open_code", {})).toBe("Open Code");
    expect(getProviderDisplayName("claude", {
      claude: {
        id: "claude",
        display_name: "Claude",
      },
    })).toBe("Claude");
  });

  it("preserves legacy capability defaults when chat metadata is missing", () => {
    expect(providerSupportsBash("claude", {})).toBe(true);
    expect(providerSupportsImages("claude", {})).toBe(false);
    expect(providerSupportsBash("codex", {})).toBe(true);
    expect(providerSupportsImages("codex", {})).toBe(true);
  });

  it("sorts provider ids by display name across registry and live sessions", () => {
    const providers = {
      codex: codexMetadata,
      claude: { id: "claude", display_name: "Claude" },
    };

    expect(listProviderIDs(providers, { gemini: [] })).toEqual(["claude", "codex", "gemini"]);
  });

  it("summarizes visible chat capabilities", () => {
    expect(summarizeProviderCapabilities(codexMetadata)).toBe("images · bash · streaming");
  });
});
