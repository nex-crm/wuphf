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
      undefined,
      { signal: undefined },
    );
    expect(out).toEqual(pending);
  });

  it("threads the abort signal so a superseded poll is cancellable", async () => {
    // React Query passes its `signal`; without threading it, a late response
    // from a previous poll could repopulate stale approval cards next run.
    (get as Mock).mockResolvedValue({ pending: [] });
    const controller = new AbortController();
    await getBrowserApprovals("app_x", controller.signal);
    expect(get).toHaveBeenCalledWith(
      "/operator/apps/app_x/workflow/browser/pending",
      undefined,
      { signal: controller.signal },
    );
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

  it("posts a deny decision unchanged", async () => {
    // The deny path gates a send; a typo in the literal would silently let it
    // through, so pin the exact "deny" value that reaches the broker.
    (post as Mock).mockResolvedValue({ status: "ok" });
    await resolveBrowserApproval("app_x", "a1", "deny");
    expect(post).toHaveBeenCalledWith(
      "/operator/apps/app_x/workflow/browser/approve",
      { approval_id: "a1", decision: "deny" },
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
