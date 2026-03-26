// @vitest-environment jsdom

import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it } from "vitest";
import { TodoProgressPanel } from "./TodoProgressPanel";

afterEach(cleanup);

describe("TodoProgressPanel", () => {
  it("renders progress counts and highlights the active item", () => {
    render(
      <TodoProgressPanel
        progress={{
          completedCount: 2,
          totalCount: 4,
          items: [
            { text: "Read config", completed: true, active: false },
            { text: "Patch parser", completed: true, active: false },
            { text: "Write tests", completed: false, active: true },
            { text: "Update docs", completed: false, active: false },
          ],
        }}
      />
    );

    expect(screen.getByText("Todo Progress")).toBeTruthy();
    expect(screen.getAllByText("2/4")).toHaveLength(1);
    expect(screen.getByText("Write tests")).toBeTruthy();
    expect(screen.getByText("Update docs")).toBeTruthy();
    expect(screen.getByText("2/4 items completed")).toBeTruthy();
  });
});
