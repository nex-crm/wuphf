import { describe, it, beforeEach } from 'node:test';
import assert from 'node:assert/strict';
import { TaskRouter } from '../../src/orchestration/router.js';
import type { TaskDefinition } from '../../src/orchestration/types.js';

function makeTask(overrides: Partial<TaskDefinition> = {}): TaskDefinition {
  return {
    id: 'task-1',
    title: 'Test task',
    description: 'A test task',
    requiredSkills: [],
    priority: 'medium',
    status: 'pending',
    createdAt: Date.now(),
    ...overrides,
  };
}

describe('TaskRouter', () => {
  let router: TaskRouter;

  beforeEach(() => {
    router = new TaskRouter();
  });

  // ── findBestAgent ─────────────────────────────────────────────────

  it('findBestAgent returns agent with highest skill match', () => {
    router.registerAgent('seo-bot', [
      { name: 'seo', description: 'SEO', proficiency: 0.9 },
      { name: 'keyword-research', description: 'Keywords', proficiency: 0.8 },
    ]);
    router.registerAgent('sales-bot', [
      { name: 'prospecting', description: 'Sales', proficiency: 0.9 },
    ]);

    const task = makeTask({ requiredSkills: ['seo', 'keyword-research'] });
    const result = router.findBestAgent(task);
    assert.ok(result);
    assert.equal(result.agentSlug, 'seo-bot');
  });

  it('findBestAgent returns null when no agents registered', () => {
    const task = makeTask({ requiredSkills: ['seo'] });
    const result = router.findBestAgent(task);
    assert.equal(result, null);
  });

  it('findBestAgent returns null when no agents match skills', () => {
    router.registerAgent('sales-bot', [
      { name: 'prospecting', description: 'Sales', proficiency: 0.9 },
    ]);

    const task = makeTask({ requiredSkills: ['quantum-physics'] });
    const result = router.findBestAgent(task);
    assert.equal(result, null);
  });

  // ── findCapableAgents ─────────────────────────────────────────────

  it('findCapableAgents returns sorted list', () => {
    router.registerAgent('seo-bot', [
      { name: 'seo', description: 'SEO', proficiency: 0.9 },
    ]);
    router.registerAgent('content-bot', [
      { name: 'seo', description: 'SEO basics', proficiency: 0.5 },
    ]);
    router.registerAgent('sales-bot', [
      { name: 'prospecting', description: 'Sales', proficiency: 0.9 },
    ]);

    const task = makeTask({ requiredSkills: ['seo'] });
    const results = router.findCapableAgents(task);

    assert.ok(results.length >= 2);
    // First should have higher score than second
    assert.ok(results[0].score >= results[1].score);
    assert.equal(results[0].agentSlug, 'seo-bot');
  });

  it('findCapableAgents excludes agents with 0 matching skills', () => {
    router.registerAgent('seo-bot', [
      { name: 'seo', description: 'SEO', proficiency: 0.9 },
    ]);
    router.registerAgent('sales-bot', [
      { name: 'prospecting', description: 'Sales', proficiency: 0.9 },
    ]);

    const task = makeTask({ requiredSkills: ['seo'] });
    const results = router.findCapableAgents(task);

    // sales-bot should not appear (score 0)
    const slugs = results.map((r) => r.agentSlug);
    assert.ok(!slugs.includes('sales-bot'));
  });

  // ── scoreMatch ────────────────────────────────────────────────────

  it('agent with 0 matching skills gets score 0', () => {
    router.registerAgent('sales-bot', [
      { name: 'prospecting', description: 'Sales', proficiency: 0.9 },
    ]);

    const task = makeTask({ requiredSkills: ['quantum-physics'] });
    const score = router.scoreMatch('sales-bot', task);
    assert.equal(score, 0);
  });

  it('task with no required skills matches any agent with score 1', () => {
    router.registerAgent('generic-bot', [
      { name: 'general', description: 'General', proficiency: 0.5 },
    ]);

    const task = makeTask({ requiredSkills: [] });
    const score = router.scoreMatch('generic-bot', task);
    assert.equal(score, 1);
  });

  it('scoreMatch returns 0 for unregistered agent', () => {
    const task = makeTask({ requiredSkills: ['seo'] });
    const score = router.scoreMatch('nonexistent', task);
    assert.equal(score, 0);
  });

  it('higher proficiency yields higher score', () => {
    router.registerAgent('expert', [
      { name: 'seo', description: 'SEO', proficiency: 0.9 },
    ]);
    router.registerAgent('novice', [
      { name: 'seo', description: 'SEO', proficiency: 0.3 },
    ]);

    const task = makeTask({ requiredSkills: ['seo'] });
    const expertScore = router.scoreMatch('expert', task);
    const noviceScore = router.scoreMatch('novice', task);
    assert.ok(expertScore > noviceScore);
  });

  // ── unregisterAgent ───────────────────────────────────────────────

  it('unregisterAgent removes the agent', () => {
    router.registerAgent('seo-bot', [
      { name: 'seo', description: 'SEO', proficiency: 0.9 },
    ]);
    router.unregisterAgent('seo-bot');

    const task = makeTask({ requiredSkills: ['seo'] });
    const result = router.findBestAgent(task);
    assert.equal(result, null);
  });

  // ── fuzzy matching ────────────────────────────────────────────────

  it('fuzzy matches similar skill names', () => {
    router.registerAgent('enricher', [
      { name: 'data-enrichment', description: 'Enrichment', proficiency: 0.8 },
    ]);

    // "enrichment" should fuzzy-match "data-enrichment"
    const task = makeTask({ requiredSkills: ['enrichment'] });
    const score = router.scoreMatch('enricher', task);
    assert.ok(score > 0, `Expected score > 0, got ${score}`);
  });

  it('agent with zero skills gets score 0 for any task with requirements', () => {
    router.registerAgent('empty-bot', []);
    const task = makeTask({ requiredSkills: ['seo'] });
    const score = router.scoreMatch('empty-bot', task);
    assert.equal(score, 0);
  });
});
