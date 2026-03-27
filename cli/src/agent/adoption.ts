/**
 * Selective insight adoption scoring and credibility tracking.
 * Determines whether an agent should adopt, test, or reject a gossip insight.
 */

import { readFileSync, writeFileSync, mkdirSync, existsSync } from 'node:fs';
import { join } from 'node:path';
import { homedir } from 'node:os';
import type { GossipInsight } from './gossip.js';

export interface AdoptionScore {
  sourceCredibility: number;
  semanticRelevance: number;
  temporalFreshness: number;
  total: number;
  decision: 'adopt' | 'test' | 'reject';
}

const CREDIBILITY_WEIGHT = 0.4;
const RELEVANCE_WEIGHT = 0.4;
const FRESHNESS_WEIGHT = 0.2;
const ADOPT_THRESHOLD = 0.7;
const TEST_THRESHOLD = 0.4;

// Insights older than 7 days start losing freshness
const FRESHNESS_HALF_LIFE_MS = 7 * 24 * 60 * 60 * 1000;

export function scoreInsight(
  insight: GossipInsight,
  _currentContext: string,
  sourceCredibility?: number,
): AdoptionScore {
  const credibility = sourceCredibility ?? 0.5;
  const semanticRelevance = Math.max(0, Math.min(1, insight.relevance));

  const age = Date.now() - insight.timestamp;
  const temporalFreshness = Math.max(0, Math.exp(-age / FRESHNESS_HALF_LIFE_MS));

  const total =
    credibility * CREDIBILITY_WEIGHT +
    semanticRelevance * RELEVANCE_WEIGHT +
    temporalFreshness * FRESHNESS_WEIGHT;

  let decision: 'adopt' | 'test' | 'reject';
  if (total >= ADOPT_THRESHOLD) {
    decision = 'adopt';
  } else if (total >= TEST_THRESHOLD) {
    decision = 'test';
  } else {
    decision = 'reject';
  }

  return {
    sourceCredibility: credibility,
    semanticRelevance,
    temporalFreshness,
    total,
    decision,
  };
}

interface CredibilityRecord {
  successes: number;
  failures: number;
}

export class CredibilityTracker {
  private baseDir: string;
  private filePath: string;
  private data: Record<string, CredibilityRecord>;

  constructor(baseDir?: string) {
    this.baseDir = baseDir ?? join(homedir(), '.wuphf', 'credibility');
    this.filePath = join(this.baseDir, 'scores.json');
    this.data = this.load();
  }

  private load(): Record<string, CredibilityRecord> {
    try {
      if (!existsSync(this.filePath)) return {};
      const raw = readFileSync(this.filePath, 'utf-8');
      return JSON.parse(raw) as Record<string, CredibilityRecord>;
    } catch {
      return {};
    }
  }

  private save(): void {
    mkdirSync(this.baseDir, { recursive: true });
    writeFileSync(this.filePath, JSON.stringify(this.data, null, 2) + '\n', 'utf-8');
  }

  getCredibility(agentSlug: string): number {
    const record = this.data[agentSlug];
    if (!record) return 0.5;

    const total = record.successes + record.failures;
    if (total === 0) return 0.5;

    return record.successes / total;
  }

  recordOutcome(agentSlug: string, success: boolean): void {
    if (!this.data[agentSlug]) {
      this.data[agentSlug] = { successes: 0, failures: 0 };
    }
    if (success) {
      this.data[agentSlug].successes++;
    } else {
      this.data[agentSlug].failures++;
    }
    this.save();
  }
}
