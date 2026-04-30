import type { ReactNode } from "react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

vi.mock("../../api/surfaces", () => ({
  listSurfaces: vi.fn(),
  readSurface: vi.fn(),
  createSurface: vi.fn(),
  subscribeSurfaceEvents: vi.fn(() => vi.fn()),
}));

import { createSurface, listSurfaces, readSurface } from "../../api/surfaces";
import { useAppStore } from "../../stores/app";
import { StudioApp } from "./StudioApp";

function wrap(ui: ReactNode) {
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return <QueryClientProvider client={qc}>{ui}</QueryClientProvider>;
}

describe("<StudioApp>", () => {
  beforeEach(() => {
    useAppStore.setState({ currentChannel: "general" });
    vi.mocked(listSurfaces).mockResolvedValue({
      surfaces: [
        {
          id: "launch",
          title: "Launch command center",
          channel: "general",
          created_at: "2026-04-30T00:00:00Z",
          updated_at: "2026-04-30T00:00:00Z",
          widget_count: 2,
        },
      ],
    });
    vi.mocked(readSurface).mockResolvedValue({
      surface: {
        id: "launch",
        title: "Launch command center",
        channel: "general",
        created_at: "2026-04-30T00:00:00Z",
        updated_at: "2026-04-30T00:00:00Z",
        widget_count: 2,
      },
      widgets: [
        {
          widget: {
            id: "checklist",
            title: "Open blockers",
            kind: "checklist",
            source: "kind: checklist\nitems: []\n",
          },
          source_lines: [
            { number: 1, text: "kind: checklist" },
            { number: 2, text: "items: []" },
          ],
          render: {
            schema_ok: true,
            render_ok: true,
            preview_text: "[ ] Call customer",
            normalized_widget: {
              id: "checklist",
              title: "Open blockers",
              kind: "checklist",
              schema_version: "surface.widget.v1",
              checklist: [
                { id: "customer", label: "Call customer", checked: false },
              ],
            },
          },
        },
        {
          widget: {
            id: "notes",
            title: "Notes",
            kind: "markdown",
            source: "kind: markdown\nmarkdown: Ship it.\n",
          },
          source_lines: [
            { number: 1, text: "kind: markdown" },
            { number: 2, text: "markdown: Ship it." },
          ],
          render: {
            schema_ok: true,
            render_ok: true,
            preview_text: "Ship it.",
            normalized_widget: {
              id: "notes",
              title: "Notes",
              kind: "markdown",
              schema_version: "surface.widget.v1",
              markdown: "Ship it.",
            },
          },
        },
      ],
      history: [
        {
          id: "history-1",
          surface_id: "launch",
          widget_id: "checklist",
          kind: "widget_updated",
          actor: "CEO",
          summary: "Updated Open blockers.",
          created_at: "2026-04-30T00:00:00Z",
        },
      ],
    });
    vi.mocked(createSurface).mockResolvedValue({
      surface: {
        id: "general-command-center-2",
        title: "general command center 2",
        channel: "general",
        created_at: "2026-04-30T00:00:00Z",
        updated_at: "2026-04-30T00:00:00Z",
      },
    });
  });

  it("renders the workspace, widgets, and numbered source inspector", async () => {
    render(wrap(<StudioApp />));

    await waitFor(() =>
      expect(screen.getByText("Open blockers")).toBeInTheDocument(),
    );
    expect(screen.getAllByText("Launch command center").length).toBeGreaterThan(
      0,
    );
    expect(screen.getByText("Call customer")).toBeInTheDocument();
    expect(screen.getByText("kind: checklist")).toBeInTheDocument();
    expect(screen.getByText("Recent activity")).toBeInTheDocument();
    expect(screen.getByText("Updated Open blockers.")).toBeInTheDocument();
    expect(screen.getByText("1")).toBeInTheDocument();
  });

  it("creates the next available command-center title for repeat clicks", async () => {
    vi.mocked(listSurfaces).mockResolvedValue({
      surfaces: [
        {
          id: "general-command-center",
          title: "general command center",
          channel: "general",
          created_at: "2026-04-30T00:00:00Z",
          updated_at: "2026-04-30T00:00:00Z",
          widget_count: 0,
        },
      ],
    });

    render(wrap(<StudioApp />));

    await waitFor(() =>
      expect(screen.getByText("general command center")).toBeInTheDocument(),
    );
    fireEvent.click(screen.getByRole("button", { name: /New surface/i }));

    await waitFor(() =>
      expect(createSurface).toHaveBeenCalledWith({
        title: "general command center 2",
        channel: "general",
      }),
    );
  });
});
