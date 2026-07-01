import { beforeEach, describe, expect, it, type Mock, vi } from "vitest";

import {
  type BrowserApproval,
  browserApprovalPrompt,
  getBrowserApprovals,
  resolveBrowserApproval,
} from "./browserApprovals";

vi.mock("../../api/client", () => ({ get: vi.fn(), post: vi.fn() }));

import { get, post } from "../../api/client";

describe("getBrowserApprovals", () => {
  beforeEach(() => vi.clearAllMocks());

  it("returns the app's paused asks", async () => {
    const pending: BrowserApproval[] = [
      { id: "a1", app_id: "app_x", kind: "control", goal: "email the digest" },
    ];
    (get as Mock).mockResolvedValue({ pending });
    const out = await getBrowserApprovals("app x");
    expect(get).toHaveBeenCalledWith(
      "/operator/apps/app%20x/workflow/browser/pending",
    );
    expect(out).toEqual(pending);
  });

  it("tolerates a missing pending array", async () => {
    (get as Mock).mockResolvedValue({});
    expect(await getBrowserApprovals("app_x")).toEqual([]);
  });
});

describe("resolveBrowserApproval", () => {
  beforeEach(() => vi.clearAllMocks());

  it("posts the decision for the ask", async () => {
    (post as Mock).mockResolvedValue({ status: "ok" });
    await resolveBrowserApproval("app_x", "a1", "approve");
    expect(post).toHaveBeenCalledWith(
      "/operator/apps/app_x/workflow/browser/approve",
      { approval_id: "a1", decision: "approve" },
    );
  });
});

describe("browserApprovalPrompt", () => {
  it("asks for browser control on a control ask", () => {
    const p = browserApprovalPrompt({
      id: "a",
      app_id: "x",
      kind: "control",
      goal: "post the update",
    });
    expect(p).toContain("control your browser");
    expect(p).toContain("post the update");
  });

  it("asks to confirm a send on a send ask", () => {
    const p = browserApprovalPrompt({
      id: "a",
      app_id: "x",
      kind: "send",
      goal: "Send to finance",
    });
    expect(p).toContain("Send it?");
    expect(p).toContain("Send to finance");
  });
});
