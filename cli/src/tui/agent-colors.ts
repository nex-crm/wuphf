/**
 * Persistent agent color assignment.
 *
 * Each agent slug gets a stable color from a palette of six terminal-safe
 * colours. Assignments survive re-renders (module-level map) and cycle
 * through the palette so every new agent gets the next colour in order.
 */

const AGENT_COLORS = ['cyan', 'green', 'yellow', 'magenta', 'blue', 'red'] as const;
export type AgentColor = typeof AGENT_COLORS[number];

// Module-level state so colours persist across renders.
const colorMap = new Map<string, AgentColor>();
let nextColorIdx = 0;

/**
 * Return the colour assigned to `agentSlug`, assigning a new one on first
 * encounter. The same slug always returns the same colour.
 */
export function getAgentColor(agentSlug: string): AgentColor {
  const existing = colorMap.get(agentSlug);
  if (existing !== undefined) return existing;

  const color = AGENT_COLORS[nextColorIdx % AGENT_COLORS.length];
  colorMap.set(agentSlug, color);
  nextColorIdx++;
  return color;
}

/** Read-only snapshot of the current colour map. */
export function getAllAgentColors(): ReadonlyMap<string, AgentColor> {
  return new Map(colorMap);
}

/** Reset all assignments (useful in tests). */
export function resetAgentColors(): void {
  colorMap.clear();
  nextColorIdx = 0;
}
