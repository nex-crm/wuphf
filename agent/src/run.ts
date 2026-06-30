// Live runner: compile a description into a WorkflowSpec via the pi-mono engine.
//   bun run src/run.ts "route inbound demo requests over $5k to slack"
import { buildWorkflow } from "./buildAgent.js";

const msg =
	process.argv.slice(2).join(" ") ||
	"When a demo request comes in, score the lead; if it is over $5000 of expected value route it to the sales-priority Slack channel, otherwise add it to the nurture list.";
const spec = await buildWorkflow(msg);
console.log(JSON.stringify(spec, null, 2));
