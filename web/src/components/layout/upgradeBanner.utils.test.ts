import { describe, expect, it } from "vitest";

import {
  compareVersions,
  groupCommits,
  isDevVersion,
  parseCommit,
  VERSION_RE,
} from "./upgradeBanner.utils";

describe("compareVersions", () => {
  it.each([
    ["0.79.10", "0.79.15", -1],
    ["v0.79.15", "v0.79.15", 0],
    ["0.79.15", "0.79.10", 1],
    ["0.79.10", "0.79.10.1", -1],
    ["0.80.0", "0.79.99", 1],
    ["dev", "0.79.10", -1],
    // Pre-release suffix stripped before comparison — must NOT invert
    // ordering the way the previous splitVersion did.
    ["0.79.10-rc.1", "0.79.10", 0],
    ["0.79.10", "0.79.10-rc.1", 0],
    ["0.79.10-rc.1", "0.79.11", -1],
  ] as const)("compareVersions(%s, %s) === %i", (a, b, want) => {
    expect(compareVersions(a, b)).toBe(want);
  });
});

describe("isDevVersion", () => {
  it.each([
    ["", true],
    ["dev", true],
    ["  dev  ", true],
    ["0.79.10", false],
    ["v0.79.10", false],
    [null, true],
    [undefined, true],
  ] as const)("isDevVersion(%j) === %s", (v, want) => {
    expect(isDevVersion(v)).toBe(want);
  });
});

describe("parseCommit", () => {
  it("parses a canonical conventional commit with scope and PR", () => {
    expect(
      parseCommit("feat(wiki): inline citations on hover (#310)", "abc"),
    ).toEqual({
      type: "feat",
      scope: "wiki",
      description: "inline citations on hover",
      pr: "310",
      sha: "abc",
      breaking: false,
    });
  });

  it("captures the breaking-change ! marker", () => {
    expect(
      parseCommit("feat(api)!: drop legacy /v1 endpoints (#400)", "abc"),
    ).toMatchObject({
      type: "feat",
      scope: "api",
      description: "drop legacy /v1 endpoints",
      pr: "400",
      breaking: true,
    });
  });

  it("preserves an inline (#42) inside the description", () => {
    // Mirrors the Go test in internal/upgradecheck. If the two parsers
    // ever drift, this pair of tests is the canary.
    expect(
      parseCommit("fix(api): handle (#42) properly (#310)", "abc"),
    ).toMatchObject({
      type: "fix",
      scope: "api",
      description: "handle (#42) properly",
      pr: "310",
    });
  });

  it("falls back to 'other' for non-conventional subjects", () => {
    expect(
      parseCommit("Random subject without conventional prefix (#42)", "abc"),
    ).toMatchObject({
      type: "other",
      scope: "",
      description: "Random subject without conventional prefix (#42)",
      pr: "42",
    });
  });

  it("strips body and only inspects the first line", () => {
    expect(
      parseCommit("fix: broken link\n\nbody text here", "abc"),
    ).toMatchObject({
      type: "fix",
      description: "broken link",
      pr: null,
    });
  });
});

describe("groupCommits", () => {
  it("promotes breaking changes to the first group regardless of underlying type", () => {
    const grouped = groupCommits([
      {
        type: "feat",
        scope: "",
        description: "alpha",
        pr: null,
        sha: "1",
        breaking: false,
      },
      {
        type: "fix",
        scope: "",
        description: "beta",
        pr: null,
        sha: "2",
        breaking: true,
      },
      {
        type: "docs",
        scope: "",
        description: "gamma",
        pr: null,
        sha: "3",
        breaking: false,
      },
    ]);
    expect(grouped[0].label).toBe("Breaking changes");
    expect(grouped[0].entries[0].description).toBe("beta");
    // feat group should still appear, but after breaking.
    expect(
      grouped.find((g) => g.label === "New features")?.entries[0].description,
    ).toBe("alpha");
  });

  it("buckets unknown conventional types into 'Other changes'", () => {
    const grouped = groupCommits([
      {
        type: "chore",
        scope: "",
        description: "tidy",
        pr: null,
        sha: "1",
        breaking: false,
      },
      {
        type: "ci",
        scope: "",
        description: "pin",
        pr: null,
        sha: "2",
        breaking: false,
      },
    ]);
    const other = grouped.find((g) => g.label === "Other changes");
    expect(other?.entries.map((e) => e.description)).toEqual(["tidy", "pin"]);
  });
});

describe("VERSION_RE", () => {
  it.each([
    ["0.79.10", true],
    ["v0.79.10", true],
    ["0.79.10.1", true],
    ["1.2.3-rc.4", true],
    ["dev", false],
    ["", false],
    ["../etc/passwd", false],
    ["0.79.10/extra", false],
    ["v0.79.10; rm -rf /", false],
  ] as const)("VERSION_RE.test(%j) === %s", (input, want) => {
    expect(VERSION_RE.test(input)).toBe(want);
  });
});
