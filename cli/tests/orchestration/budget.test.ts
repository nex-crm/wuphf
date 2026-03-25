import { describe, it, beforeEach } from 'node:test';
import assert from 'node:assert/strict';
import { BudgetTracker } from '../../src/orchestration/budget.js';

describe('BudgetTracker', () => {
  let tracker: BudgetTracker;

  beforeEach(() => {
    tracker = new BudgetTracker({ maxTokens: 10_000, maxCostUsd: 1.0 });
  });

  // ── record() ──────────────────────────────────────────────────────

  it('record() tracks usage correctly', () => {
    tracker.record('agent-a', 500, 0.05);
    const snap = tracker.getSnapshot('agent-a');
    assert.equal(snap.tokensUsed, 500);
    assert.equal(snap.costUsd, 0.05);
  });

  it('record() accumulates multiple calls', () => {
    tracker.record('agent-a', 500, 0.05);
    tracker.record('agent-a', 300, 0.03);
    const snap = tracker.getSnapshot('agent-a');
    assert.equal(snap.tokensUsed, 800);
    assert.equal(snap.costUsd, 0.08);
  });

  it('record() tracks separate agents independently', () => {
    tracker.record('agent-a', 500, 0.05);
    tracker.record('agent-b', 200, 0.02);
    assert.equal(tracker.getSnapshot('agent-a').tokensUsed, 500);
    assert.equal(tracker.getSnapshot('agent-b').tokensUsed, 200);
  });

  // ── canProceed() ──────────────────────────────────────────────────

  it('canProceed() returns true when under budget', () => {
    tracker.record('agent-a', 5000, 0.5);
    assert.equal(tracker.canProceed('agent-a'), true);
  });

  it('canProceed() returns false when over token budget', () => {
    tracker.record('agent-a', 11_000, 0.5);
    assert.equal(tracker.canProceed('agent-a'), false);
  });

  it('canProceed() returns false when over cost budget', () => {
    tracker.record('agent-a', 500, 1.5);
    assert.equal(tracker.canProceed('agent-a'), false);
  });

  it('canProceed() returns true for unknown agent', () => {
    assert.equal(tracker.canProceed('nonexistent'), true);
  });

  // ── isWarning() ───────────────────────────────────────────────────

  it('isWarning() returns false below 80%', () => {
    tracker.record('agent-a', 7000, 0.5);
    assert.equal(tracker.isWarning('agent-a'), false);
  });

  it('isWarning() returns true above 80% tokens', () => {
    tracker.record('agent-a', 8500, 0.5);
    assert.equal(tracker.isWarning('agent-a'), true);
  });

  it('isWarning() returns true above 80% cost', () => {
    tracker.record('agent-a', 1000, 0.85);
    assert.equal(tracker.isWarning('agent-a'), true);
  });

  // ── reset() ───────────────────────────────────────────────────────

  it('reset() clears usage for an agent', () => {
    tracker.record('agent-a', 5000, 0.5);
    tracker.reset('agent-a');
    const snap = tracker.getSnapshot('agent-a');
    assert.equal(snap.tokensUsed, 0);
    assert.equal(snap.costUsd, 0);
  });

  it('reset() does not affect other agents', () => {
    tracker.record('agent-a', 5000, 0.5);
    tracker.record('agent-b', 3000, 0.3);
    tracker.reset('agent-a');
    assert.equal(tracker.getSnapshot('agent-b').tokensUsed, 3000);
  });

  // ── getGlobalUsage() ─────────────────────────────────────────────

  it('getGlobalUsage() sums all agents', () => {
    tracker.record('agent-a', 3000, 0.3);
    tracker.record('agent-b', 2000, 0.2);
    const global = tracker.getGlobalUsage();
    assert.equal(global.tokens, 5000);
    assert.equal(global.cost, 0.5);
    assert.equal(global.percentTokens, 50);
    assert.equal(global.percentCost, 50);
  });

  it('getGlobalUsage() returns zeros when empty', () => {
    const global = tracker.getGlobalUsage();
    assert.equal(global.tokens, 0);
    assert.equal(global.cost, 0);
  });

  // ── getAllSnapshots() ─────────────────────────────────────────────

  it('getAllSnapshots() returns snapshots for all tracked agents', () => {
    tracker.record('agent-a', 1000, 0.1);
    tracker.record('agent-b', 2000, 0.2);
    const snapshots = tracker.getAllSnapshots();
    assert.equal(snapshots.length, 2);
    const slugs = snapshots.map((s) => s.agentSlug).sort();
    assert.deepEqual(slugs, ['agent-a', 'agent-b']);
  });

  // ── percentUsed / warning / exceeded ──────────────────────────────

  it('snapshot percentUsed uses max of token and cost percent', () => {
    // 50% tokens, 90% cost -> percentUsed should be 90
    tracker.record('agent-a', 5000, 0.9);
    const snap = tracker.getSnapshot('agent-a');
    assert.equal(snap.percentUsed, 90);
    assert.equal(snap.warning, true);
    assert.equal(snap.exceeded, false);
  });

  it('snapshot exceeded is true when over 100%', () => {
    tracker.record('agent-a', 15_000, 0.5);
    const snap = tracker.getSnapshot('agent-a');
    assert.equal(snap.exceeded, true);
  });
});
