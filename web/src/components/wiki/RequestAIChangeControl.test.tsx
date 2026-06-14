import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";

import type { Task } from "../../api/tasks";
import * as tasksApi from "../../api/tasks";
import RequestAIChangeControl, {
  buildWikiChangeTask,
  LIBRARIAN_SLUG,
} from "./RequestAIChangeControl";

const ARTICLE_TITLE = "Acme Corp";
const ARTICLE_PATH = "team/accounts/acme-corp.md";

function mkTask(id: string): Task {
  return { id, title: `Update wiki article: ${ARTICLE_TITLE}` } as Task;
}

describe("buildWikiChangeTask", () => {
  it("composes the librarian task with the instruction + path + related-items line", () => {
    const task = buildWikiChangeTask(
      ARTICLE_TITLE,
      ARTICLE_PATH,
      "Refresh the pricing table.",
    );
    expect(task.title).toBe("Update wiki article: Acme Corp");
    expect(task.assignee).toBe(LIBRARIAN_SLUG);
    expect(task.details).toContain("Refresh the pricing table.");
    expect(task.details).toContain(ARTICLE_PATH);
    expect(task.details).toContain(
      "related items (linked articles, the index)",
    );
  });
});

describe("<RequestAIChangeControl>", () => {
  beforeEach(() => {
    vi.restoreAllMocks();
  });

  it("opens the modal from the toolbar button", async () => {
    render(
      <RequestAIChangeControl title={ARTICLE_TITLE} path={ARTICLE_PATH} />,
    );
    await userEvent.setup().click(screen.getByTestId("wk-article-ai-change"));
    expect(
      screen.getByTestId("wk-article-ai-change-modal"),
    ).toBeInTheDocument();
    expect(screen.getByTestId("wk-ai-change-input")).toBeInTheDocument();
  });

  it("requires the instruction text before creating the task", async () => {
    const createSpy = vi.spyOn(tasksApi, "createTasks");
    render(
      <RequestAIChangeControl title={ARTICLE_TITLE} path={ARTICLE_PATH} />,
    );
    const user = userEvent.setup();
    await user.click(screen.getByTestId("wk-article-ai-change"));
    await user.click(screen.getByTestId("wk-ai-change-submit"));
    expect(screen.getByRole("alert")).toBeInTheDocument();
    expect(createSpy).not.toHaveBeenCalled();
  });

  it("creates the librarian task and confirms with a link to it", async () => {
    const createSpy = vi
      .spyOn(tasksApi, "createTasks")
      .mockResolvedValue({ tasks: [mkTask("WIKI-9")] });
    render(
      <RequestAIChangeControl title={ARTICLE_TITLE} path={ARTICLE_PATH} />,
    );
    const user = userEvent.setup();
    await user.click(screen.getByTestId("wk-article-ai-change"));
    await user.type(
      screen.getByTestId("wk-ai-change-input"),
      "Add the new renewal terms from the June call.",
    );
    await user.click(screen.getByTestId("wk-ai-change-submit"));

    await waitFor(() =>
      expect(screen.getByTestId("wk-ai-change-confirm")).toBeInTheDocument(),
    );
    expect(createSpy).toHaveBeenCalledWith(
      [
        {
          title: "Update wiki article: Acme Corp",
          assignee: LIBRARIAN_SLUG,
          details:
            "Add the new renewal terms from the June call.\n\n" +
            `Wiki article: ${ARTICLE_PATH}\n` +
            "Also update related items (linked articles, the index) that this change affects.",
        },
      ],
      { channel: "general", createdBy: "human" },
    );
    const link = screen.getByRole("link", { name: /Open task WIKI-9/ });
    expect(link).toHaveAttribute("href", "#/tasks/WIKI-9");
  });

  it("surfaces a visible error when task creation fails", async () => {
    vi.spyOn(tasksApi, "createTasks").mockRejectedValue(
      new Error("channel access denied"),
    );
    render(
      <RequestAIChangeControl title={ARTICLE_TITLE} path={ARTICLE_PATH} />,
    );
    const user = userEvent.setup();
    await user.click(screen.getByTestId("wk-article-ai-change"));
    await user.type(screen.getByTestId("wk-ai-change-input"), "Do the thing.");
    await user.click(screen.getByTestId("wk-ai-change-submit"));
    await waitFor(() => expect(screen.getByRole("alert")).toBeInTheDocument());
    expect(screen.getByRole("alert").textContent).toContain(
      "channel access denied",
    );
  });
});
