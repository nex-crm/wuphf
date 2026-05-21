// @vitest-environment happy-dom

import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { WorkBoard } from "../../../src/renderer/work-board/WorkBoard.tsx";

import { jsonResponse, renderWithProviders } from "../test-utils.tsx";

import { sampleThreadView, threadListWireFromViews } from "./fixtures.ts";

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("WorkBoard", () => {
  it("renders the loading skeleton before the query resolves", () => {
    vi.stubGlobal(
      "fetch",
      vi.fn<typeof fetch>(() => new Promise<Response>(() => undefined)),
    );
    renderWithProviders(<WorkBoard />);

    expect(screen.getByTestId("work-board-skeleton")).toBeInTheDocument();
    expect(screen.getByText(/Loading threads/i)).toBeInTheDocument();
  });

  it("renders the error state and refetches when retry is clicked", async () => {
    const fetchMock = vi.fn<typeof fetch>();
    fetchMock.mockResolvedValueOnce(jsonResponse({ error: "broker offline" }, 500));
    fetchMock.mockResolvedValueOnce(
      jsonResponse(
        threadListWireFromViews([
          sampleThreadView({ boardColumn: "running", title: "After retry" }),
        ]),
      ),
    );
    vi.stubGlobal("fetch", fetchMock);

    renderWithProviders(<WorkBoard />);

    expect(await screen.findByText(/Could not load threads/i)).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: /Retry/i }));

    expect(await screen.findByText("After retry")).toBeInTheDocument();
    expect(fetchMock).toHaveBeenCalledTimes(2);
  });

  it("renders four columns with threads bucketed by boardColumn", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn<typeof fetch>(() =>
        Promise.resolve(
          jsonResponse(
            threadListWireFromViews([
              sampleThreadView({ boardColumn: "needs_me", title: "Approve this" }),
              sampleThreadView({ boardColumn: "running", title: "Migrating schema" }),
              sampleThreadView({ boardColumn: "running", title: "Recompiling broker" }),
              sampleThreadView({ boardColumn: "review", title: "Ready for review" }),
              sampleThreadView({ boardColumn: "done", title: "Merged thing" }),
            ]),
          ),
        ),
      ),
    );

    renderWithProviders(<WorkBoard />);

    await waitFor(() => {
      expect(screen.getAllByTestId("work-board-column")).toHaveLength(4);
    });

    const needsMe = screen.getByRole("region", { name: /needs me/i });
    expect(within(needsMe).getByText("Approve this")).toBeInTheDocument();

    const running = screen.getByRole("region", { name: /running/i });
    expect(within(running).getAllByTestId("thread-card")).toHaveLength(2);

    const review = screen.getByRole("region", { name: /review/i });
    expect(within(review).getByText("Ready for review")).toBeInTheDocument();

    const done = screen.getByRole("region", { name: /done/i });
    expect(within(done).getByText("Merged thing")).toBeInTheDocument();

    expect(screen.getByText("5 threads")).toBeInTheDocument();
  });

  it("renders 'no threads' counters when the broker returns an empty list", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn<typeof fetch>(() =>
        Promise.resolve(jsonResponse(threadListWireFromViews([]))),
      ),
    );
    renderWithProviders(<WorkBoard />);

    expect(await screen.findByText("0 threads")).toBeInTheDocument();
    expect(screen.getByText(/Nothing waiting on you/i)).toBeInTheDocument();
  });
});
