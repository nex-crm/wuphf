import { beforeEach, describe, expect, it, vi } from "vitest";

import { submitAppEdit } from "./apps";

// Mock the HTTP layer so we can assert the edit goes through the broker's
// explicit improve endpoint (which drives the task_followup wake) and NOT a new
// "Improve app" task.
const post = vi.fn();
vi.mock("./client", () => ({
  get: vi.fn(),
  del: vi.fn(),
  postMessage: vi.fn(),
  post: (path: string, body: unknown) => post(path, body),
}));

describe("submitAppEdit", () => {
  beforeEach(() => {
    post.mockReset();
  });

  it("posts the change to the app's improve endpoint and returns the edit channel", async () => {
    post.mockResolvedValue({ channel: "task-office-7" });

    const channel = await submitAppEdit("app_abc", "add a CSV export button");

    expect(post).toHaveBeenCalledWith("/apps/app_abc/improve", {
      change: "add a CSV export button",
    });
    expect(post).toHaveBeenCalledTimes(1);
    expect(channel).toBe("task-office-7");
  });
});
