import { render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import type {
  NotebookAgentSummary,
  NotebookCatalogSummary,
} from "../../api/notebook";
import * as api from "../../api/notebook";
import Notebook from "./Notebook";

const CATALOG: NotebookCatalogSummary = {
  total_agents: 1,
  total_entries: 1,
  pending_promotion: 0,
  agents: [
    {
      agent_slug: "pm",
      name: "PM",
      role: "Product Manager · agent",
      entries: [
        {
          entry_slug: "e1",
          title: "Entry one",
          last_edited_ts: new Date().toISOString(),
          status: "draft",
        },
      ],
      total: 1,
      promoted_count: 0,
      last_updated_ts: new Date().toISOString(),
    },
  ],
};

const AGENT_SUMMARY: NotebookAgentSummary = CATALOG.agents[0];

describe("<Notebook>", () => {
  beforeEach(() => {
    vi.restoreAllMocks();
    vi.spyOn(api, "subscribeNotebookEvents").mockImplementation(() => () => {});
  });

  it("renders the bookshelf when no agent is selected", async () => {
    vi.spyOn(api, "fetchCatalog").mockResolvedValue(CATALOG);
    render(
      <Notebook
        agentSlug={null}
        entrySlug={null}
        onOpenCatalog={() => {}}
        onOpenAgent={() => {}}
        onOpenEntry={() => {}}
        onNavigateWiki={() => {}}
      />,
    );
    await waitFor(() =>
      expect(
        screen.getByRole("heading", { name: "Team notebooks" }),
      ).toBeInTheDocument(),
    );
    expect(screen.getByTestId("notebook-surface")).toBeInTheDocument();
  });

  it("renders the agent view when agentSlug is set", async () => {
    vi.spyOn(api, "fetchAgentEntries").mockResolvedValue({
      agent: AGENT_SUMMARY,
      entries: [],
    });
    render(
      <Notebook
        agentSlug="pm"
        entrySlug={null}
        onOpenCatalog={() => {}}
        onOpenAgent={() => {}}
        onOpenEntry={() => {}}
        onNavigateWiki={() => {}}
      />,
    );
    await waitFor(() =>
      expect(
        screen.getByRole("heading", { name: "PM's notebook" }),
      ).toBeInTheDocument(),
    );
  });

  it("renders an error state + Retry when catalog fetch fails", async () => {
    vi.spyOn(api, "fetchCatalog").mockRejectedValue(new Error("broker down"));
    render(
      <Notebook
        agentSlug={null}
        entrySlug={null}
        onOpenCatalog={() => {}}
        onOpenAgent={() => {}}
        onOpenEntry={() => {}}
        onNavigateWiki={() => {}}
      />,
    );
    await waitFor(() => expect(screen.getByRole("alert")).toBeInTheDocument());
    expect(screen.getByRole("button", { name: "Retry" })).toBeInTheDocument();
  });

  it("flips a broker-timeout into the honest error state, not an eternal spinner (C3)", async () => {
    // The v3 eval caught the Notebooks tab at "Loading bookshelf…" for
    // 60s+ — the catalog GET had no timeout, so a wedged broker left
    // the promise pending forever. The api client now aborts GETs and
    // throws this message; the surface must land on the retry state.
    vi.spyOn(api, "fetchCatalog").mockRejectedValue(
      new Error("Broker not responding — request timed out."),
    );
    render(
      <Notebook
        agentSlug={null}
        entrySlug={null}
        onOpenCatalog={() => {}}
        onOpenAgent={() => {}}
        onOpenEntry={() => {}}
        onNavigateWiki={() => {}}
      />,
    );
    await waitFor(() =>
      expect(screen.getByRole("alert")).toHaveTextContent(
        /broker not responding/i,
      ),
    );
    expect(screen.getByRole("button", { name: "Retry" })).toBeInTheDocument();
    expect(screen.queryByText(/Loading bookshelf/)).toBeNull();
  });

  it("subscribes to notebook events on mount and unsubscribes on unmount", () => {
    const unsub = vi.fn();
    const spy = vi
      .spyOn(api, "subscribeNotebookEvents")
      .mockImplementation(() => unsub);
    vi.spyOn(api, "fetchCatalog").mockResolvedValue(CATALOG);
    const { unmount } = render(
      <Notebook
        agentSlug={null}
        entrySlug={null}
        onOpenCatalog={() => {}}
        onOpenAgent={() => {}}
        onOpenEntry={() => {}}
        onNavigateWiki={() => {}}
      />,
    );
    expect(spy).toHaveBeenCalled();
    unmount();
    expect(unsub).toHaveBeenCalled();
  });
});
