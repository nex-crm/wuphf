import { describe, it, beforeEach, afterEach } from "node:test";
import assert from "node:assert/strict";
import { mkdtempSync, rmSync } from "node:fs";
import { join } from "node:path";
import { tmpdir } from "node:os";
import { AgentService } from "../../../src/tui/services/agent-service.js";
import { ToolRegistry } from "../../../src/agent/tools.js";
import { AgentSessionStore } from "../../../src/agent/session-store.js";
import { MessageQueues } from "../../../src/agent/queues.js";
import type { AgentConfig } from "../../../src/agent/types.js";

describe("AgentService", () => {
  let tmpDir: string;
  let toolRegistry: ToolRegistry;
  let sessionStore: AgentSessionStore;
  let queues: MessageQueues;
  let service: AgentService;

  const testConfig: AgentConfig = {
    slug: "test-agent",
    name: "Test Agent",
    expertise: ["testing", "validation"],
  };

  beforeEach(() => {
    tmpDir = mkdtempSync(join(tmpdir(), "wuphf-agent-svc-test-"));
    toolRegistry = new ToolRegistry();
    sessionStore = new AgentSessionStore(tmpDir);
    queues = new MessageQueues();
    service = new AgentService(toolRegistry, sessionStore, queues);
  });

  afterEach(() => {
    rmSync(tmpDir, { recursive: true, force: true });
  });

  // ── create ────────────────────────────────────────────────────

  it("create() adds agent to the service", () => {
    const managed = service.create(testConfig);
    assert.equal(managed.config.slug, "test-agent");
    assert.equal(managed.config.name, "Test Agent");
    assert.deepEqual(managed.config.expertise, ["testing", "validation"]);
    assert.equal(managed.state.phase, "idle");
  });

  it("create() returns the managed agent", () => {
    const managed = service.create(testConfig);
    assert.ok(managed.loop);
    assert.ok(managed.config);
    assert.ok(managed.state);
  });

  it("create() throws if slug already exists", () => {
    service.create(testConfig);
    assert.throws(
      () => service.create(testConfig),
      { message: 'Agent "test-agent" already exists.' },
    );
  });

  // ── createFromTemplate ────────────────────────────────────────

  it("createFromTemplate() uses template config", () => {
    const managed = service.createFromTemplate("my-seo", "seo-agent");
    assert.equal(managed.config.slug, "my-seo");
    assert.equal(managed.config.name, "SEO Analyst");
    assert.ok(managed.config.expertise.includes("seo"));
  });

  it("createFromTemplate() throws for unknown template", () => {
    assert.throws(
      () => service.createFromTemplate("x", "nonexistent"),
      /Unknown template/,
    );
  });

  // ── list / get / getState ─────────────────────────────────────

  it("list() returns all agents", () => {
    assert.equal(service.list().length, 0);
    service.create(testConfig);
    service.create({ slug: "agent-2", name: "Agent 2", expertise: ["other"] });
    assert.equal(service.list().length, 2);
  });

  it("get() returns agent by slug", () => {
    service.create(testConfig);
    const managed = service.get("test-agent");
    assert.ok(managed);
    assert.equal(managed.config.name, "Test Agent");
  });

  it("get() returns undefined for unknown slug", () => {
    assert.equal(service.get("nope"), undefined);
  });

  it("getState() returns current state snapshot", () => {
    service.create(testConfig);
    const state = service.getState("test-agent");
    assert.ok(state);
    assert.equal(state.phase, "idle");
  });

  it("getState() returns undefined for unknown slug", () => {
    assert.equal(service.getState("nope"), undefined);
  });

  // ── start / stop ──────────────────────────────────────────────

  it("start() changes agent phase from idle", async () => {
    service.create(testConfig);
    await service.start("test-agent");
    const state = service.getState("test-agent");
    assert.ok(state);
    // After start + tick, phase should have progressed past idle
    assert.notEqual(state.phase, "idle");
  });

  it("stop() sets agent phase to done", async () => {
    service.create(testConfig);
    await service.start("test-agent");
    service.stop("test-agent");
    const state = service.getState("test-agent");
    assert.ok(state);
    assert.equal(state.phase, "done");
  });

  it("start() throws for unknown slug", async () => {
    await assert.rejects(
      () => service.start("nope"),
      { message: 'Agent "nope" not found.' },
    );
  });

  it("stop() throws for unknown slug", () => {
    assert.throws(
      () => service.stop("nope"),
      { message: 'Agent "nope" not found.' },
    );
  });

  // ── subscribe ─────────────────────────────────────────────────

  it("subscribe() fires on create", () => {
    let callCount = 0;
    service.subscribe(() => { callCount++; });
    service.create(testConfig);
    assert.ok(callCount > 0, "Listener should fire on create");
  });

  it("subscribe() fires on state change (start/stop)", async () => {
    service.create(testConfig);
    let callCount = 0;
    service.subscribe(() => { callCount++; });
    await service.start("test-agent");
    const afterStart = callCount;
    assert.ok(afterStart > 0, "Listener should fire on start");

    service.stop("test-agent");
    assert.ok(callCount > afterStart, "Listener should fire on stop");
  });

  it("unsubscribe stops notifications", () => {
    let callCount = 0;
    const unsub = service.subscribe(() => { callCount++; });
    service.create(testConfig);
    const afterCreate = callCount;
    unsub();
    service.create({ slug: "another", name: "Another", expertise: [] });
    assert.equal(callCount, afterCreate, "Listener should not fire after unsubscribe");
  });

  // ── steer / followUp ─────────────────────────────────────────

  it("steer() puts message in queue", () => {
    service.create(testConfig);
    service.steer("test-agent", "change direction");
    // Verify by checking the queues have a steer message
    assert.ok(queues.hasSteer("test-agent"));
  });

  it("steer() throws for unknown slug", () => {
    assert.throws(
      () => service.steer("nope", "msg"),
      { message: 'Agent "nope" not found.' },
    );
  });

  it("followUp() puts message in queue", () => {
    service.create(testConfig);
    service.followUp("test-agent", "next question");
    assert.ok(queues.hasFollowUp("test-agent"));
  });

  it("followUp() throws for unknown slug", () => {
    assert.throws(
      () => service.followUp("nope", "msg"),
      { message: 'Agent "nope" not found.' },
    );
  });

  // ── templates ─────────────────────────────────────────────────

  it("getTemplateNames() returns available templates", () => {
    const names = service.getTemplateNames();
    assert.ok(names.length > 0);
    assert.ok(names.includes("seo-agent"));
    assert.ok(names.includes("lead-gen"));
    assert.ok(names.includes("research"));
  });

  it("getTemplate() returns template config", () => {
    const tmpl = service.getTemplate("seo-agent");
    assert.ok(tmpl);
    assert.equal(tmpl.name, "SEO Analyst");
    assert.ok(tmpl.expertise.includes("seo"));
  });

  it("getTemplate() returns undefined for unknown name", () => {
    assert.equal(service.getTemplate("nonexistent"), undefined);
  });
});
