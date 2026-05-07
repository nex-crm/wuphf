import { StrictMode, useEffect, useRef, useState } from "react";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import type { WikiMaintenanceAction } from "../../api/wiki";
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
      "wuphf:wiki-maint:rejected:team/people/sarah-chen.md:summarize",
    );
    expect(stored).not.toBeNull();
    expect(Number(stored)).toBeGreaterThan(0);
  });

  it("Snoozed actions render disabled with a 'snoozed' label", () => {
    window.localStorage.setItem(
      "wuphf:wiki-maint:rejected:team/people/sarah-chen.md:add_citation",
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

describe("<WikiMaintenanceAssistant> — snooze + path scoping", () => {
  it("Snoozed actions stay snoozed across SHA changes for the same article", () => {
    // Reject was recorded for the article path; switching SHA must not
    // un-snooze the action — the user's "no, do not bug me" decision is
    // about the page, not the commit.
    window.localStorage.setItem(
      "wuphf:wiki-maint:rejected:team/people/sarah-chen.md:summarize",
      String(Date.now()),
    );
    const { rerender } = render(
      <WikiMaintenanceAssistant
        articlePath="team/people/sarah-chen.md"
        articleSha="sha-A"
        onApplied={vi.fn()}
      />,
    );
    fireEvent.click(screen.getByTestId("wk-maint-open"));
    expect(screen.getByTestId("wk-maint-action-summarize")).toBeDisabled();

    rerender(
      <WikiMaintenanceAssistant
        articlePath="team/people/sarah-chen.md"
        articleSha="sha-B"
        onApplied={vi.fn()}
      />,
    );
    expect(screen.getByTestId("wk-maint-action-summarize")).toBeDisabled();
  });

  it("articlePath change clears suggestion state and ignores stale responses", async () => {
    let resolveA: (s: wikiApi.WikiMaintenanceSuggestion) => void = () => {};
    const pendingA = new Promise<wikiApi.WikiMaintenanceSuggestion>((r) => {
      resolveA = r;
    });
    const fetchSpy = vi
      .spyOn(wikiApi, "fetchMaintenanceSuggestion")
      .mockImplementationOnce(() => pendingA)
      .mockResolvedValueOnce({
        ...SAMPLE_SUGGESTION,
        description: "B suggestion",
      });

    const { rerender } = render(
      <WikiMaintenanceAssistant
        articlePath="team/people/article-a.md"
        articleSha="sha-A"
        onApplied={vi.fn()}
      />,
    );
    fireEvent.click(screen.getByTestId("wk-maint-open"));
    fireEvent.click(screen.getByTestId("wk-maint-action-summarize"));
    expect(screen.getByTestId("wk-maint-loading")).toBeInTheDocument();

    // Navigate to article B before A's request has resolved.
    rerender(
      <WikiMaintenanceAssistant
        articlePath="team/people/article-b.md"
        articleSha="sha-B"
        onApplied={vi.fn()}
      />,
    );
    // No active action on the new path.
    expect(screen.queryByTestId("wk-maint-suggestion")).toBeNull();
    expect(screen.queryByTestId("wk-maint-loading")).toBeNull();

    // Resolve A's response — must be ignored because the path changed.
    resolveA({ ...SAMPLE_SUGGESTION, description: "A response (stale)" });
    await Promise.resolve();
    expect(screen.queryByText(/A response \(stale\)/)).toBeNull();

    // Activating an action on B fetches fresh against B.
    fireEvent.click(screen.getByTestId("wk-maint-action-summarize"));
    await waitFor(() =>
      expect(screen.getByText(/B suggestion/)).toBeInTheDocument(),
    );
    expect(fetchSpy).toHaveBeenLastCalledWith(
      "summarize",
      "team/people/article-b.md",
    );
  });
});

// Mirrors WikiArticle's ArticleRightSidebar consume pattern: a ref-guarded
// effect that captures the hand-off on first mount-per-path so React 19
// StrictMode's intentional double-invoke does not clear the slot before
// state has had a chance to record it.
function ConsumerHarness({ articlePath }: { articlePath: string }) {
  const [target, setTarget] = useState<WikiMaintenanceAction | undefined>(
    undefined,
  );
  const consumedRef = useRef<string | null>(null);
  useEffect(() => {
    if (consumedRef.current === articlePath) return;
    consumedRef.current = articlePath;
    const next = consumeMaintenanceTarget(articlePath) ?? undefined;
    if (next) setTarget(next);
  }, [articlePath]);
  return <div data-testid="harness-target">{target ?? "none"}</div>;
}

describe("WikiArticle hand-off consumption", () => {
  it("StrictMode double-render does not lose the hand-off", async () => {
    requestMaintenanceTarget("sarah-chen", "summarize");
    render(
      <StrictMode>
        <ConsumerHarness articlePath="team/people/sarah-chen.md" />
      </StrictMode>,
    );
    await waitFor(() =>
      expect(screen.getByTestId("harness-target").textContent).toBe(
        "summarize",
      ),
    );
    // After consumption the slot is empty so a fresh mount sees nothing.
    expect(window.sessionStorage.getItem("wuphf:wiki-maint:target")).toBeNull();
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
    // Slot is cleared even on mismatch so a later correct-slug consume
    // does not surface a stale hand-off.
    expect(window.sessionStorage.getItem("wuphf:wiki-maint:target")).toBeNull();
    expect(consumeMaintenanceTarget("team/people/sarah-chen.md")).toBeNull();
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
