import { render, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import * as api from "../../api/learning";
import TeamLearningPanel from "./TeamLearningPanel";

describe("<TeamLearningPanel>", () => {
  afterEach(() => {
    vi.restoreAllMocks();
  });

  it("renders linked learnings for a playbook", async () => {
    vi.spyOn(api, "fetchTeamLearnings").mockResolvedValue([
      {
        id: "lrn-1",
        type: "pitfall",
        key: "active-skill-filter",
        insight: "Filter inactive skills before prompt injection.",
        confidence: 8,
        effective_confidence: 8,
        source: "observed",
        trusted: false,
        scope: "playbook:ship-pr",
        playbook_slug: "ship-pr",
        created_by: "codex",
        created_at: "2026-04-30T10:00:00Z",
      },
    ]);

    render(<TeamLearningPanel playbookSlug="ship-pr" />);

    await screen.findByText("active-skill-filter");
    expect(
      screen.getByText("Filter inactive skills before prompt injection."),
    ).toBeInTheDocument();
    expect(api.fetchTeamLearnings).toHaveBeenCalledWith({
      playbook_slug: "ship-pr",
      limit: 6,
    });
  });

  it("renders an empty state", async () => {
    vi.spyOn(api, "fetchTeamLearnings").mockResolvedValue([]);

    render(<TeamLearningPanel playbookSlug="ship-pr" />);

    await waitFor(() =>
      expect(screen.getByText(/no structured learnings/i)).toBeInTheDocument(),
    );
  });

  it("does not fetch unscoped learnings without a playbook slug", async () => {
    const fetchSpy = vi.spyOn(api, "fetchTeamLearnings").mockResolvedValue([]);

    render(<TeamLearningPanel />);

    await waitFor(() =>
      expect(screen.getByText(/no structured learnings/i)).toBeInTheDocument(),
    );
    expect(fetchSpy).not.toHaveBeenCalled();
  });

  it("renders fetch failures separately from empty results", async () => {
    vi.spyOn(api, "fetchTeamLearnings").mockRejectedValue(
      new Error("401 Unauthorized"),
    );

    render(<TeamLearningPanel playbookSlug="ship-pr" />);

    await screen.findByText(/could not load team learnings/i);
    expect(screen.getByText(/401 Unauthorized/)).toBeInTheDocument();
    expect(
      screen.queryByText(/no structured learnings/i),
    ).not.toBeInTheDocument();
  });
});
