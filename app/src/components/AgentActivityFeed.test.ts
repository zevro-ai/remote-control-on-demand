import { describe, expect, it } from "vitest";

import type { StreamBlock } from "../api/types";
import { partitionStreamBlocks } from "./AgentActivityFeed";

describe("partitionStreamBlocks", () => {
  it("keeps earlier text blocks visible when a later tool call is still running", () => {
    const blocks: StreamBlock[] = [
      { type: "text", content: "thinking" },
      {
        type: "tool_use",
        index: 0,
        id: "tool-1",
        name: "command_execution",
        inputJSON: "pwd",
        outputText: "repo",
        done: true,
      },
      {
        type: "tool_use",
        index: 1,
        id: "tool-2",
        name: "command_execution",
        inputJSON: "ls",
        done: false,
      },
    ];

    const result = partitionStreamBlocks(blocks);

    expect(result.completedItems).toEqual([blocks[1]]);
    expect(result.activeBlocks).toEqual([blocks[0], blocks[2]]);
  });
});
