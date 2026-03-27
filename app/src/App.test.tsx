// @vitest-environment jsdom

import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it } from "vitest";

import { AuthPrompt } from "./App";

afterEach(cleanup);

describe("AuthPrompt", () => {
  it("renders the token prompt when token auth is active", () => {
    render(
      <AuthPrompt
        authStatus={{ mode: "token", token_enabled: true, authenticated: false }}
        value=""
        hasStoredToken={false}
        onChange={() => {}}
      />
    );

    expect(screen.getByPlaceholderText("Paste API token")).toBeTruthy();
    expect(screen.getByRole("button", { name: "Save token" })).toBeTruthy();
  });

  it("renders the external provider prompt when external auth is active", () => {
    render(
      <AuthPrompt
        authStatus={{
          mode: "external",
          token_enabled: false,
          authenticated: false,
          provider: { id: "github", display_name: "GitHub" },
          login_url: "/api/auth/login",
        }}
        value=""
        hasStoredToken={false}
        onChange={() => {}}
      />
    );

    expect(screen.getByRole("button", { name: "Sign in with GitHub" })).toBeTruthy();
    expect(screen.queryByPlaceholderText("Paste API token")).toBeNull();
  });
});
