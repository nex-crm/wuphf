import { describe, expect, it } from "vitest";

import { isSourceKind, kindFromSourceId } from "./sources";

describe("kindFromSourceId", () => {
  it("derives the kind from the id prefix before the first dash", () => {
    expect(kindFromSourceId("task-wup-12")).toBe("task");
    expect(kindFromSourceId("chat-general-2026-06-25")).toBe("chat");
    expect(kindFromSourceId("decision-rrf-1")).toBe("decision");
    expect(kindFromSourceId("doc-abc")).toBe("doc");
    expect(kindFromSourceId("url-xyz")).toBe("url");
    expect(kindFromSourceId("note-foo")).toBe("note");
  });

  it("returns null for an unknown prefix or a missing dash", () => {
    expect(kindFromSourceId("bogus-1")).toBeNull();
    expect(kindFromSourceId("noprefix")).toBeNull();
    expect(kindFromSourceId("-leading")).toBeNull();
  });
});

describe("isSourceKind", () => {
  it("narrows known kinds", () => {
    expect(isSourceKind("task")).toBe(true);
    expect(isSourceKind("note")).toBe(true);
    expect(isSourceKind("nope")).toBe(false);
  });
});
