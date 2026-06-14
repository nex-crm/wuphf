import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import type { OfficeMember } from "../../api/client";

const mutateAsync = vi.fn(
  async (_input: Record<string, unknown>) => ({ task: { id: "task-1" } }),
);

vi.mock("../../hooks/useCreateTask", () => ({
  useCreateTask: () => ({ mutateAsync, isPending: false }),
}));

const SAMPLE_MEMBERS: OfficeMember[] = [
  { slug: "ceo", name: "CEO", role: "supervisor", emoji: "👔" },
  { slug: "bookkeeper", name: "Bookkeeper", role: "specialist", emoji: "📒" },
];

vi.mock("../../hooks/useMembers", () => ({
  useOfficeMembers: () => ({ data: SAMPLE_MEMBERS, isPending: false }),
}));

vi.mock("../../api/client", () => ({
  getConfig: async () => ({ team_lead_slug: "ceo" }),
}));

vi.mock("../../lib/router", () => ({
  router: { navigate: vi.fn() },
}));

import { TaskCreateDialog } from "./TaskCreateDialog";

function renderDialog() {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return render(
    <QueryClientProvider client={client}>
      <TaskCreateDialog open={true} onOpenChange={() => {}} />
    </QueryClientProvider>,
  );
}

describe("<TaskCreateDialog>", () => {
  beforeEach(() => {
    mutateAsync.mockClear();
  });

  it("does not render a channel selector (channels are no longer user-facing)", () => {
    renderDialog();

    // Regression: the modal used to expose a "#channel" picker chip. Channels
    // are no longer a creation-time choice — every task gets its own channel.
    expect(screen.queryByTestId("issue-create-channel")).toBeNull();
    expect(
      screen.queryByRole("combobox", { name: /channel/i }),
    ).toBeNull();
    // The assignee chip is still the one remaining property control.
    expect(screen.getByTestId("issue-create-assignee")).toBeInTheDocument();
  });

  it("creates a task without sending a user-chosen channel", async () => {
    renderDialog();

    fireEvent.change(screen.getByTestId("issue-create-title"), {
      target: { value: "Reconcile the books" },
    });
    fireEvent.click(screen.getByTestId("issue-create-submit"));

    await waitFor(() => expect(mutateAsync).toHaveBeenCalledTimes(1));
    const payload = mutateAsync.mock.calls[0][0] as Record<string, unknown>;
    expect(payload).not.toHaveProperty("channel");
    expect(payload.title).toBe("Reconcile the books");
  });
});
