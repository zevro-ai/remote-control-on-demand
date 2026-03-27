import type { ChatSession, ProviderMetadata, Session } from "../api/types";

export type ProviderRecord = Record<string, ProviderMetadata>;

type ProviderScoped =
  | string
  | ProviderMetadata
  | {
      agent?: string;
      provider?: string;
      provider_meta?: ProviderMetadata;
    };

export function getProviderId(value: ProviderScoped) {
  if (typeof value === "string") {
    return value;
  }
  if ("display_name" in value) {
    return value.id;
  }
  return value.provider || value.agent || "";
}

export function getProviderMetadata(value: ProviderScoped, providers: ProviderRecord = {}) {
  if (typeof value !== "string" && "display_name" in value) {
    return value;
  }

  if (typeof value !== "string" && "provider_meta" in value && value.provider_meta) {
    return value.provider_meta;
  }

  const providerID = getProviderId(value);
  return providers[providerID] || fallbackProviderMetadata(providerID);
}

export function getProviderDisplayName(value: ProviderScoped, providers: ProviderRecord = {}) {
  const metadata = getProviderMetadata(value, providers);
  return metadata.display_name || formatProviderID(metadata.id);
}

export function getProviderChatCapabilities(value: ProviderScoped, providers: ProviderRecord = {}) {
  return getProviderMetadata(value, providers).chat;
}

export function providerSupportsImages(value: ProviderScoped, providers: ProviderRecord = {}) {
  const capabilities = getProviderChatCapabilities(value, providers);
  if (capabilities) {
    return Boolean(capabilities.image_attachments);
  }

  return getProviderId(value) === "codex";
}

export function providerSupportsBash(value: ProviderScoped, providers: ProviderRecord = {}) {
  const capabilities = getProviderChatCapabilities(value, providers);
  if (capabilities) {
    return Boolean(capabilities.shell_command_exec);
  }

  return true;
}

export function listProviderIDs(providers: ProviderRecord, chatSessions: Record<string, ChatSession[]>) {
  return Array.from(new Set([...Object.keys(providers), ...Object.keys(chatSessions)])).sort((left, right) =>
    getProviderDisplayName(left, providers).localeCompare(getProviderDisplayName(right, providers))
  );
}

export function getProviderSessions(providerID: string, chatSessions: Record<string, ChatSession[]>) {
  return chatSessions[providerID] || [];
}

export function getSessionProviderID(session: Pick<Session | ChatSession, "provider" | "agent">) {
  return session.provider || session.agent;
}

export function summarizeProviderCapabilities(provider: ProviderMetadata) {
  const capabilities = provider.chat;
  if (!capabilities) {
    return "Chat provider";
  }

  const labels: string[] = [];
  if (capabilities.image_attachments) {
    labels.push("images");
  }
  if (capabilities.shell_command_exec) {
    labels.push("bash");
  }
  if (capabilities.streaming_deltas) {
    labels.push("streaming");
  }

  return labels.length > 0 ? labels.join(" · ") : "Chat provider";
}

function fallbackProviderMetadata(providerID: string): ProviderMetadata {
  return {
    id: providerID,
    display_name: formatProviderID(providerID),
  };
}

function formatProviderID(providerID: string) {
  return providerID
    .split(/[-_]/g)
    .filter(Boolean)
    .map((part) => part.charAt(0).toUpperCase() + part.slice(1))
    .join(" ");
}
