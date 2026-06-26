import type { ReactNode } from "react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import type { OfficeStats } from "../../api/platform";
import * as platformApi from "../../api/platform";
import { useAppStore } from "../../stores/app";
import { TasksNavButton } from "./TasksNavButton";

vi.mock("../../routes/useCurrentRoute", async (importOriginal) => {
  const actual =
    await importOriginal<typeof import("../../routes/useCurrentRoute")>();
  return {
    ...actual,
    useCurrentApp: () => null,
  };
});

vi.mock("../../lib/notificationSound", () => ({
  playInboxDing: vi.fn(),
}));

const STATS: OfficeStats = {
  tasks: {
    backlog: 0,
    active: 1,
    blocked: 0,
    review: 0,
    needs_human: 1,
    done: 0,
    archive: 0,
  },
  requests: { blocking: 1, notices: 1 },
  inbox_attention: 11,
  wiki_articles: 3,
  agents_active: 1,
  generated_at: "2026-06-11T00:00:00Z",
};

function wrap(ui: ReactNode) {
  const client = new QueryClient({
    defaultOptions: {
      queries: { retry: false, refetchInterval: false, gcTime: 0 },
    },
  });
  return <QueryClientProvider client={client}>{ui}</QueryClientProvider>;
}

afterEach(() => {
  vi.restoreAllMocks();
  useAppStore.setState({ brokerConnected: false });
});

describe("<TasksNavButton>", () => {
  it("labels the primary Work entry as Tasks", () => {
    render(wrap(<TasksNavButton />));
    expect(screen.getByText("Tasks")).toBeInTheDocument();
  });

  it("renders the broker-computed inbox_attention as the badge (C1)", async () => {
    useAppStore.setState({ brokerConnected: true });
    vi.spyOn(platformApi, "getOfficeStats").mockResolvedValue(STATS);

    render(wrap(<TasksNavButton />));

    // The badge is the broker's number — the same attention roll-up that
    // used to live on the standalone Inbox button, now hosted on Tasks.
    await waitFor(() => {
      expect(screen.getByTestId("inbox-unread-badge")).toHaveTextContent("11");
    });
  });

  it("renders no badge while the count is unknown or zero", () => {
    render(wrap(<TasksNavButton />));
    expect(screen.queryByTestId("inbox-unread-badge")).toBeNull();
  });
});
