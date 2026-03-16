import { describe, test, expect } from "bun:test";
import { fallbackIntegrations } from "../../src/commands/setup.ts";
import type { IntegrationEntry } from "../../src/commands/setup.ts";

describe("fallbackIntegrations", () => {
  test("returns a non-empty list of integrations", () => {
    const result = fallbackIntegrations();
    expect(Array.isArray(result)).toBeTruthy();
    expect(result.length > 0).toBeTruthy();
  });

  test("each entry has required fields", () => {
    const result = fallbackIntegrations();
    for (const entry of result) {
      expect(typeof entry.type === "string" && entry.type.length > 0).toBeTruthy();
      expect(typeof entry.provider === "string" && entry.provider.length > 0).toBeTruthy();
      expect(typeof entry.display_name === "string" && entry.display_name.length > 0).toBeTruthy();
      expect(typeof entry.description === "string" && entry.description.length > 0).toBeTruthy();
      expect(Array.isArray(entry.connections)).toBeTruthy();
      expect(entry.connections.length).toBe(0);
    }
  });

  test("includes all expected integrations", () => {
    const result = fallbackIntegrations();
    const providers = result.map((e) => `${e.type}/${e.provider}`);
    expect(providers.includes("email/google")).toBeTruthy();
    expect(providers.includes("calendar/google")).toBeTruthy();
    expect(providers.includes("email/microsoft")).toBeTruthy();
    expect(providers.includes("calendar/microsoft")).toBeTruthy();
    expect(providers.includes("messaging/slack")).toBeTruthy();
    expect(providers.includes("crm/salesforce")).toBeTruthy();
    expect(providers.includes("crm/hubspot")).toBeTruthy();
    expect(providers.includes("crm/attio")).toBeTruthy();
  });

  test("has no duplicate type/provider combinations", () => {
    const result = fallbackIntegrations();
    const keys = result.map((e) => `${e.type}/${e.provider}`);
    const unique = new Set(keys);
    expect(keys.length).toBe(unique.size);
  });
});
