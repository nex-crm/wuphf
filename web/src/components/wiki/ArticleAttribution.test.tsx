import type { ReactNode } from "react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

vi.mock("../../api/attribution", () => ({
  fetchArticleAttribution: vi.fn(),
}));

import { fetchArticleAttribution } from "../../api/attribution";
import { ArticleAttribution } from "./ArticleAttribution";

const mocked = vi.mocked(fetchArticleAttribution);

function wrap(ui: ReactNode) {
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return <QueryClientProvider client={qc}>{ui}</QueryClientProvider>;
}

describe("<ArticleAttribution>", () => {
  it("renders a Produced-for link to the task", async () => {
    mocked.mockResolvedValue({
      taskId: "OFFICE-1",
      taskTitle: "Q2 pricing launch",
      owner: "revops",
    });
    render(wrap(<ArticleAttribution articleRef="ra_abc" />));
    const link = await screen.findByTestId("article-attribution");
    expect(link).toHaveAttribute("href", "#/tasks/OFFICE-1");
    expect(link).toHaveTextContent("Q2 pricing launch");
    expect(link).toHaveTextContent("@revops");
  });

  it("renders nothing when there is no producing task", async () => {
    mocked.mockResolvedValue(null);
    const { container } = render(
      wrap(<ArticleAttribution articleRef="ra_x" />),
    );
    await waitFor(() => {
      expect(mocked).toHaveBeenCalled();
    });
    expect(screen.queryByTestId("article-attribution")).toBeNull();
    expect(container.querySelector(".article-attribution")).toBeNull();
  });
});
