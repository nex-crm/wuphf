import { describe, expect, it } from "vitest";

import type { CommandItem } from "./commandTypes";
import { matchesQuery } from "./useCommandItems";

type ItemBase = Omit<CommandItem, "run">;

function item(overrides: Partial<ItemBase> = {}): ItemBase {
  return {
    id: "test:item",
    group: "Actions",
    icon: "⚡",
    label: "Test item",
    desc: "A description",
    ...overrides,
  };
}

describe("matchesQuery", () => {
  it("returns true for empty query", () => {
    expect(matchesQuery(item(), "")).toBe(true);
  });

  it("matches label substring", () => {
    expect(matchesQuery(item({ label: "Open settings" }), "settings")).toBe(
      true,
    );
  });

  it("matches description substring", () => {
    expect(
      matchesQuery(
        item({ desc: "Configure providers and themes" }),
        "providers",
      ),
    ).toBe(true);
  });

  it("matches alias", () => {
    expect(
      matchesQuery(
        item({ aliases: ["doctor", "health", "diagnose"] }),
        "doctor",
      ),
    ).toBe(true);
  });

  it("is case-insensitive", () => {
    expect(matchesQuery(item({ label: "Open Settings" }), "SETTINGS")).toBe(
      true,
    );
  });

  it("matches meta", () => {
    expect(matchesQuery(item({ meta: "@ceo" }), "ceo")).toBe(true);
  });

  it("returns false when nothing matches", () => {
    expect(
      matchesQuery(
        item({
          label: "Open settings",
          desc: "Configure options",
          meta: "@slug",
          aliases: ["config"],
        }),
        "zzz-no-match",
      ),
    ).toBe(false);
  });

  it("handles items without optional fields", () => {
    expect(
      matchesQuery(
        { id: "x", group: "Actions", icon: "x", label: "Foo" },
        "foo",
      ),
    ).toBe(true);
  });
});
