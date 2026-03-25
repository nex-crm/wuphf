import { describe, it } from 'node:test';
import assert from 'node:assert/strict';
import React from 'react';
import { render } from 'ink-testing-library';
import { InsightsView } from '../../../src/tui/views/insights.js';
import type { Insight } from '../../../src/tui/views/insights.js';

// Strip ANSI escape sequences for assertion matching
function strip(s: string): string {
  return s.replace(/\x1b\[[0-9;]*m/g, '');
}

describe('InsightsView', () => {
  const sampleInsights: Insight[] = [
    {
      id: 'ins-1',
      title: 'Pipeline stall detected',
      body: 'The ingest pipeline has been stalled for 2+ hours on the Acme Corp import.',
      priority: 'critical',
      category: 'pipeline',
      recordIds: ['rec-abc', 'rec-def'],
      timestamp: new Date(Date.now() - 30 * 60_000).toISOString(), // 30m ago
    },
    {
      id: 'ins-2',
      title: 'New competitor mentioned',
      body: 'A new competitor "RivalCo" was mentioned in 3 recent emails.',
      priority: 'high',
      category: 'competitive',
      recordIds: ['rec-xyz'],
      timestamp: new Date(Date.now() - 3 * 3_600_000).toISOString(), // 3h ago
    },
    {
      id: 'ins-3',
      title: 'Contact engagement drop',
      body: 'Email open rates for the Enterprise segment dropped 15% this week.',
      priority: 'medium',
      category: 'engagement',
      recordIds: [],
      timestamp: new Date(Date.now() - 25 * 3_600_000).toISOString(), // ~1d ago
    },
    {
      id: 'ins-4',
      title: 'Calendar sync healthy',
      body: 'All calendar integrations are syncing normally.',
      priority: 'low',
      category: 'system',
      timestamp: new Date(Date.now() - 5 * 60_000).toISOString(), // 5m ago
    },
  ];

  it('renders the Insights title', () => {
    const { lastFrame } = render(<InsightsView insights={sampleInsights} />);
    const frame = strip(lastFrame() ?? '');
    assert.ok(frame.includes('Insights'), 'should show Insights title');
  });

  it('shows insight count', () => {
    const { lastFrame } = render(<InsightsView insights={sampleInsights} />);
    const frame = strip(lastFrame() ?? '');
    assert.ok(frame.includes('4 insights'), 'should show insight count');
  });

  it('renders priority badges', () => {
    const { lastFrame } = render(<InsightsView insights={sampleInsights} />);
    const frame = strip(lastFrame() ?? '');
    assert.ok(frame.includes('[CRIT]'), 'should show CRIT badge for critical');
    assert.ok(frame.includes('[HIGH]'), 'should show HIGH badge for high');
    assert.ok(frame.includes('[MED]'), 'should show MED badge for medium');
    assert.ok(frame.includes('[LOW]'), 'should show LOW badge for low');
  });

  it('renders category brackets', () => {
    const { lastFrame } = render(<InsightsView insights={sampleInsights} />);
    const frame = strip(lastFrame() ?? '');
    assert.ok(frame.includes('[pipeline]'), 'should show pipeline category');
    assert.ok(frame.includes('[competitive]'), 'should show competitive category');
    assert.ok(frame.includes('[engagement]'), 'should show engagement category');
    assert.ok(frame.includes('[system]'), 'should show system category');
  });

  it('renders insight titles', () => {
    const { lastFrame } = render(<InsightsView insights={sampleInsights} />);
    const frame = strip(lastFrame() ?? '');
    assert.ok(frame.includes('Pipeline stall detected'), 'should show first title');
    assert.ok(frame.includes('New competitor mentioned'), 'should show second title');
    assert.ok(frame.includes('Contact engagement drop'), 'should show third title');
    assert.ok(frame.includes('Calendar sync healthy'), 'should show fourth title');
  });

  it('renders insight body text', () => {
    const { lastFrame } = render(<InsightsView insights={sampleInsights} />);
    const frame = strip(lastFrame() ?? '');
    assert.ok(frame.includes('ingest pipeline has been stalled'), 'should show first body');
    assert.ok(frame.includes('RivalCo'), 'should show competitor name in body');
  });

  it('renders record IDs when present', () => {
    const { lastFrame } = render(<InsightsView insights={sampleInsights} />);
    const frame = strip(lastFrame() ?? '');
    assert.ok(frame.includes('rec-abc'), 'should show first record ID');
    assert.ok(frame.includes('rec-def'), 'should show second record ID');
    assert.ok(frame.includes('rec-xyz'), 'should show third record ID');
  });

  it('renders relative timestamps', () => {
    const { lastFrame } = render(<InsightsView insights={sampleInsights} />);
    const frame = strip(lastFrame() ?? '');
    assert.ok(frame.includes('30m ago'), 'should show 30m ago for critical insight');
    assert.ok(frame.includes('3h ago'), 'should show 3h ago for high insight');
    assert.ok(frame.includes('1d ago'), 'should show 1d ago for medium insight');
    assert.ok(frame.includes('5m ago'), 'should show 5m ago for low insight');
  });

  it('shows empty state when no insights', () => {
    const { lastFrame } = render(<InsightsView insights={[]} />);
    const frame = strip(lastFrame() ?? '');
    assert.ok(frame.includes('No insights found'), 'should show empty state text');
  });

  it('renders keyboard hints', () => {
    const { lastFrame } = render(<InsightsView insights={[]} />);
    const frame = strip(lastFrame() ?? '');
    assert.ok(frame.includes('Esc=back'), 'should show escape hint');
  });

  it('handles single insight count grammar', () => {
    const single: Insight[] = [{
      id: 'ins-solo',
      title: 'Solo insight',
      body: 'Only one',
      priority: 'medium',
      category: 'test',
      timestamp: new Date().toISOString(),
    }];
    const { lastFrame } = render(<InsightsView insights={single} />);
    const frame = strip(lastFrame() ?? '');
    assert.ok(frame.includes('1 insight'), 'should show singular "insight"');
    assert.ok(!frame.includes('1 insights'), 'should not show plural for single');
  });
});
