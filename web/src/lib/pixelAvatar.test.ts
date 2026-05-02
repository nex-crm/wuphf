import { describe, expect, it } from "vitest";

import { resolveKnownPortraitSprite } from "./avatarSprites.generated";
import {
  getAgentColor,
  paintPixelAvatarData,
  resolvePortraitSprite,
} from "./pixelAvatar";

describe("pixel avatar sprite resolution", () => {
  it("maps operation-created agent slugs into the generated avatar catalog", () => {
    const mappings = new Map([
      ["planner", "hybridPm"],
      ["builder", "hybridEng"],
      ["growth", "hybridGtm"],
      ["reviewer", "hybridQa"],
      ["operator", "hybridNex"],
    ]);

    for (const [slug, id] of mappings) {
      expect(resolveKnownPortraitSprite(slug)?.id).toBe(id);
    }
  });

  it("normalizes slugs before resolving portraits", () => {
    expect(resolvePortraitSprite(" Planner ")?.id).toBe("hybridPm");

    const mixedCase = resolvePortraitSprite("Custom-Ops-Agent");
    const normalized = resolvePortraitSprite(" custom-ops-agent ");
    expect(mixedCase.id).toBe(normalized.id);
    expect(mixedCase.palette).toEqual(normalized.palette);
  });

  it("keeps arbitrary new-agent slugs on generated office sprites", () => {
    const avatar = resolvePortraitSprite("custom-ops-agent");
    const idParts = avatar.id.split(":");
    const baseID = idParts[idParts.length - 1];

    expect(avatar.id).toMatch(/^procedural:custom-ops-agent:hybrid/);
    expect([
      "hybridCeo",
      "hybridGeneric",
      "hybridHuman",
      "hybridPam",
      "hybridPamCute",
    ]).not.toContain(baseID);
    expect(avatar.portrait.length).toBeGreaterThan(0);
  });

  it("procedurally varies generated office palettes by slug", () => {
    const first = resolvePortraitSprite("custom-ops-agent");
    const again = resolvePortraitSprite("custom-ops-agent");
    const second = resolvePortraitSprite("custom-sales-agent");

    expect(first.id).toBe(again.id);
    expect(first.palette).toEqual(again.palette);
    expect(`${first.id}:${first.palette.join(",")}`).not.toBe(
      `${second.id}:${second.palette.join(",")}`,
    );
  });

  it("keeps procedural agent colors stable and accent-like", () => {
    expect(getAgentColor("ceo")).toBe("#E8A838");
    expect(getAgentColor("custom-ops-agent")).toMatch(/^#[0-9A-F]{6}$/i);
    expect(getAgentColor("custom-ops-agent")).toBe(
      getAgentColor("custom-ops-agent"),
    );
  });

  it("keeps known role aliases on canonical role colors", () => {
    expect(getAgentColor("planner")).toBe(getAgentColor("pm"));
    expect(getAgentColor("builder")).toBe(getAgentColor("eng"));
    expect(getAgentColor("growth")).toBe(getAgentColor("gtm"));
    expect(getAgentColor("operator")).toBe(getAgentColor("nex"));
  });

  it("treats missing cells in short sprite rows as transparent", () => {
    const data = new Uint8ClampedArray(2 * 2 * 4);

    paintPixelAvatarData(data, [[1], [0, 1]], { 1: [10, 20, 30] }, 2);

    expect(Array.from(data.slice(0, 4))).toEqual([10, 20, 30, 255]);
    expect(Array.from(data.slice(4, 8))).toEqual([0, 0, 0, 0]);
    expect(Array.from(data.slice(8, 12))).toEqual([0, 0, 0, 0]);
    expect(Array.from(data.slice(12, 16))).toEqual([10, 20, 30, 255]);
  });
});
