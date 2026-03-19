import { describe, expect, it } from "vitest";
import { MAX_REPEAT_COUNT, normalizeRepeatCount, repeatSequentially } from "./repeatAction";

describe("normalizeRepeatCount", () => {
  it("falls back to 1 for invalid input", () => {
    expect(normalizeRepeatCount(undefined)).toBe(1);
    expect(normalizeRepeatCount("")).toBe(1);
    expect(normalizeRepeatCount(0)).toBe(1);
    expect(normalizeRepeatCount(-4)).toBe(1);
  });

  it("rounds down and clamps to the maximum", () => {
    expect(normalizeRepeatCount("3")).toBe(3);
    expect(normalizeRepeatCount(4.8)).toBe(4);
    expect(normalizeRepeatCount(MAX_REPEAT_COUNT + 10)).toBe(MAX_REPEAT_COUNT);
  });
});

describe("repeatSequentially", () => {
  it("runs the action in order for each iteration", async () => {
    const calls: string[] = [];

    await repeatSequentially(3, async (iteration, total) => {
      calls.push(`start:${iteration}/${total}`);
      await Promise.resolve();
      calls.push(`done:${iteration}/${total}`);
    });

    expect(calls).toEqual([
      "start:1/3",
      "done:1/3",
      "start:2/3",
      "done:2/3",
      "start:3/3",
      "done:3/3",
    ]);
  });
});
