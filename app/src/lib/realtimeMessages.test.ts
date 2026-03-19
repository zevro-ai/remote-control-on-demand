import { describe, expect, it } from "vitest";

import type { Message } from "../api/types";
import { mergeIncomingMessage, removeOptimisticMessage } from "./realtimeMessages";

describe("mergeIncomingMessage", () => {
  it("replaces the last optimistic user message with the realtime version", () => {
    const existing: Message[] = [
      {
        role: "user",
        kind: "text",
        content: "describe this",
        timestamp: "2026-03-17T12:00:00.000Z",
        optimistic: true,
        optimistic_id: "optimistic-1",
        attachments: [
          {
            id: "pending-1",
            name: "wall.png",
            content_type: "image/png",
            size: 123,
          },
        ],
      },
    ];

    const merged = mergeIncomingMessage(existing, {
      role: "user",
      kind: "text",
      content: "describe this",
      timestamp: "2026-03-17T12:00:01.000Z",
      attachments: [
        {
          id: "real-1",
          name: "wall.png",
          content_type: "image/png",
          size: 123,
          url: "/api/uploads/wall.png",
        },
      ],
    });

    expect(merged).toHaveLength(1);
    expect(merged[0].optimistic).toBeUndefined();
    expect(merged[0].attachments?.[0].url).toBe("/api/uploads/wall.png");
  });

  it("appends realtime messages when the last message is not optimistic", () => {
    const existing: Message[] = [
      {
        role: "user",
        kind: "bash",
        content: "pwd",
        timestamp: "2026-03-17T12:00:00.000Z",
        command: { command: "pwd" },
      },
    ];

    const merged = mergeIncomingMessage(existing, {
      role: "user",
      kind: "bash",
      content: "pwd",
      timestamp: "2026-03-17T12:00:02.000Z",
      command: { command: "pwd" },
    });

    expect(merged).toHaveLength(2);
  });

  it("appends non-matching realtime messages after optimistic entries", () => {
    const existing: Message[] = [
      {
        role: "user",
        kind: "text",
        content: "first prompt",
        timestamp: "2026-03-17T12:00:00.000Z",
        optimistic: true,
        optimistic_id: "optimistic-2",
      },
    ];

    const merged = mergeIncomingMessage(existing, {
      role: "assistant",
      kind: "text",
      content: "reply",
      timestamp: "2026-03-17T12:00:01.000Z",
    });

    expect(merged).toHaveLength(2);
    expect(merged[1].content).toBe("reply");
  });

  it("replaces the latest matching optimistic message even when it is not last", () => {
    const existing: Message[] = [
      {
        role: "user",
        kind: "text",
        content: "describe this",
        timestamp: "2026-03-17T12:00:00.000Z",
        optimistic: true,
        optimistic_id: "optimistic-3",
      },
      {
        role: "assistant",
        kind: "text",
        content: "working",
        timestamp: "2026-03-17T12:00:01.000Z",
      },
    ];

    const merged = mergeIncomingMessage(existing, {
      role: "user",
      kind: "text",
      content: "describe this",
      timestamp: "2026-03-17T12:00:02.000Z",
    });

    expect(merged).toHaveLength(2);
    expect(merged[0].optimistic).toBeUndefined();
    expect(merged[1].content).toBe("working");
  });
});

describe("removeOptimisticMessage", () => {
  it("removes only the matching optimistic message", () => {
    const messages: Message[] = [
      {
        role: "user",
        kind: "text",
        content: "failed prompt",
        timestamp: "2026-03-17T12:00:00.000Z",
        optimistic: true,
        optimistic_id: "optimistic-failed",
      },
      {
        role: "user",
        kind: "text",
        content: "retry prompt",
        timestamp: "2026-03-17T12:00:01.000Z",
        optimistic: true,
        optimistic_id: "optimistic-retry",
      },
    ];

    expect(removeOptimisticMessage(messages, "optimistic-failed")).toEqual([messages[1]]);
  });
});
