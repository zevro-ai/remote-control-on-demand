import { afterEach, describe, expect, it, vi } from "vitest";

import { APIError, api } from "./client";

describe("api client", () => {
  afterEach(() => {
    vi.restoreAllMocks();
  });

  it("throws APIError with the HTTP status for JSON requests", async () => {
    vi.stubGlobal("localStorage", {
      getItem: vi.fn().mockReturnValue(null),
      setItem: vi.fn(),
      removeItem: vi.fn(),
      clear: vi.fn(),
    });
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue({
        ok: false,
        status: 404,
        statusText: "Not Found",
        json: async () => ({ error: "not found" }),
      })
    );

    await expect(api.get("/api/providers")).rejects.toEqual(
      expect.objectContaining<Partial<APIError>>({
        name: "APIError",
        message: "not found",
        status: 404,
      })
    );
  });
});
