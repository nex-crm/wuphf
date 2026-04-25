import { render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import type { NotebookAgentSummary, NotebookEntry } from "../../api/notebook";
import * as api from "../../api/notebook";
import AgentNotebookView from "./AgentNotebookView";

const PM_AGENT: NotebookAgentSummary = {
  agent_slug: "pm",
  name: "PM",
  role: "Product Manager · agent",
  entries: [],
  total: 2,
  promoted_count: 0,
  last_updated_ts: new Date().toISOString(),
};

const PM_ENTRIES: NotebookEntry[] = [
  {
    agent_slug: "pm",
    entry_slug: "acme",
    title: "Customer Acme rough notes",
    body_md: "Body.",
    last_edited_ts: new Date().toISOString(),
    revisions: 1,
    status: "draft",
    file_path: "acme.md",
    reviewer_slug: "ceo",
  },
  {
    agent_slug: "pm",
    entry_slug: "pricing",
    title: "Pricing objections",
    body_md: "Body 2.",
    last_edited_ts: new Date(Date.now() - 60_000).toISOString(),
    revisions: 1,
    status: "draft",
    file_path: "pricing.md",
    reviewer_slug: "ceo",
  },
];

describe("<AgentNotebookView>", () => {
  beforeEach(() => {
    vi.restoreAllMocks();
  });

  it("loads the agent and renders the most recent entry by default", async () => {
    vi.spyOn(api, "fetchAgentEntries").mockResolvedValue({
      agent: PM_AGENT,
      entries: PM_ENTRIES,
    });
    render(
      <AgentNotebookView
        agentSlug="pm"
        entrySlug={null}
        onNavigateCatalog={() => {}}
        onSelectEntry={() => {}}
      />,
    );
    await waitFor(() =>
      expect(
        screen.getByRole("heading", { name: "Customer Acme rough notes" }),
      ).toBeInTheDocument(),
    );
    expect(
      screen.getByRole("heading", { name: "PM's notebook" }),
    ).toBeInTheDocument();
  });

  it("renders the specified entry when entrySlug is provided", async () => {
    vi.spyOn(api, "fetchAgentEntries").mockResolvedValue({
      agent: PM_AGENT,
      entries: PM_ENTRIES,
    });
    render(
      <AgentNotebookView
        agentSlug="pm"
        entrySlug="pricing"
        onNavigateCatalog={() => {}}
        onSelectEntry={() => {}}
      />,
    );
    await waitFor(() =>
      expect(
        screen.getByRole("heading", { name: "Pricing objections" }),
      ).toBeInTheDocument(),
    );
  });

  it("shows landing prompt when agent has no entries", async () => {
    vi.spyOn(api, "fetchAgentEntries").mockResolvedValue({
      agent: { ...PM_AGENT, total: 0 },
      entries: [],
    });
    render(
      <AgentNotebookView
        agentSlug="pm"
        entrySlug={null}
        onNavigateCatalog={() => {}}
        onSelectEntry={() => {}}
      />,
    );
    await waitFor(() =>
      expect(
        screen.getByText(/PM has not written anything/),
      ).toBeInTheDocument(),
    );
  });

  it("renders an error state with retry button when fetch fails", async () => {
    vi.spyOn(api, "fetchAgentEntries").mockRejectedValue(new Error("boom"));
    render(
      <AgentNotebookView
        agentSlug="pm"
        entrySlug={null}
        onNavigateCatalog={() => {}}
        onSelectEntry={() => {}}
      />,
    );
    await waitFor(() => expect(screen.getByRole("alert")).toBeInTheDocument());
    expect(screen.getByRole("button", { name: "Retry" })).toBeInTheDocument();
  });

  it("shows agent-not-found fallback when API returns null agent", async () => {
    vi.spyOn(api, "fetchAgentEntries").mockResolvedValue({
      agent: null,
      entries: [],
    });
    render(
      <AgentNotebookView
        agentSlug="zzz"
        entrySlug={null}
        onNavigateCatalog={() => {}}
        onSelectEntry={() => {}}
      />,
    );
    await waitFor(() =>
      expect(screen.getByText(/Agent not found/)).toBeInTheDocument(),
    );
  });
});
