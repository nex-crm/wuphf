import { describe, it, beforeEach, afterEach } from 'node:test';
import assert from 'node:assert/strict';
import { mkdtempSync, rmSync } from 'node:fs';
import { join } from 'node:path';
import { tmpdir } from 'node:os';
import { Scheduler, parseCron, nextRun } from '../../src/calendar/scheduler.js';
import { CalendarStore } from '../../src/calendar/store.js';

describe('parseCron', () => {
  it('handles "daily"', () => {
    assert.equal(parseCron('daily'), '0 0 * * *');
  });

  it('handles "hourly"', () => {
    assert.equal(parseCron('hourly'), '0 * * * *');
  });

  it('handles "6h"', () => {
    assert.equal(parseCron('6h'), '0 */6 * * *');
  });

  it('handles "12h"', () => {
    assert.equal(parseCron('12h'), '0 */12 * * *');
  });

  it('handles "30m"', () => {
    assert.equal(parseCron('30m'), '*/30 * * * *');
  });

  it('passes through valid 5-field cron', () => {
    assert.equal(parseCron('0 */4 * * *'), '0 */4 * * *');
    assert.equal(parseCron('15 3 * * 1'), '15 3 * * 1');
  });

  it('throws for invalid expression', () => {
    assert.throws(() => parseCron('bogus expression here and more'), /Invalid cron/);
  });

  it('handles "weekly"', () => {
    assert.equal(parseCron('weekly'), '0 0 * * 0');
  });

  it('is case insensitive', () => {
    assert.equal(parseCron('DAILY'), '0 0 * * *');
    assert.equal(parseCron('Hourly'), '0 * * * *');
  });
});

describe('nextRun', () => {
  it('calculates correct time for hourly', () => {
    const after = new Date('2025-06-15T10:30:00Z');
    const result = nextRun('hourly', after);
    // Next hourly after 10:30 is 11:00
    assert.equal(result.getUTCHours(), 11);
    assert.equal(result.getUTCMinutes(), 0);
  });

  it('calculates correct time for daily', () => {
    const after = new Date('2025-06-15T10:30:00Z');
    const result = nextRun('daily', after);
    // Next daily (midnight) after 10:30 is next day 00:00
    assert.equal(result.getUTCHours(), 0);
    assert.equal(result.getUTCMinutes(), 0);
    assert.equal(result.getUTCDate(), 16);
  });

  it('returns a Date in the future', () => {
    const now = new Date();
    const result = nextRun('hourly', now);
    assert.ok(result.getTime() > now.getTime());
  });

  it('handles every 6 hours', () => {
    const after = new Date('2025-06-15T07:00:00Z');
    const result = nextRun('6h', after);
    // */6 hours: 0,6,12,18. After 07:00, next is 12:00
    assert.equal(result.getUTCHours(), 12);
    assert.equal(result.getUTCMinutes(), 0);
  });
});

describe('Scheduler', () => {
  let tmpDir: string;
  let store: CalendarStore;
  let scheduler: Scheduler;

  beforeEach(() => {
    tmpDir = mkdtempSync(join(tmpdir(), 'wuphf-scheduler-test-'));
    store = new CalendarStore(join(tmpDir, 'calendar.json'));
    scheduler = new Scheduler(store);
  });

  afterEach(() => {
    rmSync(tmpDir, { recursive: true, force: true });
  });

  it('schedule creates a task with ID and nextRun', () => {
    const task = scheduler.schedule({
      agentSlug: 'seo-agent',
      type: 'heartbeat',
      cronExpression: 'daily',
      enabled: true,
    });
    assert.ok(task.id);
    assert.ok(task.nextRun > Date.now() - 1000);
    assert.equal(task.missed, false);
    assert.equal(task.agentSlug, 'seo-agent');
  });

  it('getMissed detects overdue tasks', () => {
    // Manually add a task with nextRun in the past
    store.add({
      id: 'overdue-1',
      agentSlug: 'bot',
      type: 'heartbeat',
      cronExpression: 'daily',
      nextRun: Date.now() - 60_000,
      missed: false,
      enabled: true,
    });

    const missed = scheduler.getMissed();
    assert.equal(missed.length, 1);
    assert.equal(missed[0].id, 'overdue-1');
  });

  it('getMissed ignores disabled tasks', () => {
    store.add({
      id: 'disabled-1',
      agentSlug: 'bot',
      type: 'heartbeat',
      cronExpression: 'daily',
      nextRun: Date.now() - 60_000,
      missed: false,
      enabled: false,
    });

    const missed = scheduler.getMissed();
    assert.equal(missed.length, 0);
  });

  it('getUpcoming returns tasks in range', () => {
    const now = Date.now();
    store.add({
      id: 'soon-1',
      agentSlug: 'bot',
      type: 'heartbeat',
      cronExpression: 'daily',
      nextRun: now + 60_000,
      missed: false,
      enabled: true,
    });
    store.add({
      id: 'far-1',
      agentSlug: 'bot',
      type: 'heartbeat',
      cronExpression: 'daily',
      nextRun: now + 999_999_999,
      missed: false,
      enabled: true,
    });

    const upcoming = scheduler.getUpcoming(
      new Date(now),
      new Date(now + 120_000),
    );
    assert.equal(upcoming.length, 1);
    assert.equal(upcoming[0].id, 'soon-1');
  });

  it('markCompleted updates lastRun and nextRun', () => {
    store.add({
      id: 'complete-me',
      agentSlug: 'bot',
      type: 'heartbeat',
      cronExpression: 'hourly',
      nextRun: Date.now() - 1000,
      missed: false,
      enabled: true,
    });

    scheduler.markCompleted('complete-me');
    const tasks = store.load();
    const updated = tasks.find(t => t.id === 'complete-me');
    assert.ok(updated);
    assert.ok(updated!.lastRun! > 0);
    assert.ok(updated!.nextRun > Date.now() - 1000);
  });
});
