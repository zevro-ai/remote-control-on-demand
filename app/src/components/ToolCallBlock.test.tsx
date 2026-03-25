// @vitest-environment jsdom

import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { ToolCallBlock } from "./ToolCallBlock";

describe("ToolCallBlock", () => {
  it("renders a before and after view for file edit tool calls", () => {
    render(
      <ToolCallBlock
        name="Edit"
        id="tool-1"
        inputJSON={JSON.stringify({
          file_path: "/tmp/app.ts",
          old_string: "const answer = 41;",
          new_string: "const answer = 42;",
        })}
        done
      />
    );

    fireEvent.click(screen.getByRole("button"));

    expect(screen.getByText("Before")).toBeTruthy();
    expect(screen.getByText("After")).toBeTruthy();
    expect(screen.getByText("const answer = 41;")).toBeTruthy();
    expect(screen.getByText("const answer = 42;")).toBeTruthy();
  });
});
