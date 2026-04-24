import { describe, expect, it } from "vitest";

import { directChannelSlug, isDMChannel } from "./app";

describe("DM channel helpers", () => {
  it("uses the broker canonical direct slug", () => {
    expect(directChannelSlug("ceo")).toBe("ceo__human");
    expect(directChannelSlug("pm")).toBe("human__pm");
  });

  it("recognizes canonical and legacy DM slugs", () => {
    expect(isDMChannel("ceo__human", {})).toEqual({ agentSlug: "ceo" });
    expect(isDMChannel("human__pm", {})).toEqual({ agentSlug: "pm" });
    expect(isDMChannel("dm-ceo", {})).toEqual({ agentSlug: "ceo" });
    expect(isDMChannel("dm-human-ceo", {})).toEqual({ agentSlug: "ceo" });
  });
});
