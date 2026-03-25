/**
 * Objective-first delegation: Team-Lead decomposes objectives
 * and routes sub-tasks to specialist agents via steer messages.
 *
 * Uses the orchestration TaskRouter for skill matching and
 * MessageQueues for non-blocking delegation.
 */

import type { AgentConfig } from './types.js';
import type { MessageQueues } from './queues.js';
import { TaskRouter } from '../orchestration/router.js';
import type { SkillDeclaration, TaskDefinition } from '../orchestration/types.js';
import { getAgentService } from '../tui/services/agent-service.js';

/** Map agent expertise strings to SkillDeclarations for the router. */
function expertiseToSkills(expertise: string[]): SkillDeclaration[] {
  return expertise.map(e => ({
    name: e,
    description: e,
    proficiency: 0.8,
  }));
}

/** Extract actionable sub-tasks from the Team-Lead's response text. */
export function extractSubTasks(response: string): Array<{ action: string; skills: string[] }> {
  const subTasks: Array<{ action: string; skills: string[] }> = [];

  // Skill keyword mapping — matches action words to required expertise
  const skillPatterns: Array<{ pattern: RegExp; skills: string[] }> = [
    { pattern: /\b(research|investigate|analyze|analysis)\b/i, skills: ['research', 'market-research', 'competitive-analysis'] },
    { pattern: /\b(find leads|generate leads|prospect|outreach|prospecting)\b/i, skills: ['prospecting', 'enrichment', 'outreach'] },
    { pattern: /\b(enrich|validate|fill in|complete data|data quality)\b/i, skills: ['data-enrichment', 'research', 'validation'] },
    { pattern: /\b(seo|keyword|search visibility|content.?analysis|ranking)\b/i, skills: ['seo', 'content-analysis', 'keyword-research'] },
    { pattern: /\b(customer success|health score|renewal|churn|retention)\b/i, skills: ['relationship-management', 'health-scoring', 'renewal-tracking'] },
  ];

  // Split response into sentences and look for action-oriented ones
  const sentences = response.split(/[.!?\n]+/).map(s => s.trim()).filter(Boolean);

  for (const sentence of sentences) {
    for (const { pattern, skills } of skillPatterns) {
      if (pattern.test(sentence)) {
        // Avoid duplicates
        if (!subTasks.some(t => t.action === sentence)) {
          subTasks.push({ action: sentence, skills });
        }
      }
    }
  }

  return subTasks;
}

export interface Specialist {
  config: AgentConfig;
}

/**
 * Delegate sub-tasks to specialist agents.
 * Non-blocking: sends steer messages to matching specialists
 * so they pick up work on their next tick.
 */
export function delegateToSpecialists(
  response: string,
  specialists: Specialist[],
  queues: MessageQueues,
): Array<{ agentSlug: string; task: string }> {
  if (specialists.length === 0) return [];

  const subTasks = extractSubTasks(response);
  if (subTasks.length === 0) return [];

  // Build a router with registered specialists
  const router = new TaskRouter();
  for (const spec of specialists) {
    router.registerAgent(spec.config.slug, expertiseToSkills(spec.config.expertise));
  }

  const delegated: Array<{ agentSlug: string; task: string }> = [];

  for (const subTask of subTasks) {
    const taskDef: TaskDefinition = {
      id: `delegate-${Date.now()}-${Math.random().toString(36).slice(2, 8)}`,
      title: subTask.action,
      description: subTask.action,
      requiredSkills: subTask.skills,
      priority: 'medium',
      status: 'pending',
      createdAt: Date.now(),
    };

    const best = router.findBestAgent(taskDef);
    if (best && best.score > 0.1) {
      // Steer the specialist with the sub-task
      queues.steer(best.agentSlug, `[TEAM-LEAD DELEGATION] ${subTask.action}`);
      delegated.push({ agentSlug: best.agentSlug, task: subTask.action });

      // Ensure specialist agent tick loop is running
      try {
        const agentService = getAgentService();
        agentService.ensureRunning(best.agentSlug);
      } catch {
        // AgentService may not be initialized yet — delegation still succeeds via queue
      }
    }
  }

  return delegated;
}
