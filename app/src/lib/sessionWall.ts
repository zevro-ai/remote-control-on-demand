import type { ChatSession, StreamBlock } from "../api/types";
import { getProviderDisplayName, getSessionProviderID } from "./providers";
import { isTodoToolBlock, isToolUseBlock } from "./todoProgress";

export type { ChatSession };
export type OverviewDensity = "compact" | "comfortable" | "focus";
export type PreviewTone = "user" | "assistant" | "tool" | "system";
export interface FocusTarget {
  sessionId: string;
  type: string;
}

export interface PreviewLine {
  text: string;
  tone: PreviewTone;
}

export interface WallSlot {
  paneLabel: string;
  session: ChatSession | null;
}

const DENSITY_LINE_COUNT: Record<OverviewDensity, number> = {
  compact: 6,
  comfortable: 8,
  focus: 10,
};

const DENSITY_MIN_SLOTS: Record<OverviewDensity, number> = {
  compact: 6,
  comfortable: 4,
  focus: 2,
};

const DENSITY_PANE_COLUMNS: Record<OverviewDensity, number> = {
  compact: 3,
  comfortable: 2,
  focus: 2,
};

const MAX_PREVIEW_MESSAGES = 4;
const MAX_TERMINAL_WIDTH = 96;

export function getPreviewLineCount(density: OverviewDensity) {
  return DENSITY_LINE_COUNT[density];
}

export function sortSessionsForWall(sessions: ChatSession[]) {
  return [...sessions].sort((left, right) => {
    if (left.busy !== right.busy) {
      return left.busy ? -1 : 1;
    }

    const rightUpdated = getSessionTimestamp(right);
    const leftUpdated = getSessionTimestamp(left);
    if (rightUpdated !== leftUpdated) {
      return rightUpdated - leftUpdated;
    }

    const rightMessages = right.messages?.length || 0;
    const leftMessages = left.messages?.length || 0;
    if (rightMessages !== leftMessages) {
      return rightMessages - leftMessages;
    }

    return left.rel_name.localeCompare(right.rel_name);
  });
}

export function buildWallSlots(
  sessions: ChatSession[],
  density: OverviewDensity
): WallSlot[] {
  const slotCount = Math.max(sessions.length, DENSITY_MIN_SLOTS[density]);
  const columns = DENSITY_PANE_COLUMNS[density];

  return Array.from({ length: slotCount }, (_, index) => ({
    paneLabel: formatPaneLabel(index, columns),
    session: sessions[index] || null,
  }));
}

export function resolveFocusedSession(
  focusedPanel: FocusTarget | null,
  allSessions: ChatSession[]
) {
  if (!focusedPanel) {
    return null;
  }

  return allSessions.find((session) => (
    session.id === focusedPanel.sessionId &&
    getSessionProviderID(session) === focusedPanel.type
  )) || null;
}

export function buildSessionPreview(
  session: ChatSession,
  streamBlocks: StreamBlock[],
  lineCount: number
): PreviewLine[] {
  const preview: PreviewLine[] = [];

  for (const message of (session.messages || []).slice(-MAX_PREVIEW_MESSAGES)) {
    if (message.kind === "bash" && message.command) {
      pushPreview(preview, "tool", `$ ${message.command.command}`);
      continue;
    }

    if (message.kind === "bash_result" && message.command) {
      pushPreview(
        preview,
        message.command.exit_code === 0 ? "assistant" : "system",
        `exit ${message.command.exit_code} :: ${message.command.command}`
      );
      pushPreview(preview, "assistant", message.content);
      continue;
    }

    if (message.role === "user") {
      pushPreview(preview, "user", message.content, "> ");
      continue;
    }

    for (const block of message.blocks || []) {
      if (block.type === "tool_use") {
        if (isTodoToolBlock(block)) {
          continue;
        }
        pushPreview(preview, "tool", formatToolLine(block.name, block.done));
      }
    }

    pushPreview(preview, "assistant", message.content);
  }

  for (const block of streamBlocks) {
    if (isToolUseBlock(block)) {
      if (isTodoToolBlock(block)) {
        continue;
      }
      pushPreview(preview, "tool", formatToolLine(block.name, block.done));
      continue;
    }

    pushPreview(preview, "assistant", block.content);
  }

  if (session.busy && streamBlocks.length === 0) {
    pushPreview(preview, "system", `${getProviderDisplayName(session)} is thinking...`);
  }

  if (preview.length === 0) {
    return [{ tone: "system", text: "awaiting first prompt" }];
  }

  return preview.slice(-lineCount);
}

function formatPaneLabel(index: number, columns: number) {
  const row = Math.floor(index / columns);
  const col = (index % columns) + 1;
  const rowLabel = String.fromCharCode(65 + row);
  return `${rowLabel}${col}`;
}

function pushPreview(target: PreviewLine[], tone: PreviewTone, value: string, prefix = "") {
  const normalized = value.replace(/\r/g, "").trim();
  if (!normalized) {
    return;
  }

  normalized.split("\n").forEach((line, index) => {
    const trimmed = line.trim();
    if (!trimmed) {
      return;
    }
    const leading = index === 0 ? prefix : prefix ? "  " : "";
    target.push({
      tone,
      text: truncate(`${leading}${trimmed}`),
    });
  });
}

function formatToolLine(name: string, done: boolean) {
  return `$ ${name || "tool"}${done ? "" : " ..."}`;
}

function truncate(value: string) {
  if (value.length <= MAX_TERMINAL_WIDTH) {
    return value;
  }
  return `${value.slice(0, MAX_TERMINAL_WIDTH - 1)}…`;
}

function getSessionTimestamp(session: ChatSession) {
  const parsed = Date.parse(session.updated_at || session.created_at);
  return Number.isNaN(parsed) ? 0 : parsed;
}
