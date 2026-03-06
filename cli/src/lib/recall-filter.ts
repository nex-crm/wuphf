import { readFileSync, writeFileSync, mkdirSync } from "node:fs";
import { join } from "node:path";
import { homedir } from "node:os";

const DATA_DIR = join(homedir(), ".nex");
const STATE_FILE = join(DATA_DIR, "recall-state.json");
const DEBOUNCE_MS = 30_000;

const QUESTION_WORDS =
  /\b(who|what|when|where|why|how|which|tell|explain|describe|summarize|summarise|list|find|show|get|any|does|did|is|are|was|were|have|has|do|can|could|should|would|will)\b/i;
const TOOL_COMMANDS =
  /^\s*(run|build|test|lint|format|deploy|install|uninstall|start|stop|restart|commit|push|pull|merge|rebase|checkout|fetch|init|fix|refactor|rename|move|delete|remove|add|create|update|upgrade|downgrade|migrate|generate|scaffold|compile|bundle|watch|serve|debug|profile|bench|clean|reset|undo|redo|revert|squash|cherry-pick|tag|release|publish|npm|npx|yarn|pnpm|bun|pip|cargo|go|make|docker|kubectl|terraform|git|gh|cd|ls|cat|grep|find|mkdir|rm|cp|mv|touch|echo|export|source|chmod|chown|curl|wget|ssh|scp)\b/i;
const FILE_REF =
  /(?:[\w./\\-]+\.\w{1,10}|src\/|lib\/|dist\/|node_modules\/|\.\/|\.\.\/)/;

function isCodeHeavy(text: string): boolean {
  const alpha = text.replace(/[^a-zA-Z]/g, "").length;
  return alpha < text.length * 0.5;
}

export interface RecallDecision {
  shouldRecall: boolean;
  reason: string;
}

interface RecallState {
  lastRecallAt: number;
}

function readState(): RecallState {
  try {
    return JSON.parse(readFileSync(STATE_FILE, "utf-8")) as RecallState;
  } catch {
    return { lastRecallAt: 0 };
  }
}

function writeState(state: RecallState): void {
  try {
    mkdirSync(DATA_DIR, { recursive: true });
    writeFileSync(STATE_FILE, JSON.stringify(state), "utf-8");
  } catch {
    /* best-effort */
  }
}

export function recordRecall(): void {
  writeState({ lastRecallAt: Date.now() });
}

export function shouldRecall(
  prompt: string,
  isFirstPrompt: boolean,
): RecallDecision {
  const trimmed = prompt.trim();
  if (isFirstPrompt) return { shouldRecall: true, reason: "first-prompt" };
  if (trimmed.startsWith("!"))
    return { shouldRecall: false, reason: "opt-out" };
  if (trimmed.length < 15)
    return { shouldRecall: false, reason: "too-short" };

  const hasQuestion = QUESTION_WORDS.test(trimmed);

  if (!hasQuestion) {
    const state = readState();
    if (
      state.lastRecallAt > 0 &&
      Date.now() - state.lastRecallAt < DEBOUNCE_MS
    ) {
      return { shouldRecall: false, reason: "debounce" };
    }
  }

  if (TOOL_COMMANDS.test(trimmed) && !hasQuestion)
    return { shouldRecall: false, reason: "tool-command" };
  if (isCodeHeavy(trimmed) && FILE_REF.test(trimmed) && !hasQuestion)
    return { shouldRecall: false, reason: "code-prompt" };
  if (hasQuestion) return { shouldRecall: true, reason: "question" };
  return { shouldRecall: true, reason: "default" };
}
