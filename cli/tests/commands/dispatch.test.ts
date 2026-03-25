import { describe, it } from "node:test";
import assert from "node:assert/strict";
import { dispatch, commandNames, commandHelp } from "../../src/commands/dispatch.js";
import type { CommandResult, CommandContext } from "../../src/commands/dispatch.js";

describe("dispatch", () => {
  it("returns error for empty input", async () => {
    const result = await dispatch("");
    assert.equal(result.exitCode, 1);
    assert.ok(result.error, "should have an error message");
    assert.match(result.error!, /no command/i);
  });

  it("returns error for whitespace-only input", async () => {
    const result = await dispatch("   ");
    assert.equal(result.exitCode, 1);
    assert.ok(result.error);
  });

  it("returns error for unknown command", async () => {
    const result = await dispatch("nonexistent");
    assert.equal(result.exitCode, 1);
    assert.ok(result.error);
    assert.match(result.error!, /unknown command/i);
  });

  it("returns error for unknown two-word command", async () => {
    const result = await dispatch("record nonexistent");
    assert.equal(result.exitCode, 1);
    assert.ok(result.error);
    assert.match(result.error!, /unknown command/i);
  });

  it("returns validation error for ask without query", async () => {
    const result = await dispatch("ask");
    assert.equal(result.exitCode, 1);
    assert.ok(result.error);
    assert.match(result.error!, /no query/i);
  });

  it("returns validation error for search without query", async () => {
    const result = await dispatch("search");
    assert.equal(result.exitCode, 1);
    assert.ok(result.error);
  });

  it("returns validation error for record get without ID", async () => {
    const result = await dispatch("record get");
    assert.equal(result.exitCode, 1);
    assert.ok(result.error);
    assert.match(result.error!, /no record id/i);
  });

  it("returns validation error for record list without slug", async () => {
    const result = await dispatch("record list");
    assert.equal(result.exitCode, 1);
    assert.ok(result.error);
    assert.match(result.error!, /no object slug/i);
  });

  it("returns validation error for object get without slug", async () => {
    const result = await dispatch("object get");
    assert.equal(result.exitCode, 1);
    assert.ok(result.error);
  });

  it("returns validation error for remember without content", async () => {
    const result = await dispatch("remember");
    assert.equal(result.exitCode, 1);
    assert.ok(result.error);
    assert.match(result.error!, /no content/i);
  });

  it("returns validation error for artifact without ID", async () => {
    const result = await dispatch("artifact");
    assert.equal(result.exitCode, 1);
    assert.ok(result.error);
  });

  it("returns validation error for record create missing --data", async () => {
    const result = await dispatch("record create person");
    assert.equal(result.exitCode, 1);
    assert.ok(result.error);
    assert.match(result.error!, /--data/i);
  });

  it("returns validation error for record create with invalid JSON", async () => {
    const result = await dispatch('record create person --data notjson');
    assert.equal(result.exitCode, 1);
    assert.ok(result.error);
    assert.match(result.error!, /invalid json/i);
  });

  it("returns validation error for note create missing --title", async () => {
    const result = await dispatch("note create");
    assert.equal(result.exitCode, 1);
    assert.ok(result.error);
    assert.match(result.error!, /--title/i);
  });

  it("returns validation error for task create missing --title", async () => {
    const result = await dispatch("task create");
    assert.equal(result.exitCode, 1);
    assert.ok(result.error);
    assert.match(result.error!, /--title/i);
  });

  it("returns validation error for integrate connect without name", async () => {
    const result = await dispatch("integrate connect");
    assert.equal(result.exitCode, 1);
    assert.ok(result.error);
  });

  it("returns validation error for integrate connect with unknown name", async () => {
    const result = await dispatch("integrate connect fakething");
    assert.equal(result.exitCode, 1);
    assert.ok(result.error);
    assert.match(result.error!, /unknown integration/i);
  });

  it("handles quoted query in ask command", async () => {
    // This tests the parsing path — the command will fail on auth, but that
    // proves parsing worked correctly (it got past the "no query" check)
    const result = await dispatch('ask "who is important?"');
    // Should NOT be the "no query" error — it should be an auth/network error
    if (result.exitCode !== 0) {
      assert.ok(!/no query/i.test(result.error!), `expected auth/network error, got: ${result.error}`);
    }
  });

  it("config show works without API key", async () => {
    const result = await dispatch("config show", { format: "json" });
    assert.equal(result.exitCode, 0);
    assert.ok(result.data);
    const data = result.data as Record<string, unknown>;
    assert.ok("config_path" in data);
    assert.ok("base_url" in data);
  });

  it("config path works without API key", async () => {
    const result = await dispatch("config path", { format: "json" });
    assert.equal(result.exitCode, 0);
    assert.ok(result.data);
    const data = result.data as Record<string, unknown>;
    assert.ok(typeof data.path === "string");
  });
});

describe("commandNames", () => {
  it("is a non-empty array", () => {
    assert.ok(Array.isArray(commandNames));
    assert.ok(commandNames.length > 0);
  });

  it("is sorted", () => {
    const sorted = [...commandNames].sort();
    assert.deepEqual(commandNames, sorted);
  });

  it("includes core commands", () => {
    assert.ok(commandNames.includes("ask"), "should include ask");
    assert.ok(commandNames.includes("search"), "should include search");
    assert.ok(commandNames.includes("remember"), "should include remember");
    assert.ok(commandNames.includes("recall"), "should include recall");
    assert.ok(commandNames.includes("artifact"), "should include artifact");
    assert.ok(commandNames.includes("capture"), "should include capture");
    assert.ok(commandNames.includes("graph"), "should include graph");
  });

  it("includes two-word commands", () => {
    assert.ok(commandNames.includes("record list"), "should include record list");
    assert.ok(commandNames.includes("record get"), "should include record get");
    assert.ok(commandNames.includes("record create"), "should include record create");
    assert.ok(commandNames.includes("object list"), "should include object list");
    assert.ok(commandNames.includes("object get"), "should include object get");
    assert.ok(commandNames.includes("config show"), "should include config show");
    assert.ok(commandNames.includes("note create"), "should include note create");
    assert.ok(commandNames.includes("task create"), "should include task create");
    assert.ok(commandNames.includes("integrate list"), "should include integrate list");
    assert.ok(commandNames.includes("insight list"), "should include insight list");
  });

  it("has no duplicates", () => {
    const unique = new Set(commandNames);
    assert.equal(commandNames.length, unique.size);
  });
});

describe("commandHelp", () => {
  it("is a non-empty array", () => {
    assert.ok(Array.isArray(commandHelp));
    assert.ok(commandHelp.length > 0);
  });

  it("each entry has required fields", () => {
    for (const entry of commandHelp) {
      assert.ok(typeof entry.command === "string" && entry.command.length > 0, `command should be non-empty: ${entry.command}`);
      assert.ok(typeof entry.description === "string" && entry.description.length > 0, `description should be non-empty for ${entry.command}`);
      assert.ok(typeof entry.category === "string" && entry.category.length > 0, `category should be non-empty for ${entry.command}`);
    }
  });

  it("includes all categories", () => {
    const categories = new Set(commandHelp.map((e) => e.category));
    assert.ok(categories.has("query"), "should have query category");
    assert.ok(categories.has("write"), "should have write category");
    assert.ok(categories.has("config"), "should have config category");
    assert.ok(categories.has("ai"), "should have ai category");
    assert.ok(categories.has("graph"), "should have graph category");
  });

  it("has same count as commandNames", () => {
    assert.equal(commandHelp.length, commandNames.length);
  });

  it("every help entry maps to a valid command name", () => {
    const nameSet = new Set(commandNames);
    for (const entry of commandHelp) {
      assert.ok(nameSet.has(entry.command), `help entry ${entry.command} not in commandNames`);
    }
  });
});

describe("command aliases", () => {
  it("resolves 'agents' alias to agent list", async () => {
    const result = await dispatch("agents");
    // Should NOT be "unknown command" -- the alias resolves to "agent list"
    assert.ok(!result.error || !/unknown command/i.test(result.error), `'agents' should resolve, got: ${result.error}`);
    assert.equal(result.exitCode, 0);
  });

  it("resolves 'objects' alias to object list", async () => {
    const result = await dispatch("objects");
    // This hits the network (no API key), so check it resolved past "unknown command"
    if (result.error) {
      assert.ok(!/unknown command/i.test(result.error), `'objects' should resolve, got: ${result.error}`);
    }
  });

  it("resolves 'orch' alias to orchestration", async () => {
    const result = await dispatch("orch");
    assert.ok(!result.error || !/unknown command/i.test(result.error), `'orch' should resolve, got: ${result.error}`);
    assert.equal(result.exitCode, 0);
  });

  it("chat command returns hint text", async () => {
    const result = await dispatch("chat");
    assert.equal(result.exitCode, 0);
    assert.ok(result.output.includes("keybinding"), `expected keybinding hint, got: ${result.output}`);
  });

  it("calendar command returns hint text", async () => {
    const result = await dispatch("calendar");
    assert.equal(result.exitCode, 0);
    assert.ok(result.output.includes("keybinding"), `expected keybinding hint, got: ${result.output}`);
  });

  it("orchestration command returns hint text", async () => {
    const result = await dispatch("orchestration");
    assert.equal(result.exitCode, 0);
    assert.ok(result.output.includes("keybinding"), `expected keybinding hint, got: ${result.output}`);
  });
});
