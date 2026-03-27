const OPEN_TAG = "<wuphf-context>";
const CLOSE_TAG = "</wuphf-context>";

export interface NexRecallResult {
  answer: string;
  entityCount: number;
  sessionId?: string;
}

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

export function stripNexContext(text: string): string {
  let result = text.replace(/<wuphf-context>[\s\S]*?<\/wuphf-context>/g, "");
  result = result.replace(/<wuphf-context>[\s\S]*/g, "");
  return result.trim();
}
