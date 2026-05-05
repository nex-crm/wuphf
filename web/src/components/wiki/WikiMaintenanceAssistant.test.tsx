import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import * as wikiApi from "../../api/wiki";
import {
  consumeMaintenanceTarget,
  requestMaintenanceTarget,
} from "./maintenanceTarget";
import WikiMaintenanceAssistant from "./WikiMaintenanceAssistant";

const SAMPLE_SUGGESTION: wikiApi.WikiMaintenanceSuggestion = {
  action: "summarize",
  title: "Summarize page",
  description: "Insert a TL;DR.",
  diff: {
    proposed_content: "# Sample\n\n> **TL;DR:** Hello.\n\nBody.",
    added: ["> **TL;DR:** Hello."],
  },
  evidence: [
    {
      kind: "wiki_article",
      label: "Article body lead",
      path: "team/people/sarah-chen.md",
    },
  ],
  expected_sha: "abc1234",
};

const SKIPPED_SUGGESTION: wikiApi.WikiMaintenanceSuggestion = {
  action: "split_long_page",
  title: "Split long page",
  skipped: true,
  skipped_reason: "Article is short.",
};

const FACTS_SUGGESTION: wikiApi.WikiMaintenanceSuggestion = {
  action: "extract_facts",
  title: "Extract facts",
  description: "Review proposed facts.",
  facts: [
    {
      subject: "sarah-chen",
      predicate: "role_at",
      object: "acme",
      confidence: 0.6,
      source_line: 3,
    },
  ],
};

beforeEach(() => {
  vi.restoreAllMocks();
  if (typeof window !== "undefined") {
    window.localStorage.clear();
    window.sessionStorage.clear();
  }
});

describe("<WikiMaintenanceAssistant>", () => {
  it("renders collapsed by default with an open button", () => {
    render(
      <WikiMaintenanceAssistant
        articlePath="team/people/sarah-chen.md"
        articleSha="abc1234"
        onApplied={vi.fn()}
      />,
    );
    expect(screen.getByTestId("wk-maint-collapsed")).toBeInTheDocument();
    expect(screen.queryByTestId("wk-maint-panel")).toBeNull();
  });

  it("expanding the panel shows all 7 actions", () => {
    render(
      <WikiMaintenanceAssistant
        articlePath="team/people/sarah-chen.md"
        articleSha="abc1234"
        onApplied={vi.fn()}
      />,
    );
    fireEvent.click(screen.getByTestId("wk-maint-open"));
    expect(screen.getByTestId("wk-maint-panel")).toBeInTheDocument();
    expect(screen.getByTestId("wk-maint-action-summarize")).toBeInTheDocument();
    expect(
      screen.getByTestId("wk-maint-action-add_citation"),
    ).toBeInTheDocument();
    expect(
      screen.getByTestId("wk-maint-action-extract_facts"),
    ).toBeInTheDocument();
    expect(
      screen.getByTestId("wk-maint-action-link_related"),
    ).toBeInTheDocument();
    expect(
      screen.getByTestId("wk-maint-action-split_long_page"),
    ).toBeInTheDocument();
    expect(
      screen.getByTestId("wk-maint-action-refresh_stale"),
    ).toBeInTheDocument();
    expect(
      screen.getByTestId("wk-maint-action-resolve_contradiction"),
    ).toBeInTheDocument();
  });

  it("clicking an action calls fetchMaintenanceSuggestion and shows the diff", async () => {
    const fetchSpy = vi
      .spyOn(wikiApi, "fetchMaintenanceSuggestion")
      .mockResolvedValue(SAMPLE_SUGGESTION);
    render(
      <WikiMaintenanceAssistant
        articlePath="team/people/sarah-chen.md"
        articleSha="abc1234"
        onApplied={vi.fn()}
      />,
    );
    fireEvent.click(screen.getByTestId("wk-maint-open"));
    fireEvent.click(screen.getByTestId("wk-maint-action-summarize"));
    expect(fetchSpy).toHaveBeenCalledWith(
      "summarize",
      "team/people/sarah-chen.md",
    );
    await waitFor(() =>
      expect(screen.getByTestId("wk-maint-suggestion")).toBeInTheDocument(),
    );
    expect(screen.getByTestId("wk-maint-diff")).toBeInTheDocument();
    expect(screen.getByTestId("wk-maint-accept")).toBeInTheDocument();
  });

  it("Accept routes the diff through writeArticle and calls onApplied", async () => {
    vi.spyOn(wikiApi, "fetchMaintenanceSuggestion").mockResolvedValue(
      SAMPLE_SUGGESTION,
    );
    const onApplied = vi.fn();
    const writeArticle = vi.fn().mockResolvedValue({
      path: "team/people/sarah-chen.md",
      commit_sha: "def5678",
      bytes_written: 42,
    });

    render(
      <WikiMaintenanceAssistant
        articlePath="team/people/sarah-chen.md"
        articleSha="abc1234"
        onApplied={onApplied}
        writeArticle={writeArticle}
      />,
    );
    fireEvent.click(screen.getByTestId("wk-maint-open"));
    fireEvent.click(screen.getByTestId("wk-maint-action-summarize"));
    await waitFor(() =>
      expect(screen.getByTestId("wk-maint-accept")).toBeInTheDocument(),
    );
    fireEvent.click(screen.getByTestId("wk-maint-accept"));
    await waitFor(() => expect(onApplied).toHaveBeenCalledTimes(1));
    expect(writeArticle).toHaveBeenCalledWith(
      expect.objectContaining({
        path: "team/people/sarah-chen.md",
        content: SAMPLE_SUGGESTION.diff?.proposed_content,
        expectedSha: "abc1234",
      }),
    );
  });

  it("Reject snoozes the action for 24h via localStorage", async () => {
    vi.spyOn(wikiApi, "fetchMaintenanceSuggestion").mockResolvedValue(
      SAMPLE_SUGGESTION,
    );
    render(
      <WikiMaintenanceAssistant
        articlePath="team/people/sarah-chen.md"
        articleSha="abc1234"
        onApplied={vi.fn()}
      />,
    );
    fireEvent.click(screen.getByTestId("wk-maint-open"));
    fireEvent.click(screen.getByTestId("wk-maint-action-summarize"));
    await waitFor(() =>
      expect(screen.getByTestId("wk-maint-suggestion")).toBeInTheDocument(),
    );
    fireEvent.click(screen.getByTestId("wk-maint-reject"));

    const stored = window.localStorage.getItem(
      "wuphf:wiki-maint:rejected:abc1234:summarize",
    );
    expect(stored).not.toBeNull();
    expect(Number(stored)).toBeGreaterThan(0);
  });

  it("Snoozed actions render disabled with a 'snoozed' label", () => {
    window.localStorage.setItem(
      "wuphf:wiki-maint:rejected:abc1234:add_citation",
      String(Date.now()),
    );
    render(
      <WikiMaintenanceAssistant
        articlePath="team/people/sarah-chen.md"
        articleSha="abc1234"
        onApplied={vi.fn()}
      />,
    );
    fireEvent.click(screen.getByTestId("wk-maint-open"));
    const btn = screen.getByTestId("wk-maint-action-add_citation");
    expect(btn).toBeDisabled();
    expect(btn.textContent).toMatch(/snoozed/i);
  });
});

describe("<WikiMaintenanceAssistant> — content shapes", () => {
  it("Skipped suggestions show the reason instead of accept/diff", async () => {
    vi.spyOn(wikiApi, "fetchMaintenanceSuggestion").mockResolvedValue(
      SKIPPED_SUGGESTION,
    );
    render(
      <WikiMaintenanceAssistant
        articlePath="team/people/sarah-chen.md"
        articleSha="abc1234"
        onApplied={vi.fn()}
      />,
    );
    fireEvent.click(screen.getByTestId("wk-maint-open"));
    fireEvent.click(screen.getByTestId("wk-maint-action-split_long_page"));
    await waitFor(() =>
      expect(screen.getByTestId("wk-maint-skipped")).toBeInTheDocument(),
    );
    expect(screen.queryByTestId("wk-maint-accept")).toBeNull();
    expect(screen.getByText(/article is short/i)).toBeInTheDocument();
  });

  it("Extract facts renders the proposed triples list and never auto-commits", async () => {
    const fetchSpy = vi
      .spyOn(wikiApi, "fetchMaintenanceSuggestion")
      .mockResolvedValue(FACTS_SUGGESTION);
    const writeArticle = vi.fn();
    render(
      <WikiMaintenanceAssistant
        articlePath="team/people/sarah-chen.md"
        articleSha="abc1234"
        onApplied={vi.fn()}
        writeArticle={writeArticle}
      />,
    );
    fireEvent.click(screen.getByTestId("wk-maint-open"));
    fireEvent.click(screen.getByTestId("wk-maint-action-extract_facts"));
    await waitFor(() =>
      expect(screen.getByTestId("wk-maint-facts")).toBeInTheDocument(),
    );
    expect(fetchSpy).toHaveBeenCalledTimes(1);
    expect(writeArticle).not.toHaveBeenCalled();
    expect(screen.queryByTestId("wk-maint-accept")).toBeNull(); // facts have no diff
    expect(screen.getByText("role_at")).toBeInTheDocument();
  });

  it("Conflict response surfaces a recompute message instead of writing", async () => {
    vi.spyOn(wikiApi, "fetchMaintenanceSuggestion").mockResolvedValue(
      SAMPLE_SUGGESTION,
    );
    const writeArticle = vi.fn().mockResolvedValue({
      conflict: true,
      error: "stale",
      current_sha: "newer",
      current_content: "newer body",
    } as wikiApi.WriteHumanResult);
    render(
      <WikiMaintenanceAssistant
        articlePath="team/people/sarah-chen.md"
        articleSha="abc1234"
        onApplied={vi.fn()}
        writeArticle={writeArticle}
      />,
    );
    fireEvent.click(screen.getByTestId("wk-maint-open"));
    fireEvent.click(screen.getByTestId("wk-maint-action-summarize"));
    await waitFor(() =>
      expect(screen.getByTestId("wk-maint-accept")).toBeInTheDocument(),
    );
    fireEvent.click(screen.getByTestId("wk-maint-accept"));
    await waitFor(() =>
      expect(screen.getByTestId("wk-maint-apply-error")).toBeInTheDocument(),
    );
    expect(screen.getByText(/changed since/i)).toBeInTheDocument();
  });

  it("initialAction expands and pre-selects the action without a click", async () => {
    vi.spyOn(wikiApi, "fetchMaintenanceSuggestion").mockResolvedValue(
      SAMPLE_SUGGESTION,
    );
    render(
      <WikiMaintenanceAssistant
        articlePath="team/people/sarah-chen.md"
        articleSha="abc1234"
        onApplied={vi.fn()}
        initialAction="summarize"
      />,
    );
    expect(screen.getByTestId("wk-maint-panel")).toBeInTheDocument();
    await waitFor(() =>
      expect(screen.getByTestId("wk-maint-suggestion")).toBeInTheDocument(),
    );
  });
});

describe("maintenanceTarget hand-off", () => {
  beforeEach(() => {
    if (typeof window !== "undefined") {
      window.sessionStorage.clear();
    }
  });

  it("requestMaintenanceTarget + consumeMaintenanceTarget round-trips on slug match", () => {
    requestMaintenanceTarget("sarah-chen", "resolve_contradiction");
    expect(consumeMaintenanceTarget("team/people/sarah-chen.md")).toBe(
      "resolve_contradiction",
    );
  });

  it("consume returns null on slug mismatch and clears the slot", () => {
    requestMaintenanceTarget("sarah-chen", "resolve_contradiction");
    expect(consumeMaintenanceTarget("team/people/other.md")).toBeNull();
  });

  it("consume returns null after the TTL", () => {
    const stale = {
      slug: "sarah-chen",
      action: "resolve_contradiction",
      ts: Date.now() - 120_000,
    };
    window.sessionStorage.setItem(
      "wuphf:wiki-maint:target",
      JSON.stringify(stale),
    );
    expect(consumeMaintenanceTarget("team/people/sarah-chen.md")).toBeNull();
  });

  it("consume only fires once — second call returns null", () => {
    requestMaintenanceTarget("sarah-chen", "summarize");
    expect(consumeMaintenanceTarget("team/people/sarah-chen.md")).toBe(
      "summarize",
    );
    expect(consumeMaintenanceTarget("team/people/sarah-chen.md")).toBeNull();
  });
});
