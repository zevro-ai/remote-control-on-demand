// @vitest-environment jsdom

import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import {
  AuthPrompt,
  DeploymentRefreshBanner,
  buildExternalLoginURL,
  hasDeploymentUpdate,
  shouldPollDeploymentMeta,
} from "./App";

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

  it("keeps the token fallback visible when external auth also enables bearer tokens", () => {
    render(
      <AuthPrompt
        authStatus={{
          mode: "external",
          token_enabled: true,
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
    expect(screen.getByPlaceholderText("Paste API token")).toBeTruthy();
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

describe("hasDeploymentUpdate", () => {
  it("returns true when the build ID changes", () => {
    expect(
      hasDeploymentUpdate(
        { version: "v0.21.0", build_id: "v0.21.0+aaaa", started_at: "2026-03-29T17:00:00Z" },
        { version: "v0.22.0", build_id: "v0.22.0+bbbb", started_at: "2026-03-29T17:05:00Z" },
      ),
    ).toBe(true);
  });

  it("returns false when the build ID stays the same", () => {
    expect(
      hasDeploymentUpdate(
        { version: "v0.22.0", build_id: "v0.22.0+bbbb", started_at: "2026-03-29T17:00:00Z" },
        { version: "v0.22.0", build_id: "v0.22.0+bbbb", started_at: "2026-03-29T17:05:00Z" },
      ),
    ).toBe(false);
  });
});

describe("shouldPollDeploymentMeta", () => {
  it("returns false while the dashboard is still loading", () => {
    expect(shouldPollDeploymentMeta(true, false)).toBe(false);
  });

  it("returns false when authentication is required", () => {
    expect(shouldPollDeploymentMeta(false, true)).toBe(false);
  });

  it("returns true once the dashboard is ready", () => {
    expect(shouldPollDeploymentMeta(false, false)).toBe(true);
  });
});

describe("DeploymentRefreshBanner", () => {
  it("renders version-aware copy and triggers refresh", () => {
    const onRefresh = vi.fn();

    render(
      <DeploymentRefreshBanner
        initialDeployment={{ version: "v0.21.0", build_id: "v0.21.0+aaaa", started_at: "2026-03-29T17:00:00Z" }}
        latestDeployment={{ version: "v0.22.0", build_id: "v0.22.0+bbbb", started_at: "2026-03-29T17:05:00Z" }}
        onRefresh={onRefresh}
      />,
    );

    expect(screen.getByText("Refresh to load the latest dashboard deployment")).toBeTruthy();
    expect(screen.getByText(/still running v0.21.0/i)).toBeTruthy();

    fireEvent.click(screen.getByRole("button", { name: "Refresh" }));
    expect(onRefresh).toHaveBeenCalledTimes(1);
  });
});
