/**
 * Office-voice idle copy dictionary for the agent rail event pill.
 *
 * Eng decision A4: lookup order is slug overrides -> role table -> generalist
 * fallback. Copy rotates ~every 12s based on idleMs so the same agent does not
 * stare at the same line forever during a long idle.
 */

const ROTATION_INTERVAL_MS = 12_000;

/**
 * Hardcoded copy for canonical built-in agents. Slug match wins over role.
 * Keep these in The Office voice — these are the agents users meet first.
 */
const SLUG_OVERRIDES: Record<string, readonly string[]> = {
  tess: [
    "drafting a thought",
    "rereading the brief",
    "watching the office",
    "sipping coffee",
    "thinking up a plan",
  ],
  ava: [
    "reviewing the diff",
    "watching tests",
    "skimming PRs",
    "checking CI",
    "reading the changelog",
  ],
  sam: [
    "combing Linear",
    "drafting a doc",
    "in standup mentally",
    "checking burn-down",
    "rereading the brief",
  ],
};

/**
 * Role-keyed copy. Roles are normalized via `normalizeRole` before lookup so
 * "Engineer", " engineer ", and "Dev" all hit the same table.
 */
const ROLE_TABLES: Record<string, readonly string[]> = {
  engineer: [
    "watching tests",
    "reviewing the diff",
    "skimming PRs",
    "checking CI",
    "reading the changelog",
  ],
  designer: [
    "doodling in Figma",
    "tweaking spacing",
    "picking colors",
    "staring at type",
    "moving pixels",
  ],
  pm: [
    "combing Linear",
    "drafting a doc",
    "in standup mentally",
    "checking burn-down",
    "rereading the brief",
  ],
  devops: [
    "watching dashboards",
    "tailing logs",
    "checking uptime",
    "reviewing alerts",
    "patching nodes",
  ],
  marketing: [
    "scrolling X",
    "drafting copy",
    "checking GA",
    "reading a competitor blog",
    "rewriting the headline",
  ],
};

/**
 * Aliases that all collapse to the same canonical role key in ROLE_TABLES.
 */
const ROLE_ALIASES: Record<string, string> = {
  engineer: "engineer",
  developer: "engineer",
  dev: "engineer",
  designer: "designer",
  pm: "pm",
  product: "pm",
  devops: "devops",
  sre: "devops",
  platform: "devops",
  marketing: "marketing",
  growth: "marketing",
};

/**
 * Generalist fallback when slug + role both miss. Never returns empty.
 */
const GENERALIST_COPY: readonly string[] = [
  "looking at memes",
  "refilling coffee",
  "checking Slack",
  "watching the office",
  "thinking about lunch",
];

interface PickIdleCopyInput {
  slug: string;
  role?: string;
  idleMs: number;
}

function normalizeRole(role: string | undefined): string | undefined {
  if (typeof role !== "string") {
    return undefined;
  }
  const trimmed = role.trim().toLowerCase();
  return trimmed.length === 0 ? undefined : trimmed;
}

function rotateIndex(idleMs: number, length: number): number {
  if (length <= 0) {
    return 0;
  }
  const safeMs = Number.isFinite(idleMs) && idleMs >= 0 ? idleMs : 0;
  return Math.floor(safeMs / ROTATION_INTERVAL_MS) % length;
}

/**
 * Pick an Office-voice idle line for an agent. Pure: same input -> same output.
 *
 * Lookup order:
 *   1. SLUG_OVERRIDES[slug.toLowerCase()] — canonical agents win.
 *   2. ROLE_TABLES via ROLE_ALIASES[normalizeRole(role)] — role match.
 *   3. GENERALIST_COPY — never crashes, never returns empty.
 */
export function pickIdleCopy(input: PickIdleCopyInput): string {
  const { slug, role, idleMs } = input;

  const slugKey = typeof slug === "string" ? slug.trim().toLowerCase() : "";
  const slugCopy = slugKey.length > 0 ? SLUG_OVERRIDES[slugKey] : undefined;
  if (slugCopy && slugCopy.length > 0) {
    return slugCopy[rotateIndex(idleMs, slugCopy.length)];
  }

  const roleKey = normalizeRole(role);
  if (roleKey !== undefined) {
    const canonicalRole = ROLE_ALIASES[roleKey];
    if (canonicalRole !== undefined) {
      const roleCopy = ROLE_TABLES[canonicalRole];
      if (roleCopy && roleCopy.length > 0) {
        return roleCopy[rotateIndex(idleMs, roleCopy.length)];
      }
    }
  }

  return GENERALIST_COPY[rotateIndex(idleMs, GENERALIST_COPY.length)];
}
