// @vitest-environment jsdom

import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it } from "vitest";

import { AuthPrompt, buildExternalLoginURL } from "./App";

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

  it("preserves the current page when building the external login redirect", () => {
    expect(
      buildExternalLoginURL("/api/auth/login", {
        origin: "https://rcod.example.com",
        pathname: "/dashboard",
        search: "?panel=codex",
        hash: "#session-1",
      }),
    ).toBe(
      "https://rcod.example.com/api/auth/login?redirect=%2Fdashboard%3Fpanel%3Dcodex%23session-1",
    );
  });
});
