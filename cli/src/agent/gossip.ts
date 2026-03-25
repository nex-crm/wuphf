/**
 * Gossip layer for knowledge propagation between agents.
 * Uses WUPHF Ask/Remember APIs to publish and query insights.
 */

import type { NexClient } from '../lib/client.js';

export interface GossipInsight {
  content: string;
  source: string;
  timestamp: number;
  relevance: number;
}

export class GossipLayer {
  private client: NexClient;

  constructor(client: NexClient) {
    this.client = client;
  }

  async publish(agentSlug: string, insight: string, context?: string): Promise<string> {
    const taggedContent = `[agent:${agentSlug}] ${insight}`;
    const tags = ['gossip', `agent:${agentSlug}`];
    if (context) tags.push(`ctx:${context}`);

    const result = await this.client.post<{ id?: string }>('/remember', {
      content: taggedContent,
      tags,
    });

    return result.id ?? 'stored';
  }

  async query(agentSlug: string, topic: string): Promise<GossipInsight[]> {
    const result = await this.client.post<{
      results?: Array<{
        content?: string;
        score?: number;
        metadata?: Record<string, unknown>;
      }>;
    }>('/search', {
      query: `[gossip] ${topic}`,
      limit: 10,
    });

    if (!result.results || !Array.isArray(result.results)) return [];

    return result.results
      .filter(r => {
        // Exclude the querying agent's own insights
        const content = r.content ?? '';
        return !content.startsWith(`[agent:${agentSlug}]`);
      })
      .map(r => {
        const content = r.content ?? '';
        const sourceMatch = content.match(/^\[agent:([^\]]+)\]/);
        const source = sourceMatch ? sourceMatch[1] : 'unknown';
        const cleanContent = content.replace(/^\[agent:[^\]]+\]\s*/, '');

        return {
          content: cleanContent,
          source,
          timestamp: Date.now(),
          relevance: r.score ?? 0,
        };
      });
  }
}
