import { describe, expect, it } from "vitest";

import {
  RESERVED_WORKSPACE_NAMES,
  validateWorkspaceSlug,
} from "../../../api/workspaces";

describe("validateWorkspaceSlug", () => {
  it("rejects empty slugs", () => {
    expect(validateWorkspaceSlug("").ok).toBe(false);
    expect(validateWorkspaceSlug("   ").ok).toBe(false);
  });

  it("accepts valid slugs", () => {
    expect(validateWorkspaceSlug("a").ok).toBe(true);
    expect(validateWorkspaceSlug("acme-demo").ok).toBe(true);
    expect(validateWorkspaceSlug("workspace-1").ok).toBe(true);
    expect(validateWorkspaceSlug("a".repeat(31)).ok).toBe(true);
  });

  it("rejects slugs that don't start with a letter", () => {
    expect(validateWorkspaceSlug("9bad").ok).toBe(false);
    expect(validateWorkspaceSlug("-bad").ok).toBe(false);
  });

  it("rejects uppercase, spaces, slashes, dots", () => {
    expect(validateWorkspaceSlug("Bad").ok).toBe(false);
    expect(validateWorkspaceSlug("bad name").ok).toBe(false);
    expect(validateWorkspaceSlug("a/b").ok).toBe(false);
    expect(validateWorkspaceSlug("a.b").ok).toBe(false);
  });

  it("rejects slugs longer than 31 chars", () => {
    expect(validateWorkspaceSlug("a".repeat(32)).ok).toBe(false);
  });

  it("rejects dot-prefixed and __-prefixed slugs", () => {
    expect(validateWorkspaceSlug(".hidden").ok).toBe(false);
    expect(validateWorkspaceSlug("__internal").ok).toBe(false);
  });

  it("rejects every reserved name with a 'reserved' message", () => {
    for (const reserved of RESERVED_WORKSPACE_NAMES) {
      const result = validateWorkspaceSlug(reserved);
      expect(result.ok).toBe(false);
      expect(result.reason).toMatch(/reserved/i);
    }
  });
});
