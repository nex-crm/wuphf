import type { ReactNode } from "react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
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
  useCurrentTaskId: () => null,
}));

describe("<MessageBubble> rich artifact references", () => {
  beforeEach(() => {
    vi.restoreAllMocks();
    vi.spyOn(richApi, "fetchRichArtifact").mockResolvedValue(ARTIFACT_DETAIL);
  });

  it("renders a clickable article link card (no inline embed, no iframe) and hides the raw marker line", async () => {
    renderWithQueryClient(<MessageBubble message={MESSAGE} />);

    expect(
      screen.getByText("I made the interactive strategy map."),
    ).toBeInTheDocument();
    expect(screen.queryByText(/visual-artifact:/)).toBeNull();

    // Clickable card with title + summary + Open action.
    const card = await screen.findByRole("button", {
      name: "Open article: Product strategy map",
    });
    expect(card.tagName.toLowerCase()).toBe("button");
    expect(card).toHaveTextContent("Product strategy map");
    expect(card).toHaveTextContent(
      "A richer artifact for reviewing the WUPHF rollout.",
    );
    expect(card).toHaveTextContent("Open article →");
    expect(richApi.fetchRichArtifact).toHaveBeenCalledWith(
      "ra_0123456789abcdef",
    );

    // Inline-embed UX is gone: no shadow-DOM mount in the bubble, no iframe,
    // no modal dialog, no Expand button.
    expect(document.querySelector("rich-artifact-embed")).toBeNull();
    expect(document.querySelector("iframe")).toBeNull();
    expect(screen.queryByRole("button", { name: "Expand" })).toBeNull();
    expect(screen.queryByRole("dialog")).toBeNull();
  });

  it("the link card navigates to /articles/$id when clicked", async () => {
    const user = userEvent.setup();
    renderWithQueryClient(<MessageBubble message={MESSAGE} />);
    const card = await screen.findByRole("button", {
      name: "Open article: Product strategy map",
    });
    await user.click(card);
    await waitFor(() => {
      expect(window.location.hash).toBe("#/articles/ra_0123456789abcdef");
    });
  });
});
