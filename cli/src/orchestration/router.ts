/**
 * Expertise-based task routing.
 * Matches agent skills to task requirements using fuzzy string matching
 * weighted by proficiency scores.
 */

import type { SkillDeclaration, TaskDefinition } from './types.js';

interface AgentRegistration {
  slug: string;
  skills: SkillDeclaration[];
}

/**
 * Simple normalized string similarity (Dice coefficient on bigrams).
 * Returns 0-1, where 1 is a perfect match.
 */
function similarity(a: string, b: string): number {
  const sa = a.toLowerCase().trim();
  const sb = b.toLowerCase().trim();
  if (sa === sb) return 1;
  if (sa.length < 2 || sb.length < 2) return 0;

  const bigramsA = new Set<string>();
  for (let i = 0; i < sa.length - 1; i++) bigramsA.add(sa.slice(i, i + 2));

  const bigramsB = new Set<string>();
  for (let i = 0; i < sb.length - 1; i++) bigramsB.add(sb.slice(i, i + 2));

  let intersection = 0;
  for (const bg of bigramsA) {
    if (bigramsB.has(bg)) intersection++;
  }

  return (2 * intersection) / (bigramsA.size + bigramsB.size);
}

export class TaskRouter {
  private agents: Map<string, AgentRegistration> = new Map();

  /** Register an agent with its skill declarations. */
  registerAgent(agentSlug: string, skills: SkillDeclaration[]): void {
    this.agents.set(agentSlug, { slug: agentSlug, skills });
  }

  /** Unregister an agent. */
  unregisterAgent(agentSlug: string): void {
    this.agents.delete(agentSlug);
  }

  /**
   * Score a single agent against a task.
   * For each required skill, find the best matching agent skill (fuzzy match).
   * Score = average of best matches * agent proficiency average.
   * Agents with 0 matching skills (all similarities below threshold) get 0.
   */
  scoreMatch(agentSlug: string, task: TaskDefinition): number {
    const reg = this.agents.get(agentSlug);
    if (!reg) return 0;

    const required = task.requiredSkills;
    // If task requires no skills, any agent can handle it
    if (required.length === 0) return 1;

    const agentSkills = reg.skills;
    if (agentSkills.length === 0) return 0;

    const MATCH_THRESHOLD = 0.3;
    let matchSum = 0;
    let matchCount = 0;

    for (const reqSkill of required) {
      let bestSim = 0;
      let bestProf = 0;
      for (const agentSkill of agentSkills) {
        const sim = similarity(reqSkill, agentSkill.name);
        if (sim > bestSim) {
          bestSim = sim;
          bestProf = agentSkill.proficiency;
        }
      }
      if (bestSim >= MATCH_THRESHOLD) {
        matchSum += bestSim * bestProf;
        matchCount++;
      }
    }

    // If no skills matched above threshold, return 0
    if (matchCount === 0) return 0;

    return matchSum / required.length;
  }

  /** Find the single best agent for a task, or null if none can match. */
  findBestAgent(
    task: TaskDefinition,
  ): { agentSlug: string; score: number } | null {
    const ranked = this.findCapableAgents(task);
    return ranked.length > 0 ? ranked[0] : null;
  }

  /** Find all capable agents, ranked by match score descending. */
  findCapableAgents(
    task: TaskDefinition,
  ): Array<{ agentSlug: string; score: number }> {
    const results: Array<{ agentSlug: string; score: number }> = [];

    for (const [slug] of this.agents) {
      const score = this.scoreMatch(slug, task);
      if (score > 0) {
        results.push({ agentSlug: slug, score });
      }
    }

    results.sort((a, b) => b.score - a.score);
    return results;
  }
}
