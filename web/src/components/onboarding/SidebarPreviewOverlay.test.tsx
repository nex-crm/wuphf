/**
 * SidebarPreviewOverlay tests
 *
 * 1. Shows preview rows when phase is active (non-complete)
 * 2. Hides when phase is complete (onboarded=true)
 * 3. Shows workspace label when company_name is filled
 * 4. Adds seeding class when phase = "seed"
 */

import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { SidebarPreviewOverlay } from "./SidebarPreviewOverlay";

vi.mock("../../api/client", async () => {
  const actual =
    await vi.importActual<typeof import("../../api/client")>(
      "../../api/client",
    );
  return { ...actual, get: vi.fn(), post: vi.fn() };
});

import { get } from "../../api/client";

const getMock = vi.mocked(get);

function wrapper({ children }: { children: React.ReactNode }) {
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return <QueryClientProvider client={qc}>{children}</QueryClientProvider>;
}

beforeEach(() => {
  getMock.mockReset();
});

afterEach(() => {
  cleanup();
});

describe("SidebarPreviewOverlay", () => {
  it("renders nothing when onboarded is true", async () => {
    getMock.mockResolvedValue({ onboarded: true, phase: undefined });
    render(<SidebarPreviewOverlay />, { wrapper });
    // Wait a tick for the query to resolve.
    await new Promise((r) => setTimeout(r, 50));
    expect(
      screen.queryByTestId("sidebar-preview-overlay"),
    ).not.toBeInTheDocument();
  });

  it("renders nothing when state is loading or has no phase", async () => {
    getMock.mockResolvedValue({ onboarded: false, phase: undefined });
    render(<SidebarPreviewOverlay />, { wrapper });
    await new Promise((r) => setTimeout(r, 50));
    expect(
      screen.queryByTestId("sidebar-preview-overlay"),
    ).not.toBeInTheDocument();
  });

  it("renders preview rows when phase is 'blueprint' and blueprint is chosen", async () => {
    getMock.mockResolvedValue({
      onboarded: false,
      phase: "blueprint",
      form_answers: {
        blueprint_id: "engineering-team",
        company_name: "Acme Corp",
      },
    });
    render(<SidebarPreviewOverlay />, { wrapper });

    const overlay = await screen.findByTestId("sidebar-preview-overlay");
    expect(overlay).toBeInTheDocument();

    // Engineering team blueprint has #engineering and #standup channels
    expect(overlay).toHaveTextContent("#engineering");
    expect(overlay).toHaveTextContent("#standup");
  });

  it("shows workspace label when company_name is provided", async () => {
    getMock.mockResolvedValue({
      onboarded: false,
      phase: "identity",
      form_answers: {
        company_name: "Acme Corp",
      },
    });
    render(<SidebarPreviewOverlay />, { wrapper });

    const workspace = await screen.findByTestId("sidebar-preview-workspace");
    expect(workspace).toHaveTextContent("Acme Corp");
  });

  it("shows #general channel in scratch path", async () => {
    getMock.mockResolvedValue({
      onboarded: false,
      phase: "blueprint",
      form_answers: {
        blueprint_id: "scratch",
      },
    });
    render(<SidebarPreviewOverlay />, { wrapper });

    const overlay = await screen.findByTestId("sidebar-preview-overlay");
    expect(overlay).toHaveTextContent("#general");
  });

  it("adds --seeding class when phase is 'seed'", async () => {
    getMock.mockResolvedValue({
      onboarded: false,
      phase: "seed",
      form_answers: {
        company_name: "Acme",
      },
    });
    render(<SidebarPreviewOverlay />, { wrapper });

    const overlay = await screen.findByTestId("sidebar-preview-overlay");
    expect(overlay).toHaveClass("sidebar-preview-overlay--seeding");
  });

  it("shows agent rows when picked_agents is populated", async () => {
    getMock.mockResolvedValue({
      onboarded: false,
      phase: "team",
      form_answers: {
        blueprint_id: "engineering-team",
        picked_agents: ["engineer", "pm"],
      },
    });
    render(<SidebarPreviewOverlay />, { wrapper });

    const overlay = await screen.findByTestId("sidebar-preview-overlay");
    const agentRows = overlay.querySelectorAll(
      '[data-testid="sidebar-preview-row-agent"]',
    );
    expect(agentRows.length).toBe(2);
  });

  it("renders workspace label as plain text (XSS protection)", async () => {
    getMock.mockResolvedValue({
      onboarded: false,
      phase: "identity",
      form_answers: {
        company_name: '<script>alert("xss")</script>',
      },
    });
    render(<SidebarPreviewOverlay />, { wrapper });

    await screen.findByTestId("sidebar-preview-workspace");
    // The script element should not exist in the DOM
    expect(document.querySelector("script")).not.toBeInTheDocument();
    // The text should be present as literal characters
    expect(
      screen.getByText('<script>alert("xss")</script>'),
    ).toBeInTheDocument();
  });
});
