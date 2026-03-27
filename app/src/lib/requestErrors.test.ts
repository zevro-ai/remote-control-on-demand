import { describe, expect, it } from "vitest";

import {
  hasErrorStatus,
  isUnauthorizedError,
  toLoopActionErrorMessage,
  toRequestErrorMessage,
} from "./requestErrors";

describe("isUnauthorizedError", () => {
  it("matches API unauthorized responses", () => {
    expect(isUnauthorizedError(new Error("unauthorized"))).toBe(true);
    expect(isUnauthorizedError(new Error("Unauthorized"))).toBe(true);
    expect(isUnauthorizedError(Object.assign(new Error("forbidden"), { status: 403 }))).toBe(true);
  });

  it("ignores unrelated failures", () => {
    expect(isUnauthorizedError(new Error("network failed"))).toBe(false);
    expect(isUnauthorizedError("unauthorized")).toBe(false);
  });
});

describe("hasErrorStatus", () => {
  it("matches errors with the expected HTTP status", () => {
    expect(hasErrorStatus(Object.assign(new Error("not found"), { status: 404 }), 404)).toBe(true);
    expect(hasErrorStatus(Object.assign(new Error("unauthorized"), { status: 401 }), 404)).toBe(false);
  });
});

describe("toRequestErrorMessage", () => {
  it("returns the error message when available", () => {
    expect(toRequestErrorMessage(new Error("session not found"))).toBe("session not found");
  });

  it("falls back for unknown errors", () => {
    expect(toRequestErrorMessage("boom", "Send failed.")).toBe("Send failed.");
  });
});

describe("toLoopActionErrorMessage", () => {
  it("includes loop progress for repeated actions", () => {
    expect(toLoopActionErrorMessage(new Error("timeout"), "Send", 2, 5)).toBe(
      "timeout Loop stopped at 2/5."
    );
  });

  it("omits loop progress for single actions", () => {
    expect(toLoopActionErrorMessage(new Error("timeout"), "Run", 1, 1)).toBe("timeout");
  });
});
