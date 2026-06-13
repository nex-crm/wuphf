/**
 * agentFileSections — parse the per-agent instruction files (SOUL / IDENTITY /
 * OPERATIONS / TOOLS / USER) into editable blocks and back, so the Config tab
 * can offer structured block editing instead of free-form whole-file editing
 * (which can silently destroy a file's structure).
 *
 * Design: parsing is CONTENT-DRIVEN and lossless — every `## ` section in the
 * file becomes a block, and anything before the first section is kept as the
 * preamble. The per-file SCHEMA is advisory: it supplies section hints and
 * surfaces expected-but-missing sections as empty blocks to fill. Sections that
 * aren't in the schema are preserved as "extra" blocks, never dropped.
 *
 * Section headings mirror the broker's seed renderers in
 * internal/team/agent_files.go (renderAgentSoul/Operations/Tools). Keep them in
 * sync so a freshly-seeded file maps cleanly onto its schema blocks.
 */

export interface AgentFileSection {
  /** The `## ` heading text (without the leading hashes). */
  heading: string;
  /** The section body (markdown), trimmed of surrounding blank lines. */
  body: string;
}

export interface ParsedAgentFile {
  /** The leading `# ` title line text, or "" if the file has none. */
  title: string;
  /** Content between the title and the first `## ` section. */
  preamble: string;
  sections: AgentFileSection[];
}

/** A schema entry: an expected section and a one-line hint for the editor. */
export interface SectionSchema {
  heading: string;
  hint: string;
}

/**
 * An editor block: a section the UI renders. `fromSchema` marks blocks the
 * schema expects (so the UI can show the hint and treat an empty one as
 * "to fill"); `extra` marks sections present in the file but not the schema.
 */
export interface EditorBlock {
  heading: string;
  body: string;
  hint?: string;
  fromSchema: boolean;
}

// Per-file section schemas. Keyed by the file label (SOUL/IDENTITY/...).
// IDENTITY and USER are flat (no `## ` sections in the seed) — they edit as a
// single preamble block, so their schema is empty.
export const AGENT_FILE_SECTION_SCHEMA: Record<string, SectionSchema[]> = {
  SOUL: [
    {
      heading: "Who you are",
      hint: "The agent's core identity, in a line or two.",
    },
    {
      heading: "Values",
      hint: "What it optimizes for — the principles that guide its judgment.",
    },
    { heading: "Voice", hint: "How it communicates: tone and directness." },
    {
      heading: "Boundaries",
      hint: "Hard limits — what it must never do, and when to escalate.",
    },
  ],
  OPERATIONS: [
    {
      heading: "How you work",
      hint: "The day-to-day operating procedure for this agent.",
    },
    {
      heading: "Escalation",
      hint: "When and how it escalates instead of proceeding alone.",
    },
  ],
  TOOLS: [
    { heading: "Available tools", hint: "The tools this agent may use." },
    {
      heading: "Notes",
      hint: "Usage guidance and constraints for those tools.",
    },
  ],
  IDENTITY: [],
  USER: [],
};

const TITLE_RE = /^#\s+(.*)$/;
const SECTION_RE = /^##\s+(.*)$/;

/**
 * Parse instruction-file markdown into a title, a preamble, and `## ` sections.
 * Lossless: re-serializing an unmodified parse yields equivalent markdown.
 */
export function parseAgentFile(markdown: string): ParsedAgentFile {
  const lines = (markdown ?? "").split("\n");
  let title = "";
  const preambleLines: string[] = [];
  const sections: AgentFileSection[] = [];
  let current: { heading: string; body: string[] } | null = null;
  let sawTitle = false;

  for (const line of lines) {
    if (!(sawTitle || current) && TITLE_RE.test(line)) {
      title = (line.match(TITLE_RE)?.[1] ?? "").trim();
      sawTitle = true;
      continue;
    }
    const sectionMatch = line.match(SECTION_RE);
    if (sectionMatch) {
      if (current) {
        sections.push({
          heading: current.heading,
          body: current.body.join("\n").trim(),
        });
      }
      current = { heading: sectionMatch[1].trim(), body: [] };
      continue;
    }
    if (current) {
      current.body.push(line);
    } else {
      preambleLines.push(line);
    }
  }
  if (current) {
    sections.push({
      heading: current.heading,
      body: current.body.join("\n").trim(),
    });
  }

  return {
    title,
    preamble: preambleLines.join("\n").trim(),
    sections,
  };
}

/** Serialize a parsed file (or edited blocks) back into instruction markdown. */
export function serializeAgentFile(parsed: {
  title: string;
  preamble: string;
  sections: AgentFileSection[];
}): string {
  const parts: string[] = [];
  if (parsed.title.trim()) parts.push(`# ${parsed.title.trim()}`);
  if (parsed.preamble.trim()) parts.push(parsed.preamble.trim());
  for (const section of parsed.sections) {
    const heading = section.heading.trim();
    if (!heading) continue;
    const body = section.body.trim();
    parts.push(body ? `## ${heading}\n${body}` : `## ${heading}`);
  }
  // Blocks are separated by a blank line; trailing newline matches the seed
  // renderers' file shape.
  return `${parts.join("\n\n")}\n`;
}

/**
 * Build the ordered editor blocks for a file: schema sections first (in schema
 * order, empty if absent from the file), then any extra sections the file
 * carries that the schema doesn't know about (preserved verbatim).
 */
export function buildEditorBlocks(
  parsed: ParsedAgentFile,
  schema: SectionSchema[],
): EditorBlock[] {
  // A schema heading binds to the FIRST parsed section with that heading.
  // Track consumption by index (not by heading) so that if a file carries
  // duplicate `## ` headings, every occurrence is preserved — the extras
  // beyond the first surface as their own "custom" blocks rather than being
  // silently collapsed into one (which would drop content on save).
  const firstIndexByHeading = new Map<string, number>();
  parsed.sections.forEach((s, i) => {
    const key = s.heading.toLowerCase();
    if (!firstIndexByHeading.has(key)) firstIndexByHeading.set(key, i);
  });

  const consumed = new Set<number>();
  const blocks: EditorBlock[] = [];

  for (const entry of schema) {
    const idx = firstIndexByHeading.get(entry.heading.toLowerCase());
    const found = idx === undefined ? undefined : parsed.sections[idx];
    if (idx !== undefined) consumed.add(idx);
    blocks.push({
      heading: entry.heading,
      body: found?.body ?? "",
      hint: entry.hint,
      fromSchema: true,
    });
  }
  parsed.sections.forEach((s, i) => {
    if (consumed.has(i)) return;
    blocks.push({ heading: s.heading, body: s.body, fromSchema: false });
  });
  return blocks;
}

/** Look up the schema for a file label; unknown labels get no suggested sections. */
export function schemaForFile(label: string): SectionSchema[] {
  return AGENT_FILE_SECTION_SCHEMA[label] ?? [];
}
