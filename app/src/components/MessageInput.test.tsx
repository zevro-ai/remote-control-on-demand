// @vitest-environment jsdom

import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { MessageInput } from "./MessageInput";

afterEach(cleanup);

describe("MessageInput", () => {
  it("clears the prompt immediately and disables the composer while submit is in flight", async () => {
    let resolveSubmit: (() => void) | undefined;
    const onSendPrompt = vi.fn(
      () =>
        new Promise<void>((resolve) => {
          resolveSubmit = resolve;
        })
    );

    render(<MessageInput onSendPrompt={onSendPrompt} />);

    const textarea = screen.getByPlaceholderText("Send a message...") as HTMLTextAreaElement;
    fireEvent.change(textarea, { target: { value: "Apply the patch" } });
    fireEvent.click(screen.getByRole("button", { name: "Send" }));

    expect(onSendPrompt).toHaveBeenCalledWith("Apply the patch", []);
    expect(textarea.value).toBe("");
    expect(textarea.disabled).toBe(true);

    resolveSubmit?.();

    await waitFor(() => {
      expect(textarea.disabled).toBe(false);
    });
  });

  it("restores the prompt when submit fails", async () => {
    const onSendPrompt = vi.fn().mockRejectedValue(new Error("request failed"));

    render(<MessageInput onSendPrompt={onSendPrompt} />);

    const textarea = screen.getByPlaceholderText("Send a message...") as HTMLTextAreaElement;
    fireEvent.change(textarea, { target: { value: "Retry later" } });
    fireEvent.click(screen.getByRole("button", { name: "Send" }));

    await screen.findByRole("alert");

    expect(textarea.value).toBe("Retry later");
    expect(textarea.disabled).toBe(false);
  });
});
