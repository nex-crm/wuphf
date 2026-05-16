import type { ReactNode } from "react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor, within } from "@testing-library/react";
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
}));

describe("<MessageBubble> rich artifact references", () => {
  beforeEach(() => {
    vi.restoreAllMocks();
    vi.spyOn(richApi, "fetchRichArtifact").mockResolvedValue(ARTIFACT_DETAIL);
  });

  it("renders a compact artifact card and hides the raw marker line", async () => {
    renderWithQueryClient(<MessageBubble message={MESSAGE} />);

    expect(
      screen.getByText("I made the interactive strategy map."),
    ).toBeInTheDocument();
    expect(screen.queryByText(/visual-artifact:/)).toBeNull();

    const card = await screen.findByLabelText(
      "Rich artifact: Product strategy map",
    );
    expect(within(card).getByText("Notebook visual")).toBeInTheDocument();
    expect(within(card).getByText("draft")).toBeInTheDocument();
    expect(
      within(card).getByText("agents/pm/notebook/product-strategy.md"),
    ).toBeInTheDocument();
    expect(richApi.fetchRichArtifact).toHaveBeenCalledWith(
      "ra_0123456789abcdef",
    );
  });

  it("opens artifact HTML only inside the sandboxed modal frame", async () => {
    const user = userEvent.setup();
    renderWithQueryClient(<MessageBubble message={MESSAGE} />);

    const card = await screen.findByLabelText(
      "Rich artifact: Product strategy map",
    );
    expect(screen.queryByText("Artifact body")).toBeNull();

    await user.click(within(card).getByRole("button", { name: "Open" }));

    const dialog = screen.getByRole("dialog", {
      name: "Product strategy map",
    });
    const frame = within(dialog).getByTitle("Product strategy map");
    expect(frame).toHaveAttribute("sandbox", "allow-scripts");
    expect(frame).toHaveAttribute(
      "srcdoc",
      expect.stringContaining("<h1>Artifact body</h1>"),
    );
  });

  it("closes the artifact modal with Escape", async () => {
    const user = userEvent.setup();
    renderWithQueryClient(<MessageBubble message={MESSAGE} />);

    const card = await screen.findByLabelText(
      "Rich artifact: Product strategy map",
    );
    await user.click(within(card).getByRole("button", { name: "Open" }));
    expect(
      screen.getByRole("dialog", { name: "Product strategy map" }),
    ).toBeInTheDocument();

    await user.keyboard("{Escape}");

    await waitFor(() => {
      expect(
        screen.queryByRole("dialog", { name: "Product strategy map" }),
      ).toBeNull();
    });
  });
});
