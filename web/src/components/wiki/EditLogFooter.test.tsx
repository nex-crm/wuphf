import { act, render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import * as api from "../../api/wiki";
import EditLogFooter from "./EditLogFooter";

function hasDuplicateKeyWarning(calls: unknown[][]) {
  return calls.some((args) =>
    args.some((arg) =>
      String(arg).includes("Encountered two children with the same key"),
    ),
  );
}

describe("<EditLogFooter>", () => {
  beforeEach(() => {
    vi.restoreAllMocks();
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

  it("deduplicates commit-shaped entries without duplicate React keys", () => {
    const errorSpy = vi.spyOn(console, "error").mockImplementation(() => {});
    const timestamp = new Date().toISOString();
    const initial: api.WikiEditLogEntry[] = [
      {
        who: "CEO",
        action: "edited",
        article_path: "people/customer-x",
        article_title: "Customer X",
        timestamp,
        commit_sha: "same",
      },
      {
        who: "PM",
        action: "edited",
        article_path: "people/customer-x",
        article_title: "Customer X",
        timestamp,
        commit_sha: "same",
      },
    ];

    render(<EditLogFooter initialEntries={initial} />);

    expect(screen.getAllByText("Customer X")).toHaveLength(1);
    expect(hasDuplicateKeyWarning(errorSpy.mock.calls)).toBe(false);
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

  it("hydrates entries from article history", async () => {
    vi.spyOn(api, "subscribeEditLog").mockImplementation(() => () => {});
    vi.spyOn(api, "fetchHistory").mockResolvedValue({
      commits: [
        {
          sha: "abc123",
          author_slug: "pm",
          msg: "wiki: update customer",
          date: new Date().toISOString(),
        },
      ],
    });

    render(<EditLogFooter historyPath="team/people/customer-x.md" />);

    expect(await screen.findByText("customer x")).toBeInTheDocument();
    expect(screen.getByText("pm")).toBeInTheDocument();
  });

  it("keeps live entries that arrive while history hydrates", async () => {
    type Handler = (e: api.WikiEditLogEntry) => void;
    let handler: Handler | null = null;
    vi.spyOn(api, "subscribeEditLog").mockImplementation((h: Handler) => {
      handler = h;
      return () => {};
    });
    let resolveHistory: (value: { commits: api.WikiHistoryCommit[] }) => void =
      () => {};
    vi.spyOn(api, "fetchHistory").mockReturnValue(
      new Promise((resolve) => {
        resolveHistory = resolve;
      }),
    );

    render(<EditLogFooter historyPath="team/people/customer-x.md" />);

    act(() => {
      handler?.({
        who: "Designer",
        action: "created",
        article_path: "team/brand/voice.md",
        article_title: "Brand Voice",
        timestamp: new Date().toISOString(),
        commit_sha: "live1",
      });
    });

    act(() => {
      resolveHistory({
        commits: [
          {
            sha: "hist1",
            author_slug: "pm",
            msg: "wiki: update customer",
            date: new Date().toISOString(),
          },
        ],
      });
    });

    expect(await screen.findByText("Brand Voice")).toBeInTheDocument();
    expect(await screen.findByText("customer x")).toBeInTheDocument();
  });
});
