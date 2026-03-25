import { describe, it, beforeEach, afterEach } from 'node:test';
import assert from 'node:assert/strict';
import { mkdtempSync, rmSync } from 'node:fs';
import { join } from 'node:path';
import { tmpdir } from 'node:os';
import { CalendarService } from '../../../src/tui/services/calendar-service.js';
import { CalendarStore } from '../../../src/calendar/store.js';

describe('CalendarService', () => {
  let tmpDir: string;
  let calendarFile: string;
  let service: CalendarService;

  beforeEach(() => {
    tmpDir = mkdtempSync(join(tmpdir(), 'wuphf-calendar-service-test-'));
    calendarFile = join(tmpDir, 'calendar.json');
    service = new CalendarService(calendarFile);
  });

  afterEach(() => {
    rmSync(tmpDir, { recursive: true, force: true });
  });

  it('scheduleHeartbeat() creates a scheduled task', () => {
    service.scheduleHeartbeat('seo-agent', 'SEO Agent', 'daily');

    const upcoming = service.getUpcoming(48);
    assert.ok(upcoming.length >= 1, 'should have at least one upcoming task');

    const seoTask = upcoming.find(t => t.agentSlug === 'seo-agent');
    assert.ok(seoTask, 'should find the scheduled seo-agent task');
    assert.equal(seoTask!.type, 'heartbeat');
    assert.equal(seoTask!.enabled, true);
  });

  it('getWeekEvents() returns formatted events', () => {
    // Schedule an hourly heartbeat -- will produce events in current week
    service.scheduleHeartbeat('monitor-bot', 'Monitor Bot', 'hourly');

    const events = service.getWeekEvents(0);
    assert.ok(Array.isArray(events), 'should return an array');

    // Hourly tasks should generate multiple week events
    if (events.length > 0) {
      const event = events[0];
      assert.ok(typeof event.id === 'string');
      assert.ok(typeof event.agentName === 'string');
      assert.ok(typeof event.day === 'number');
      assert.ok(event.day >= 0 && event.day <= 6);
      assert.ok(typeof event.hour === 'number');
      assert.ok(event.hour >= 0 && event.hour <= 23);
      assert.ok(typeof event.label === 'string');
      assert.ok(event.label.includes('monitor-bot'));
    }
  });

  it('getMissed() detects overdue tasks', () => {
    // Directly add an overdue task via the store
    const store = new CalendarStore(calendarFile);
    store.add({
      id: 'overdue-task',
      agentSlug: 'slow-agent',
      type: 'heartbeat',
      cronExpression: 'daily',
      nextRun: Date.now() - 120_000,
      missed: false,
      enabled: true,
    });

    const missed = service.getMissed();
    assert.equal(missed.length, 1);
    assert.equal(missed[0].id, 'overdue-task');
    assert.equal(missed[0].agentSlug, 'slow-agent');
  });

  it('getMissed() ignores disabled tasks', () => {
    const store = new CalendarStore(calendarFile);
    store.add({
      id: 'disabled-overdue',
      agentSlug: 'disabled-agent',
      type: 'heartbeat',
      cronExpression: 'daily',
      nextRun: Date.now() - 120_000,
      missed: false,
      enabled: false,
    });

    const missed = service.getMissed();
    assert.equal(missed.length, 0);
  });

  it('removeSchedule() clears agent schedules', () => {
    service.scheduleHeartbeat('remove-me', 'Remove Me', 'hourly');

    // Verify it was created
    const beforeUpcoming = service.getUpcoming(48);
    assert.ok(
      beforeUpcoming.some(t => t.agentSlug === 'remove-me'),
      'task should exist before removal',
    );

    service.removeSchedule('remove-me');

    const afterUpcoming = service.getUpcoming(48);
    assert.ok(
      !afterUpcoming.some(t => t.agentSlug === 'remove-me'),
      'task should not exist after removal',
    );
  });

  it('subscribe/notify works', () => {
    let callCount = 0;
    const unsubscribe = service.subscribe(() => {
      callCount++;
    });

    service.scheduleHeartbeat('notifier', 'Notifier', 'daily');
    assert.equal(callCount, 1);

    service.removeSchedule('notifier');
    assert.equal(callCount, 2);

    unsubscribe();
    service.scheduleHeartbeat('silent', 'Silent', 'hourly');
    assert.equal(callCount, 2, 'should not notify after unsubscribe');
  });

  it('getUpcoming() with custom hours window', () => {
    service.scheduleHeartbeat('upcoming-bot', 'Upcoming Bot', 'hourly');

    // With a 1-hour window, we should get at most 1 task
    const shortWindow = service.getUpcoming(1);
    assert.ok(shortWindow.length <= 1, 'short window should return 0-1 tasks');

    // With a 48-hour window, we should get the task
    const longWindow = service.getUpcoming(48);
    assert.ok(longWindow.length >= 1, 'long window should return at least 1 task');
  });
});
