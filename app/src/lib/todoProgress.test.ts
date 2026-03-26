import { describe, expect, it } from "vitest";
import type { StreamBlock } from "../api/types";
import { extractTodoProgress, isTodoToolBlock } from "./todoProgress";

describe("isTodoToolBlock", () => {
  it("recognizes TodoWrite blocks", () => {
    const block: StreamBlock = {
      type: "tool_use",
      index: 1,
      id: "todo-1",
      name: "TodoWrite",
      inputJSON: `{"todos":[{"text":"Inspect","completed":true}]}`,
      done: false,
    };

    expect(isTodoToolBlock(block)).toBe(true);
  });
});

describe("extractTodoProgress", () => {
  it("returns the latest todo list state and infers the active item", () => {
    const blocks: StreamBlock[] = [
      {
        type: "tool_use",
        index: 1,
        id: "todo-1",
        name: "TodoWrite",
        inputJSON: `{"todos":[{"text":"Inspect config","completed":false}]}`,
        done: true,
      },
      {
        type: "tool_use",
        index: 2,
        id: "todo-2",
        name: "TodoWrite",
        inputJSON: `{"todos":[{"text":"Inspect config","completed":true},{"text":"Write tests","completed":false},{"text":"Update docs","completed":false}]}`,
        done: false,
      },
    ];

    expect(extractTodoProgress(blocks)).toEqual({
      completedCount: 1,
      totalCount: 3,
      items: [
        { text: "Inspect config", completed: true, active: false },
        { text: "Write tests", completed: false, active: true },
        { text: "Update docs", completed: false, active: false },
      ],
    });
  });

  it("supports raw todo_list payloads from Codex", () => {
    const blocks: StreamBlock[] = [
      {
        type: "tool_use",
        index: 7,
        id: "todo-7",
        name: "todo_list",
        inputJSON: `{"items":[{"text":"Read config","completed":true},{"text":"Patch UI","completed":false}]}`,
        done: false,
      },
    ];

    expect(extractTodoProgress(blocks)).toEqual({
      completedCount: 1,
      totalCount: 2,
      items: [
        { text: "Read config", completed: true, active: false },
        { text: "Patch UI", completed: false, active: true },
      ],
    });
  });

  it("returns null for invalid or empty todo payloads", () => {
    const blocks: StreamBlock[] = [
      {
        type: "tool_use",
        index: 1,
        id: "todo-1",
        name: "TodoWrite",
        inputJSON: `{"todos":[]}`,
        done: true,
      },
    ];

    expect(extractTodoProgress(blocks)).toBeNull();
  });
});
