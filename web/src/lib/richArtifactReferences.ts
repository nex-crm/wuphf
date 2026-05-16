const RICH_ARTIFACT_ID = "ra_[0-9a-f]{16}";

const EXPLICIT_REFERENCE_PATTERN =
  "(?:\\b(?:visual|rich|html|notebook)[\\s_-]*artifact(?:[\\s_-]*(?:ref|reference|id))?\\b\\s*[:=#-]?\\s*|#?/?(?:notebook|wiki)/visual-artifacts/)(" +
  RICH_ARTIFACT_ID +
  ")(?:\\.html)?";

const REFERENCE_LINE_FILLER_RE =
  /\b(?:visual|rich|html|notebook|wiki|artifact|artifacts|reference|ref|id|open|view|see|link)\b/gi;

type FenceToken = "`" | "~";

function referenceRegex(): RegExp {
  return new RegExp(EXPLICIT_REFERENCE_PATTERN, "gi");
}

export function extractRichArtifactIds(content: string): string[] {
  const seen = new Set<string>();
  const ids: string[] = [];
  forEachNonFenceLine(content, (line) => {
    for (const match of line.matchAll(referenceRegex())) {
      const id = match[1]?.toLowerCase();
      if (!id || seen.has(id)) continue;
      seen.add(id);
      ids.push(id);
    }
  });
  return ids;
}

export function stripStandaloneRichArtifactReferenceLines(
  content: string,
): string {
  const lines = content.split(/\r?\n/);
  let inFence: FenceToken | null = null;
  let removedReferenceLine = false;
  const kept: string[] = [];
  for (const line of lines) {
    const token = fenceToken(line);
    if (token) {
      if (!inFence) {
        inFence = token;
      } else if (inFence === token) {
        inFence = null;
      }
      kept.push(line);
      continue;
    }
    if (!inFence && isStandaloneReferenceLine(line)) {
      removedReferenceLine = true;
      continue;
    }
    kept.push(line);
  }
  if (!removedReferenceLine) return content;
  return kept.join("\n").replace(/\n{3,}/g, "\n\n").trim();
}

function forEachNonFenceLine(
  content: string,
  visit: (line: string) => void,
): void {
  let inFence: FenceToken | null = null;
  for (const line of content.split(/\r?\n/)) {
    const token = fenceToken(line);
    if (token) {
      if (!inFence) {
        inFence = token;
      } else if (inFence === token) {
        inFence = null;
      }
      continue;
    }
    if (!inFence) visit(line);
  }
}

function fenceToken(line: string): FenceToken | null {
  const trimmed = line.trimStart();
  if (trimmed.startsWith("```")) return "`";
  if (trimmed.startsWith("~~~")) return "~";
  return null;
}

function isStandaloneReferenceLine(line: string): boolean {
  if (!referenceRegex().test(line)) return false;
  const withoutReferences = line.replace(referenceRegex(), "");
  const remaining = withoutReferences
    .replace(REFERENCE_LINE_FILLER_RE, "")
    .replace(/[\s()[\]{}<>`*_~|:;,.!?#="'/-]+/g, "");
  return remaining.length === 0;
}
