/**
 * MessageRouter: routes incoming messages to the best-fit agent
 * using TaskRouter for skill scoring and thread-detection heuristics
 * for follow-up messages.
 */

import { TaskRouter } from '../orchestration/router.js';
import type { TaskDefinition } from '../orchestration/types.js';

interface ThreadContext {
  agentSlug: string;
  lastActivity: number;
}

export interface RoutingResult {
  primary: string; // agent slug
  collaborators: string[];
  isFollowUp: boolean;
  teamLeadAware: boolean;
}

export class MessageRouter {
  private router = new TaskRouter();
  private recentThreads = new Map<string, ThreadContext>();
  private FOLLOWUP_WINDOW_MS = 30_000;

  registerAgent(slug: string, expertise: string[]): void {
    this.router.registerAgent(
      slug,
      expertise.map((e) => ({ name: e, description: e, proficiency: 0.8 })),
    );
  }

  unregisterAgent(slug: string): void {
    this.router.unregisterAgent(slug);
  }

  recordAgentActivity(agentSlug: string): void {
    this.recentThreads.set(agentSlug, { agentSlug, lastActivity: Date.now() });
  }

  route(
    message: string,
    availableAgents: Array<{ slug: string; expertise: string[] }>,
  ): RoutingResult {
    // Step 1: Thread detection
    const followUp = this.detectFollowUp(message);
    if (followUp) {
      return { primary: followUp, collaborators: [], isFollowUp: true, teamLeadAware: false };
    }

    // Step 2: Extract skills from message using keyword patterns
    const requiredSkills = this.extractSkills(message);

    // No recognized skills → route to team-lead for triage
    if (requiredSkills.length === 0) {
      return { primary: 'team-lead', collaborators: [], isFollowUp: false, teamLeadAware: false };
    }

    // Step 3: Register agents (ensure current state) and score
    for (const agent of availableAgents) {
      this.registerAgent(agent.slug, agent.expertise);
    }

    const task: TaskDefinition = {
      id: `route-${Date.now()}`,
      title: message,
      description: message,
      requiredSkills,
      priority: 'medium',
      status: 'pending',
      createdAt: Date.now(),
    };

    const capable = this.router.findCapableAgents(task);

    if (capable.length === 0) {
      return { primary: 'team-lead', collaborators: [], isFollowUp: false, teamLeadAware: false };
    }

    const primary = capable[0].agentSlug;
    const collaborators = capable
      .slice(1)
      .filter((c) => c.score > 0.5)
      .map((c) => c.agentSlug);

    return {
      primary,
      collaborators,
      isFollowUp: false,
      teamLeadAware: primary !== 'team-lead',
    };
  }

  private detectFollowUp(message: string): string | null {
    const now = Date.now();
    const followUpPatterns =
      /^(also|and |too |that |it |the results|those |these |this |what about|how about|can you also)/i;

    if (!followUpPatterns.test(message.trim())) return null;

    // Find most recent active thread within window
    let mostRecent: ThreadContext | null = null;
    for (const [, ctx] of this.recentThreads) {
      if (now - ctx.lastActivity <= this.FOLLOWUP_WINDOW_MS) {
        if (!mostRecent || ctx.lastActivity > mostRecent.lastActivity) {
          mostRecent = ctx;
        }
      }
    }

    return mostRecent?.agentSlug ?? null;
  }

  /** Extract skill keywords from a message using pattern matching. */
  extractSkills(message: string): string[] {
    const skillPatterns: Array<{ pattern: RegExp; skills: string[] }> = [
      {
        pattern: /\b(research|investigate|analyze|analysis|competitor)\b/i,
        skills: ['market-research', 'competitive-analysis'],
      },
      {
        pattern: /\b(leads?|prospect|outreach|prospecting|pipeline)\b/i,
        skills: ['prospecting', 'outreach'],
      },
      {
        pattern: /\b(enrich|validate|data quality|fill in|complete)\b/i,
        skills: ['data-enrichment', 'validation'],
      },
      {
        pattern: /\b(seo|keyword|search visibility|ranking|content)\b/i,
        skills: ['seo', 'content-analysis'],
      },
      {
        pattern: /\b(customer|success|health|renewal|churn|retention)\b/i,
        skills: ['relationship-management', 'health-scoring'],
      },
      {
        pattern: /\b(code|bug|fix|implement|build|deploy|test)\b/i,
        skills: ['general', 'planning'],
      },
    ];

    const skills: string[] = [];
    for (const { pattern, skills: matchedSkills } of skillPatterns) {
      if (pattern.test(message)) {
        skills.push(...matchedSkills);
      }
    }
    return [...new Set(skills)];
  }
}
