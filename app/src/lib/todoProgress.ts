import type { StreamBlock } from "../api/types";

export interface TodoProgressItem {
  text: string;
  completed: boolean;
  active: boolean;
}

export interface TodoProgressState {
  items: TodoProgressItem[];
  completedCount: number;
  totalCount: number;
}

type TodoToolBlock = Extract<StreamBlock, { type: "tool_use" }>;

interface TodoPayloadItem {
  text: string;
  completed: boolean;
}

export function isToolUseBlock(block: StreamBlock): block is TodoToolBlock {
  return block.type === "tool_use";
}

export function isTodoToolBlock(block: StreamBlock) {
  return isToolUseBlock(block) && (block.name === "TodoWrite" || block.name === "todo_list");
}

export function extractTodoProgress(blocks: StreamBlock[]): TodoProgressState | null {
  for (let index = blocks.length - 1; index >= 0; index -= 1) {
    const block = blocks[index];
    if (!block || !isToolUseBlock(block) || !isTodoToolBlock(block)) {
      continue;
    }

    const items = parseTodoItems(block.inputJSON);
    if (items.length === 0) {
      continue;
    }

    const activeIndex = items.findIndex((item) => !item.completed);
    const normalized = items.map((item, itemIndex): TodoProgressItem => ({
      text: item.text,
      completed: item.completed,
      active: activeIndex === itemIndex,
    }));

    return {
      items: normalized,
      completedCount: normalized.filter((item) => item.completed).length,
      totalCount: normalized.length,
    };
  }

  return null;
}

function parseTodoItems(inputJSON: string): TodoPayloadItem[] {
  if (!inputJSON) {
    return [];
  }

  try {
    const payload = JSON.parse(inputJSON) as {
      todos?: Array<{ text?: string; completed?: boolean }>;
      items?: Array<{ text?: string; completed?: boolean }>;
    };

    const rawItems = Array.isArray(payload.todos)
      ? payload.todos
      : Array.isArray(payload.items)
        ? payload.items
        : [];

    return rawItems
      .map((item) => ({
        text: typeof item?.text === "string" ? item.text.trim() : "",
        completed: Boolean(item?.completed),
      }))
      .filter((item) => item.text.length > 0);
  } catch {
    return [];
  }
}
