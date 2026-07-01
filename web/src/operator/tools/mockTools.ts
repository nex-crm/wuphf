// Mock model for the "workflows as tools" spike (slice 1, FE-first). A Tool is an
// AI-authored, workflow-specific scripted capability the agent calls. Here the
// authoring and calling are DETERMINISTIC MOCKS so the founder can react to the
// shape before any backend writes real code. See
// docs/specs/operator-workflows-as-tools.md.

export interface ToolInput {
  name: string;
  type: "string" | "number" | "record";
}

export interface ToolCall {
  id: string;
  args: Record<string, string>;
  result: string;
  at: string;
}

export interface Tool {
  id: string;
  /** Callable identifier the agent invokes, e.g. scoreAndRouteLead. */
  name: string;
  /** One line: what running this tool does. */
  purpose: string;
  inputs: ToolInput[];
  /** The (mock) script Nex wrote for this workflow. */
  script: string;
  /** The operator's own words that Nex turned into this tool. */
  createdFrom: string;
  calls: ToolCall[];
}

// A tiny keyword → shape table so a described workflow yields a plausible,
// recognizable tool instead of a generic stub. Order matters: first match wins.
const SHAPES: ReadonlyArray<{
  test: RegExp;
  name: string;
  purpose: string;
  inputs: ToolInput[];
  body: string;
  sampleArg: Record<string, string>;
  sampleResult: string;
}> = [
  {
    test: /\b(score|fit|route|lead|assign)\b/i,
    name: "scoreAndRouteLead",
    purpose: "Score a lead's fit and route hot ones to the right AE.",
    inputs: [{ name: "lead", type: "string" }],
    body: [
      "const fit = await nex.ai.score(lead, { rubric: 'ICP fit' });",
      "if (fit >= 75) {",
      "  const ae = await crm.ownerFor(lead);",
      "  await crm.assign(lead, ae);",
      "  return `Fit ${fit} → routed to ${ae.name}`;",
      "}",
      "return `Fit ${fit} → left in the queue`;",
    ].join("\n"),
    sampleArg: { lead: "Acme" },
    sampleResult: "Fit 82 → routed to Priya (AE)",
  },
  {
    test: /\b(summary|summar|pipeline|digest|weekly|report|recap)\b/i,
    name: "weeklyPipelineSummary",
    purpose: "Summarize last week's pipeline movement into a glanceable recap.",
    inputs: [],
    body: [
      "const deals = await crm.deals({ since: '7d' });",
      "const moved = deals.filter((d) => d.stageChanged);",
      "return nex.ai.summarize(moved, { style: 'exec recap' });",
    ].join("\n"),
    sampleArg: {},
    sampleResult:
      "6 deals moved · $420k created · 2 slipped. Biggest: Globex → Negotiation.",
  },
  {
    test: /\b(draft|follow.?up|email|reply|outreach|nudge|stall)\b/i,
    name: "draftFollowup",
    purpose: "Draft a follow-up email for a stalled deal in the rep's voice.",
    inputs: [{ name: "deal", type: "string" }],
    body: [
      "const ctx = await crm.dealContext(deal);",
      "return nex.ai.write('follow-up email', {",
      "  context: ctx,",
      "  tone: 'warm, brief',",
      "});",
    ].join("\n"),
    sampleArg: { deal: "Globex" },
    sampleResult:
      "Subject: Quick nudge on Globex — drafted a 3-line check-in ready to send.",
  },
];

let seq = 0;
function nextId(prefix: string): string {
  seq += 1;
  return `${prefix}_${seq.toString(36)}`;
}

// Derive a camelCase tool name from a free-text description when no shape matches,
// so an unknown workflow still yields a sensible callable name.
function deriveName(description: string): string {
  const words = description
    .toLowerCase()
    .replace(/[^a-z0-9\s]/g, " ")
    .split(/\s+/)
    .filter((w) => w && !STOPWORDS.has(w))
    .slice(0, 3);
  if (words.length === 0) return "runWorkflow";
  return words
    .map((w, i) => (i === 0 ? w : w[0].toUpperCase() + w.slice(1)))
    .join("");
}

const STOPWORDS = new Set([
  "the",
  "a",
  "an",
  "my",
  "our",
  "when",
  "then",
  "and",
  "to",
  "for",
  "of",
  "on",
  "in",
  "with",
  "that",
  "this",
  "it",
  "new",
  "every",
  "each",
  "from",
  "into",
  "by",
  "at",
  "is",
  "are",
  "do",
  "i",
  "we",
  "want",
  "need",
  "should",
  "please",
  "can",
  "you",
]);

/**
 * Nex "writes a tool" for a described workflow — a deterministic mock. Matches a
 * known shape when it can (recognizable script + inputs), else synthesizes a
 * named stub from the operator's words.
 */
export function authorToolFromDescription(description: string): Tool {
  const desc = description.trim();
  const shape = SHAPES.find((s) => s.test.test(desc));
  if (shape) {
    return {
      id: nextId("tool"),
      name: shape.name,
      purpose: shape.purpose,
      inputs: shape.inputs,
      script: `async function ${shape.name}(${shape.inputs
        .map((i) => i.name)
        .join(", ")}) {\n  ${shape.body.replace(/\n/g, "\n  ")}\n}`,
      createdFrom: desc,
      calls: [],
    };
  }
  const name = deriveName(desc);
  return {
    id: nextId("tool"),
    name,
    purpose: desc.charAt(0).toUpperCase() + desc.slice(1),
    inputs: [{ name: "input", type: "string" }],
    script: `async function ${name}(input) {\n  // Nex scripted this from: "${desc}"\n  const result = await nex.run(input);\n  return result;\n}`,
    createdFrom: desc,
    calls: [],
  };
}

/**
 * The agent "calls" a tool — a deterministic mock result. Uses the known shape's
 * sample result when available, else echoes the args.
 */
export function callTool(tool: Tool, args: Record<string, string>): ToolCall {
  const shape = SHAPES.find((s) => s.name === tool.name);
  const result = shape
    ? shape.sampleResult
    : `${tool.name}(${Object.values(args).join(", ") || "…"}) → done`;
  return { id: nextId("call"), args, result, at: "just now" };
}

/** Suggested args for a one-click "call it" in the mock, from the known shape. */
export function sampleArgsFor(tool: Tool): Record<string, string> {
  return SHAPES.find((s) => s.name === tool.name)?.sampleArg ?? {};
}

/** Starter workflows the operator can click, mapping to the three ICP examples. */
export const TOOL_STARTERS: readonly string[] = [
  "When a new lead comes in, score its fit and route hot ones to the right AE",
  "Every Monday, summarize last week's pipeline",
  "Draft a follow-up email for a stalled deal",
];

/** A pre-seeded Tool so the library is not empty on first open. */
export function seedTools(): Tool[] {
  const t = authorToolFromDescription(
    "Every Monday, summarize last week's pipeline",
  );
  return [t];
}
