/**
 * Resolve the display order for thematic dir groups in the wiki UI.
 *
 * PREFERRED_ORDER lists the common starter-pack groups in the order we want them
 * shown when present. Blueprint-specific groups (customers, videos, members,
 * scripts, etc.) aren't in this list — they get appended in the order they
 * first appear in the catalog so the UI never hides real articles.
 *
 * Example: catalog = [{group: 'customers'}, {group: 'playbooks'}, {group: 'scripts'}]
 *   resolveGroupOrder(catalog) → ['playbooks', 'customers', 'scripts']
 */

const PREFERRED_ORDER = [
  'people',
  'companies',
  'customers',
  'projects',
  'playbooks',
  'decisions',
  'inbox',
] as const

export function resolveGroupOrder(groups: Iterable<string>): string[] {
  const seen = new Set<string>()
  const present = new Set<string>()
  for (const g of groups) present.add(g)

  const ordered: string[] = []

  // Preferred groups first, in their canonical order, only if the catalog has them.
  for (const g of PREFERRED_ORDER) {
    if (present.has(g)) {
      ordered.push(g)
      seen.add(g)
    }
  }
  // Everything else in first-seen order so blueprint-specific groups (scripts,
  // videos, members, etc.) still render.
  for (const g of groups) {
    if (!seen.has(g)) {
      ordered.push(g)
      seen.add(g)
    }
  }
  return ordered
}
