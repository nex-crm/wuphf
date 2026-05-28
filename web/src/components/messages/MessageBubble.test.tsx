import type { ReactNode } from "react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import type { Message, OfficeMember } from "../../api/client";
import * as richApi from "../../api/richArtifacts";
import { MessageBubble } from "./MessageBubble";

const officeMembers: OfficeMember[] = [
  {
    slug: "pm",
    name: "Mara",
    role: "Product",
    built_in: false,
  } as OfficeMember,
];

const MESSAGE: Message = {
  id: "msg-1",
  from: "pm",
  channel: "general",
  content:
    "I made the interactive strategy map.\n\nvisual-artifact:ra_0123456789abcdef",
  timestamp: "2026-05-16T12:00:00Z",
};

const ARTIFACT_DETAIL: richApi.RichArtifactDetail = {
  artifact: {
    id: "ra_0123456789abcdef",
    kind: "notebook_html",
    title: "Product strategy map",
    summary: "A richer artifact for reviewing the WUPHF rollout.",
    trustLevel: "draft",
    representation: "html",
    htmlPath: "wiki/visual-artifacts/ra_0123456789abcdef.html",
    sourceMarkdownPath: "agents/pm/notebook/product-strategy.md",
    createdBy: "pm",
    createdAt: "2026-05-16T12:00:00Z",
    updatedAt: "2026-05-16T12:00:00Z",
    contentHash: "hash",
    sanitizerVersion: "sandbox-v1",
  },
  html: "<h1>Artifact body</h1>",
};

function renderWithQueryClient(ui: ReactNode) {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return render(
    <QueryClientProvider client={queryClient}>{ui}</QueryClientProvider>,
  );
}

vi.mock("../../hooks/useMembers", () => ({
  useOfficeMembers: () => ({ data: officeMembers, isLoading: false }),
}));

vi.mock("../../hooks/useConfig", () => ({
  useDefaultHarness: () => null,
}));

vi.mock("../../routes/useCurrentRoute", () => ({
  useChannelSlug: () => "general",
}));

describe("<MessageBubble> rich artifact references", () => {
  beforeEach(() => {
    vi.restoreAllMocks();
    vi.spyOn(richApi, "fetchRichArtifact").mockResolvedValue(ARTIFACT_DETAIL);
  });

  it("renders the artifact inline (no card chrome, no modal) and hides the raw marker line", async () => {
    renderWithQueryClient(<MessageBubble message={MESSAGE} />);

    expect(
      screen.getByText("I made the interactive strategy map."),
    ).toBeInTheDocument();
    expect(screen.queryByText(/visual-artifact:/)).toBeNull();

    // Embedded inline as a shadow-DOM web component — aria-label carries
    // the artifact title, no surrounding "Rich artifact: ..." chrome card.
    const embed = await screen.findByLabelText("Product strategy map", {
      selector: "rich-artifact-embed",
    });
    expect(embed.closest("figure")).not.toBeNull();
    expect(richApi.fetchRichArtifact).toHaveBeenCalledWith(
      "ra_0123456789abcdef",
    );

    // No Expand button, no NOTEBOOK VISUAL kicker, no modal dialog.
    expect(screen.queryByRole("button", { name: "Expand" })).toBeNull();
    expect(screen.queryByText("Notebook visual")).toBeNull();
    expect(screen.queryByRole("dialog")).toBeNull();
  });

  it("does not render an iframe for the artifact", async () => {
    renderWithQueryClient(<MessageBubble message={MESSAGE} />);
    await screen.findByLabelText("Product strategy map", {
      selector: "rich-artifact-embed",
    });
    await waitFor(() => {
      expect(document.querySelector("iframe")).toBeNull();
    });
  });
});
