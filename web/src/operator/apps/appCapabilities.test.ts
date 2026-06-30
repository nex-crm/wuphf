import { describe, expect, it } from "vitest";

import type { AppCapabilities } from "../../api/apps";
import {
  actionIsWrite,
  deriveCapabilityRows,
  hasAnyCapability,
} from "./appCapabilities";

describe("actionIsWrite", () => {
  it("flags mutating Composio actions as writes", () => {
    expect(actionIsWrite("SLACK_SENDS_A_MESSAGE_TO_A_SLACK_CHANNEL")).toBe(
      true,
    );
    expect(actionIsWrite("GMAIL_CREATE_DRAFT")).toBe(true);
    expect(actionIsWrite("hubspot_update_contact")).toBe(true);
  });
  it("treats read actions as reads", () => {
    expect(actionIsWrite("GMAIL_FETCH_EMAILS")).toBe(false);
    expect(actionIsWrite("SLACK_LIST_CHANNELS")).toBe(false);
  });
});

describe("deriveCapabilityRows", () => {
  it("maps bridge reads, integration reads/writes, and office writes", () => {
    const caps: AppCapabilities = {
      bridge_apis: ["getTasks", "ai", "createTask"],
      integrations: [
        {
          platform: "slack",
          actions: ["SLACK_LIST_CHANNELS", "SLACK_SENDS_A_MESSAGE"],
        },
      ],
      office_writes: ["createTask"],
    };
    const { reads, writes } = deriveCapabilityRows(caps);

    // getTasks + ai are reads; createTask is NOT a read (it's an office write).
    expect(reads.map((r) => r.label)).toContain("Tasks");
    expect(reads.map((r) => r.label)).toContain("AI · one-shot");
    expect(reads.map((r) => r.label)).not.toContain("Create a task");
    // Slack read action is a read; the send is a gated write.
    expect(reads.find((r) => r.detail === "SLACK_LIST_CHANNELS")).toBeTruthy();
    const send = writes.find((w) => w.detail === "SLACK_SENDS_A_MESSAGE");
    expect(send?.gated).toBe(true);
    // The office write is surfaced and gated.
    const officeWrite = writes.find((w) => w.label === "Create a task");
    expect(officeWrite?.gated).toBe(true);
  });

  it("counts a platform referenced with no action as a read touch", () => {
    const { reads } = deriveCapabilityRows({
      integrations: [{ platform: "notion" }],
    });
    expect(reads).toEqual([{ label: "notion", detail: "referenced" }]);
  });
});

describe("hasAnyCapability", () => {
  it("is false for an html-only app with no scannable source", () => {
    expect(hasAnyCapability({})).toBe(false);
    expect(hasAnyCapability({ source_files: ["index.html"] })).toBe(false);
  });
  it("is true when the app reads or writes anything", () => {
    expect(hasAnyCapability({ bridge_apis: ["getTasks"] })).toBe(true);
    expect(hasAnyCapability({ data_types: ["Task"] })).toBe(true);
  });
});
