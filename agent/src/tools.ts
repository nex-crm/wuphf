// The chat agent's create_tool tool, on the pi-mono stack. The operator teaches a
// workflow in the app's chat; the agent turns it into a callable Tool by calling
// create_tool. This is the ONLY way tools are made — there is no build-a-tool UI,
// and a human never runs a tool (they are agent tools).
//
// S0 authors deterministically (a keyword->shape port shared with the FE mock and
// the executor's expectations), so /tools/build is real end to end WITHOUT a model
// call. The pi-model path (the agent writing real `code` from the description) is
// the next slice on this same stack — mirrors buildAgent.ts, where the narrow task
// runs deterministically first and the pi-ai loop lands later.

import type { Tool, ToolBuildResult, ToolInput } from "./wire.js";

interface Shape {
	test: RegExp;
	name: string;
	title: string;
	purpose: string;
	inputs: string[];
	code: string;
}

// Keyword -> tool shape (first match wins). Kept in sync with the FE
// web/src/operator/tools/mockTools.ts SHAPES so a taught workflow yields the same
// recognizable tool everywhere.
const SHAPES: readonly Shape[] = [
	{
		test: /\b(score|fit|route|lead|assign)\b/i,
		name: "scoreAndRouteLead",
		title: "Score & route a lead",
		purpose: "Score a lead's fit and route hot ones to the right AE.",
		inputs: ["lead"],
		code: [
			"async function scoreAndRouteLead(lead) {",
			"  const fit = await nex.ai.score(lead, { rubric: 'ICP fit' });",
			"  if (fit >= 75) {",
			"    const ae = await crm.ownerFor(lead);",
			"    await crm.assign(lead, ae);",
			"    return `Fit ${fit} -> routed to ${ae.name}`;",
			"  }",
			"  return `Fit ${fit} -> left in the queue`;",
			"}",
		].join("\n"),
	},
	{
		test: /\b(summary|summar|pipeline|digest|weekly|report|recap)\b/i,
		name: "weeklyPipelineSummary",
		title: "Weekly pipeline summary",
		purpose: "Summarize last week's pipeline movement into a glanceable recap.",
		inputs: [],
		code: [
			"async function weeklyPipelineSummary() {",
			"  const deals = await crm.deals({ since: '7d' });",
			"  const moved = deals.filter((d) => d.stageChanged);",
			"  return nex.ai.summarize(moved, { style: 'exec recap' });",
			"}",
		].join("\n"),
	},
	{
		test: /\b(draft|follow.?up|email|reply|outreach|nudge|stall)\b/i,
		name: "draftFollowup",
		title: "Draft a follow-up email",
		purpose: "Draft a follow-up email for a stalled deal in the rep's voice.",
		inputs: ["deal"],
		code: [
			"async function draftFollowup(deal) {",
			"  const ctx = await crm.dealContext(deal);",
			"  return nex.ai.write('follow-up email', { context: ctx, tone: 'warm, brief' });",
			"}",
		].join("\n"),
	},
];

const STOPWORDS = new Set([
	"the", "a", "an", "my", "our", "when", "then", "and", "to", "for", "of", "on",
	"in", "with", "that", "this", "it", "new", "every", "each", "from", "into",
	"by", "at", "is", "are", "do", "i", "we", "want", "need", "should", "please",
	"can", "you",
]);

function toInputs(names: string[]): ToolInput[] {
	return names.map((name) => ({ name, type: "string" }));
}

function camel(words: string[]): string {
	return words.map((w, i) => (i === 0 ? w : w[0].toUpperCase() + w.slice(1))).join("");
}

/** Derive a create_tool spec from a described workflow — a known shape, else a
 * synthesized camelCase name + plain-language title. Deterministic. */
export function authorTool(description: string): Tool {
	const desc = description.trim();
	const shape = SHAPES.find((s) => s.test.test(desc));
	if (shape) {
		return { name: shape.name, title: shape.title, purpose: shape.purpose, inputs: toInputs(shape.inputs), code: shape.code };
	}
	const words = desc
		.toLowerCase()
		.replace(/[^a-z0-9\s]/g, " ")
		.split(/\s+/)
		.filter((w) => w && !STOPWORDS.has(w));
	const name = words.length ? camel(words.slice(0, 3)) : "runWorkflow";
	// Human title: drop a leading "When ... ," trigger, sentence-case the rest.
	const lead = desc.replace(/^when\b[^,]*,\s*/i, "");
	const titleWords = lead.split(/\s+/).slice(0, 6).join(" ");
	const title = (titleWords ? titleWords[0].toUpperCase() + titleWords.slice(1) : name).replace(/[.,;:]+$/, "");
	return {
		name,
		title,
		purpose: desc ? desc[0].toUpperCase() + desc.slice(1) : name,
		inputs: [{ name: "input", type: "string" }],
		code: `async function ${name}(input) {\n  // Nex scripted this from: "${desc}"\n  return nex.run(input);\n}`,
	};
}

/** The tool agent's turn: teach a workflow -> create_tool -> the tool it made. */
export function buildTool(message: string): ToolBuildResult {
	const tool = authorTool(message);
	return { tool, narration: `Built ${tool.title}.` };
}
