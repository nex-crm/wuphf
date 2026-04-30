import { act, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import * as api from "../../api/wiki";
import EditLogFooter from "./EditLogFooter";

describe("<EditLogFooter>", () => {
  beforeEach(() => {
    vi.restoreAllMocks();
    vi.spyOn(api, "subscribeEditLog").mockImplementation(() => () => {});
  });

  it("renders initial entries with the newest marked live", () => {
    const initial: api.WikiEditLogEntry[] = [
      {
        who: "CEO",
        action: "edited",
        article_path: "people/customer-x",
        article_title: "Customer X",
        timestamp: new Date().toISOString(),
        commit_sha: "a",
      },
      {
        who: "PM",
        action: "updated",
        article_path: "playbooks/churn",
        article_title: "Churn",
        timestamp: new Date().toISOString(),
        commit_sha: "b",
      },
    ];
    render(<EditLogFooter initialEntries={initial} />);
    const live = screen.getByTestId("wk-live-entry");
    expect(live).toHaveClass("wk-live");
    expect(screen.getByText("Customer X")).toBeInTheDocument();
    expect(screen.getByText("Churn")).toBeInTheDocument();
  });

  it("invokes onNavigate on entry click instead of navigating", () => {
    const onNavigate = vi.fn();
    vi.spyOn(api, "subscribeEditLog").mockImplementation(() => () => {});
    const initial: api.WikiEditLogEntry[] = [
      {
        who: "CEO",
        action: "edited",
        article_path: "people/x",
        article_title: "X",
        timestamp: new Date().toISOString(),
        commit_sha: "a",
      },
    ];
    render(<EditLogFooter initialEntries={initial} onNavigate={onNavigate} />);
    const link = screen.getByText("X");
    link.click();
    expect(onNavigate).toHaveBeenCalledWith("people/x");
  });

  it("prepends new entries from the subscription", async () => {
    // Arrange: capture handler and push to it during the test.
    type Handler = (e: api.WikiEditLogEntry) => void;
    let handler: Handler | null = null;
    vi.spyOn(api, "subscribeEditLog").mockImplementation((h: Handler) => {
      handler = h;
      return () => {};
    });
    const initial: api.WikiEditLogEntry[] = [
      {
        who: "PM",
        action: "updated",
        article_path: "playbooks/churn",
        article_title: "Churn",
        timestamp: new Date().toISOString(),
        commit_sha: "b",
      },
    ];
    // Act
    render(<EditLogFooter initialEntries={initial} />);
    act(() => {
      handler?.({
        who: "Designer",
        action: "created",
        article_path: "brand/voice",
        article_title: "Brand Voice",
        timestamp: new Date().toISOString(),
        commit_sha: "c",
      });
    });
    // Assert
    const live = screen.getByTestId("wk-live-entry");
    expect(live).toHaveTextContent("Brand Voice");
    expect(screen.getByText("Churn")).toBeInTheDocument();
  });

  it("hydrates from real article history and subscribes to live edits", async () => {
    const subscribe = vi
      .spyOn(api, "subscribeEditLog")
      .mockImplementation(() => () => {});
    vi.spyOn(api, "fetchHistory").mockResolvedValue({
      commits: [
        {
          sha: "abc123",
          author_slug: "archivist",
          msg: "wiki: update passport application",
          date: "2026-04-29T10:00:00Z",
        },
      ],
    });

    render(
      <EditLogFooter historyPath="team/playbooks/passport-application.md" />,
    );

    await waitFor(() => {
      expect(screen.getByText("passport application")).toBeInTheDocument();
    });
    expect(subscribe).toHaveBeenCalledOnce();
  });

  it("keeps live edits that arrive while history is hydrating", async () => {
    type Handler = (e: api.WikiEditLogEntry) => void;
    let handler: Handler | null = null;
    vi.spyOn(api, "subscribeEditLog").mockImplementation((h: Handler) => {
      handler = h;
      return () => {};
    });
    let resolveHistory:
      | ((value: { commits: api.WikiHistoryCommit[] }) => void)
      | null = null;
    vi.spyOn(api, "fetchHistory").mockImplementation(
      () =>
        new Promise((resolve) => {
          resolveHistory = resolve;
        }),
    );

    render(<EditLogFooter historyPath="team/playbooks/live-window.md" />);

    expect(api.subscribeEditLog).toHaveBeenCalledOnce();
    act(() => {
      handler?.({
        who: "Designer",
        action: "edited",
        article_path: "team/playbooks/live-window.md",
        article_title: "live window",
        timestamp: "2026-04-29T10:01:00Z",
        commit_sha: "live123",
      });
    });

    expect(screen.getByText("live window")).toBeInTheDocument();

    await act(async () => {
      resolveHistory?.({
        commits: [
          {
            sha: "hist123",
            author_slug: "archivist",
            msg: "wiki: update live window",
            date: "2026-04-29T10:00:00Z",
          },
        ],
      });
    });

    await waitFor(() => {
      expect(screen.getAllByText("live window").length).toBeGreaterThan(0);
      expect(screen.getByText("archivist")).toBeInTheDocument();
    });
  });

  it("clears hydrated history when leaving an article route", async () => {
    vi.spyOn(api, "fetchHistory").mockResolvedValue({
      commits: [
        {
          sha: "passport123",
          author_slug: "archivist",
          msg: "wiki: update passport application",
          date: "2026-04-29T10:00:00Z",
        },
      ],
    });

    const { rerender } = render(
      <EditLogFooter historyPath="team/playbooks/passport-application.md" />,
    );

    await waitFor(() => {
      expect(screen.getByText("passport application")).toBeInTheDocument();
    });

    rerender(<EditLogFooter historyPath={null} />);

    await waitFor(() => {
      expect(
        screen.queryByText("passport application"),
      ).not.toBeInTheDocument();
    });
  });
});
