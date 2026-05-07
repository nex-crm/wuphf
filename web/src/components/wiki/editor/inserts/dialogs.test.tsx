/**
 * Component tests for the insert dialogs.
 *
 * Covers the contracts that the phase doc commits to:
 *   - Fact dialog never auto-writes; the preview stage is mandatory.
 *   - Citation dialog requires a URL.
 *   - Decision dialog defaults the date and validates required fields.
 *   - Related dialog stitches a `## Related` block from picked entries.
 *
 * The dialogs are framework-agnostic — they don't touch ProseMirror
 * directly — so we can render them in isolation with happy-dom.
 */
import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import { CitationDialog } from "./CitationDialog";
import { DecisionDialog } from "./DecisionDialog";
import { FactDialog } from "./FactDialog";
import type { MentionItem } from "./mentionCatalog";
import { RelatedDialog } from "./RelatedDialog";

describe("<CitationDialog>", () => {
  it("rejects an empty URL and surfaces an error", () => {
    const onConfirm = vi.fn();
    render(
      <CitationDialog
        currentMarkdown=""
        onConfirm={onConfirm}
        onCancel={() => {}}
      />,
    );
    fireEvent.click(screen.getByTestId("wk-citation-confirm"));
    expect(screen.getByTestId("wk-citation-error")).toBeInTheDocument();
    expect(onConfirm).not.toHaveBeenCalled();
  });

  it("emits a footnote pair with an allocated id", () => {
    const onConfirm = vi.fn();
    render(
      <CitationDialog
        currentMarkdown="Body has [^1] in use already."
        onConfirm={onConfirm}
        onCancel={() => {}}
      />,
    );
    fireEvent.change(screen.getByTestId("wk-citation-url"), {
      target: { value: "https://example.com" },
    });
    fireEvent.change(screen.getByTestId("wk-citation-title"), {
      target: { value: "Source" },
    });
    fireEvent.click(screen.getByTestId("wk-citation-confirm"));
    expect(onConfirm).toHaveBeenCalledTimes(1);
    const built = onConfirm.mock.calls[0][0];
    expect(built.reference).toBe("[^2]");
    expect(built.definition).toContain("[^2]: Source - https://example.com");
  });
});

describe("<FactDialog> — review-required contract", () => {
  it("does not call onConfirm from the edit stage even when fields are valid", () => {
    const onConfirm = vi.fn();
    render(<FactDialog onConfirm={onConfirm} onCancel={() => {}} />);
    fireEvent.change(screen.getByTestId("wk-fact-subject"), {
      target: { value: "alex" },
    });
    fireEvent.change(screen.getByTestId("wk-fact-predicate"), {
      target: { value: "works_at" },
    });
    fireEvent.change(screen.getByTestId("wk-fact-object"), {
      target: { value: "nex" },
    });
    fireEvent.click(screen.getByTestId("wk-fact-preview"));
    // Now in preview stage — preview block visible but onConfirm has not run.
    expect(screen.getByTestId("wk-fact-preview-block")).toHaveTextContent(
      /subject: alex/,
    );
    expect(onConfirm).not.toHaveBeenCalled();
  });

  it("only writes the fact after the user confirms the preview", () => {
    const onConfirm = vi.fn();
    render(<FactDialog onConfirm={onConfirm} onCancel={() => {}} />);
    fireEvent.change(screen.getByTestId("wk-fact-subject"), {
      target: { value: "alex" },
    });
    fireEvent.change(screen.getByTestId("wk-fact-predicate"), {
      target: { value: "works_at" },
    });
    fireEvent.change(screen.getByTestId("wk-fact-object"), {
      target: { value: "nex" },
    });
    fireEvent.click(screen.getByTestId("wk-fact-preview"));
    fireEvent.click(screen.getByTestId("wk-fact-confirm"));
    expect(onConfirm).toHaveBeenCalledTimes(1);
    expect(onConfirm.mock.calls[0][0]).toContain("```fact");
    expect(onConfirm.mock.calls[0][0]).toContain("subject: alex");
  });

  it("rejects empty fields with a banner instead of advancing", () => {
    const onConfirm = vi.fn();
    render(<FactDialog onConfirm={onConfirm} onCancel={() => {}} />);
    fireEvent.click(screen.getByTestId("wk-fact-preview"));
    expect(screen.getByTestId("wk-fact-error")).toBeInTheDocument();
    expect(onConfirm).not.toHaveBeenCalled();
  });
});

describe("<DecisionDialog>", () => {
  it("requires a non-empty title and rationale", () => {
    const onConfirm = vi.fn();
    render(<DecisionDialog onConfirm={onConfirm} onCancel={() => {}} />);
    fireEvent.click(screen.getByTestId("wk-decision-confirm"));
    expect(screen.getByTestId("wk-decision-error")).toBeInTheDocument();
    expect(onConfirm).not.toHaveBeenCalled();
  });

  it("emits a decision block with the user's inputs", () => {
    const onConfirm = vi.fn();
    render(<DecisionDialog onConfirm={onConfirm} onCancel={() => {}} />);
    fireEvent.change(screen.getByTestId("wk-decision-title"), {
      target: { value: "Adopt Milkdown" },
    });
    fireEvent.change(screen.getByTestId("wk-decision-rationale"), {
      target: { value: "Round-trip-clean wikilinks." },
    });
    fireEvent.change(screen.getByTestId("wk-decision-alternatives"), {
      target: { value: "TipTap, ProseMirror raw" },
    });
    fireEvent.click(screen.getByTestId("wk-decision-confirm"));
    expect(onConfirm).toHaveBeenCalledTimes(1);
    const block = onConfirm.mock.calls[0][0];
    expect(block).toContain("```decision");
    expect(block).toContain("title: Adopt Milkdown");
    expect(block).toContain("rationale: Round-trip-clean wikilinks.");
    expect(block).toContain("alternatives: TipTap, ProseMirror raw");
  });
});

describe("<RelatedDialog>", () => {
  const items: MentionItem[] = [
    { slug: "team/people/alex", title: "Alex Chen", category: "people" },
    { slug: "team/people/sarah", title: "Sarah Lee", category: "people" },
    {
      slug: "team/projects/backend",
      title: "Backend rewrite",
      category: "projects",
    },
  ];

  it("disables Insert until at least one entry is picked", () => {
    const onConfirm = vi.fn();
    render(
      <RelatedDialog items={items} onConfirm={onConfirm} onCancel={() => {}} />,
    );
    const button = screen.getByTestId("wk-related-confirm");
    expect(button).toBeDisabled();
    fireEvent.click(screen.getByTestId("wk-related-check-team/people/alex"));
    expect(button).not.toBeDisabled();
  });

  it("emits a Related block with picked wikilinks", () => {
    const onConfirm = vi.fn();
    render(
      <RelatedDialog items={items} onConfirm={onConfirm} onCancel={() => {}} />,
    );
    fireEvent.click(screen.getByTestId("wk-related-check-team/people/alex"));
    fireEvent.click(
      screen.getByTestId("wk-related-check-team/projects/backend"),
    );
    fireEvent.click(screen.getByTestId("wk-related-confirm"));
    expect(onConfirm).toHaveBeenCalledTimes(1);
    const block = onConfirm.mock.calls[0][0];
    expect(block).toContain("## Related");
    expect(block).toContain("- [[team/people/alex|Alex Chen]]");
    expect(block).toContain("- [[team/projects/backend|Backend rewrite]]");
  });
});
