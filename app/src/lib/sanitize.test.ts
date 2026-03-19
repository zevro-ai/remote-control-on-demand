import { beforeEach, describe, expect, it, vi } from "vitest";

const sanitizeSpy = vi.fn((html: string) => html);
const ansiInstances: MockAnsiUp[] = [];
const AnsiUpMock = vi.fn(() => {
  const instance = new MockAnsiUp();
  ansiInstances.push(instance);
  return instance;
});

class MockAnsiUp {
  use_classes = false;

  ansi_to_html(raw: string) {
    return `<span>${raw}</span>`;
  }
}

vi.mock("ansi_up", () => ({
  AnsiUp: AnsiUpMock,
}));

vi.mock("dompurify", () => ({
  default: {
    sanitize: sanitizeSpy,
  },
}));

describe("ansiToSafeHtml", () => {
  beforeEach(() => {
    AnsiUpMock.mockClear();
    sanitizeSpy.mockClear();
    ansiInstances.length = 0;
  });

  it("creates a fresh AnsiUp instance for each call", async () => {
    const { ansiToSafeHtml } = await import("./sanitize");

    expect(ansiToSafeHtml("\u001b[31mred")).toBe("<span>\u001b[31mred</span>");
    expect(ansiToSafeHtml("plain")).toBe("<span>plain</span>");

    expect(AnsiUpMock).toHaveBeenCalledTimes(2);
    expect(ansiInstances).toHaveLength(2);
    expect(ansiInstances[0]).not.toBe(ansiInstances[1]);
    expect(ansiInstances[0].use_classes).toBe(true);
    expect(ansiInstances[1].use_classes).toBe(true);
    expect(sanitizeSpy).toHaveBeenNthCalledWith(1, "<span>\u001b[31mred</span>", {
      ALLOWED_TAGS: ["span"],
      ALLOWED_ATTR: ["class"],
    });
    expect(sanitizeSpy).toHaveBeenNthCalledWith(2, "<span>plain</span>", {
      ALLOWED_TAGS: ["span"],
      ALLOWED_ATTR: ["class"],
    });
  });
});
