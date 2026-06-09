import { render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import * as richApi from "../../api/richArtifacts";
import { TaskDescription } from "./TaskDescription";

// TaskDescription rendering rules:
//
//  1. Plain markdown bodies render through ReactMarkdown unchanged.
//
//  2. A `visual-artifact:<id>` marker in the description is stripped from
//     the markdown body and the underlying RichArtifactEmbed renders inline
//     above whatever prose remains.
//
//  3. A marker whose artifact 404s (or otherwise fails) leaves a clean body
//     with no leaked raw `visual-artifact:` text. The stripped marker keeps
//     literal text out of the body either way; the embed simply degrades to
//     nothing visible.
//
//  4. Multiple markers embed in document order so the agent can interleave
//     several artifacts without the FE collapsing them.

const DRAFT_DETAIL = (
  id: string,
  title: string,
): richApi.RichArtifactDetail => ({
  artifact: {
    id,
    kind: "notebook_html",
    title,
    summary: "",
    trustLevel: "draft",
    representation: "html",
    htmlPath: `wiki/visual-artifacts/${id}.html`,
    createdBy: "ceo",
    createdAt: new Date().toISOString(),
    updatedAt: new Date().toISOString(),
    contentHash: `hash-${id}`,
    sanitizerVersion: "sandbox-v2",
  },
  html: `<svg aria-label='inline-${id}'></svg>`,
});

beforeEach(() => {
  vi.restoreAllMocks();
  // Default the by-id artifact fetch to a 404 so descriptions without an
  // inline marker never accidentally embed one. Tests that exercise the
  // embed override this per-call.
  vi.spyOn(richApi, "fetchRichArtifact").mockRejectedValue(
    new Error("404 not found"),
  );
});

describe("<TaskDescription>", () => {
  it("renders a plain markdown description without any embed", async () => {
    render(
      <TaskDescription
        description="**Pilot scope** — clarify rollout for week 3."
        isDrafting={true}
      />,
    );
    expect(
      await screen.findByText(/clarify rollout for week 3/i),
    ).toBeInTheDocument();
    expect(screen.queryByTestId("rich-artifact-embed")).toBeNull();
  });

  it("renders the empty state when description is whitespace-only", () => {
    render(<TaskDescription description={"   \n  "} isDrafting={true} />);
    expect(screen.getByText(/No description yet\./i)).toBeInTheDocument();
  });

  it("strips the marker and embeds the artifact inline above the prose", async () => {
    const id = "ra_0123456789abcdef";
    const fetchSpy = vi
      .spyOn(richApi, "fetchRichArtifact")
      .mockResolvedValue(DRAFT_DETAIL(id, "Pilot Spec"));

    render(
      <TaskDescription
        description={`Short summary of the work.\n\nvisual-artifact:${id}\n\nOpen question: ship gating?`}
        isDrafting={true}
      />,
    );

    const body = await screen.findByTestId("issue-doc-description-body");
    const embed = await screen.findByTestId("rich-artifact-embed");
    expect(body.contains(embed)).toBe(true);
    expect(fetchSpy).toHaveBeenCalledWith(id);

    // Marker text never leaks into the rendered body.
    expect(body.textContent ?? "").not.toContain("visual-artifact:");
    expect(body.textContent ?? "").not.toContain(id);

    // Surrounding prose stays.
    expect(body.textContent ?? "").toContain("Short summary of the work.");
    expect(body.textContent ?? "").toContain("Open question: ship gating?");
  });

  it("hides a marker whose artifact 404s without leaking the raw text", async () => {
    // beforeEach default (rejection) drives the 404 here — no extra spy.
    const id = "ra_dead0000beef0000";
    render(
      <TaskDescription
        description={`Header line.\n\nvisual-artifact:${id}\n\nTail line.`}
        isDrafting={false}
      />,
    );

    const body = await screen.findByTestId("issue-doc-description-body");
    await waitFor(() => {
      expect(body.textContent ?? "").toContain("Header line.");
    });
    expect(body.textContent ?? "").not.toContain("visual-artifact:");
    expect(body.textContent ?? "").not.toContain(id);
    expect(screen.queryByTestId("rich-artifact-embed")).toBeNull();
  });

  it("embeds multiple markers in document order", async () => {
    const idA = "ra_aaaaaaaa00000001";
    const idB = "ra_bbbbbbbb00000002";
    const detailA = DRAFT_DETAIL(idA, "Figure A");
    const detailB = DRAFT_DETAIL(idB, "Figure B");
    vi.spyOn(richApi, "fetchRichArtifact").mockImplementation(
      async (id: string) => {
        if (id === idA) return detailA;
        if (id === idB) return detailB;
        throw new Error("404 not found");
      },
    );

    render(
      <TaskDescription
        description={`visual-artifact:${idA}\n\nMid copy.\n\nvisual-artifact:${idB}`}
        isDrafting={true}
      />,
    );

    const body = await screen.findByTestId("issue-doc-description-body");
    // Wait for both embeds to settle.
    await waitFor(() => {
      expect(
        body.querySelectorAll('[data-testid="rich-artifact-embed"]').length,
      ).toBe(2);
    });
    const embeds = Array.from(
      body.querySelectorAll('[data-testid="rich-artifact-embed"]'),
    ) as HTMLElement[];
    expect(embeds[0].getAttribute("aria-label")).toBe(detailA.artifact.title);
    expect(embeds[1].getAttribute("aria-label")).toBe(detailB.artifact.title);
    expect(body.textContent ?? "").toContain("Mid copy.");
  });
});
