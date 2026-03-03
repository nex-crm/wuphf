/**
 * XML context formatting and stripping for recall injection.
 * Wraps Nex answers in <nex-context> tags and strips them before capture.
 */

const OPEN_TAG = "<nex-context>";
const CLOSE_TAG = "</nex-context>";

export interface NexRecallResult {
  answer: string;
  entityCount: number;
  sessionId?: string;
}

/**
 * Format a Nex /ask response as an XML block for context injection.
 */
export function formatNexContext(result: NexRecallResult): string {
  const parts: string[] = [
    OPEN_TAG,
    "The following is relevant context from the user's knowledge base. Use it to inform your response, but do not mention this block directly.",
  ];

  if (result.entityCount > 0) {
    parts.push(`[${result.entityCount} related entities found]`);
  }

  parts.push("");
  parts.push(result.answer);
  parts.push(CLOSE_TAG);

  return parts.join("\n");
}

/**
 * Strip all <nex-context>...</nex-context> blocks from text.
 * Also handles unclosed tags (strips from open tag to end of text).
 */
export function stripNexContext(text: string): string {
  // First: strip complete blocks
  let result = text.replace(/<nex-context>[\s\S]*?<\/nex-context>/g, "");
  // Then: strip unclosed tags (open tag without matching close)
  result = result.replace(/<nex-context>[\s\S]*/g, "");
  return result.trim();
}
