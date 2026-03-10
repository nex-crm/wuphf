import { describe, it } from "node:test";
import assert from "node:assert/strict";
import { fallbackIntegrations } from "../../src/commands/setup.js";
import type { IntegrationEntry } from "../../src/commands/setup.js";

describe("fallbackIntegrations", () => {
  it("returns a non-empty list of integrations", () => {
    const result = fallbackIntegrations();
    assert.ok(Array.isArray(result), "should return an array");
    assert.ok(result.length > 0, "should not be empty");
  });

  it("each entry has required fields", () => {
    const result = fallbackIntegrations();
    for (const entry of result) {
      assert.ok(typeof entry.type === "string" && entry.type.length > 0, `type should be non-empty string, got: ${entry.type}`);
      assert.ok(typeof entry.provider === "string" && entry.provider.length > 0, `provider should be non-empty string, got: ${entry.provider}`);
      assert.ok(typeof entry.display_name === "string" && entry.display_name.length > 0, `display_name should be non-empty string, got: ${entry.display_name}`);
      assert.ok(typeof entry.description === "string" && entry.description.length > 0, `description should be non-empty string, got: ${entry.description}`);
      assert.ok(Array.isArray(entry.connections), "connections should be an array");
      assert.equal(entry.connections.length, 0, "fallback connections should be empty");
    }
  });

  it("includes all expected integrations", () => {
    const result = fallbackIntegrations();
    const providers = result.map((e) => `${e.type}/${e.provider}`);
    assert.ok(providers.includes("email/google"), "should include Gmail");
    assert.ok(providers.includes("calendar/google"), "should include Google Calendar");
    assert.ok(providers.includes("email/microsoft"), "should include Outlook");
    assert.ok(providers.includes("calendar/microsoft"), "should include Outlook Calendar");
    assert.ok(providers.includes("messaging/slack"), "should include Slack");
    assert.ok(providers.includes("crm/salesforce"), "should include Salesforce");
    assert.ok(providers.includes("crm/hubspot"), "should include HubSpot");
    assert.ok(providers.includes("crm/attio"), "should include Attio");
  });

  it("has no duplicate type/provider combinations", () => {
    const result = fallbackIntegrations();
    const keys = result.map((e) => `${e.type}/${e.provider}`);
    const unique = new Set(keys);
    assert.equal(keys.length, unique.size, "should have no duplicates");
  });
});
