/**
 * Format an agent slug for display.
 *
 * Short role abbreviations (ceo, pm, cro, cmo, seo, api, ux — up to 3 chars)
 * render UPPERCASE: matches how the WUPHF app treats short agent identifiers.
 *
 * Longer slugs (operator, planner, builder, reviewer, eng-1) render Title Case:
 * "Operator", "Planner", "Eng-1".
 *
 * Example:
 *   formatAgentName('ceo')      -> 'CEO'
 *   formatAgentName('operator') -> 'Operator'
 *   formatAgentName('eng-1')    -> 'Eng-1'
 */
export function formatAgentName(slug: string): string {
  if (!slug) return "";
  if (slug.length <= 3) return slug.toUpperCase();
  return slug
    .split("-")
    .map((part) =>
      part.length === 0
        ? part
        : part[0].toUpperCase() + part.slice(1).toLowerCase(),
    )
    .join("-");
}
