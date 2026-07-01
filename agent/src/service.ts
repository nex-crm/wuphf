// Thin HTTP/SSE service the operator FE talks to (no broker). Bun.serve, no
// framework:
//   GET  /health        liveness
//   GET  /providers     which inference paths are available (subscription/BYOK/local)
//   POST /build/stream  description -> the pi-mono agent assembles a WorkflowSpec (SSE)
//   POST /tools/build   teach a workflow -> the agent authors a tool (persisted per agent when `app` is set)
//   POST /tools/call    the app's chat calls a saved tool (sandboxed; gated -> approval)
//   POST /run           execute a compiled spec deterministically (gated step -> CQ1)
//   GET  /tools?agent=              the agent's persisted tools
//   GET  /routines?agent=           the agent's routines
//   POST /routines                  create a routine (+ its chat session)
//   PATCH /routines/<id>            enable/disable, edit prompt (draft), publish vN+1
//   POST /routines/<id>/run         run NOW regardless of schedule (approved: false)
//   GET  /sessions?agent=           session list; GET /sessions/<id>?agent= transcript
//   POST /sessions                  new manual session; POST /sessions/<id>/message append
//   GET  /artifacts?agent=          the agent's saved run artifacts

import { streamWorkflow } from "./buildAgent.js";
import { buildCapabilities, capabilityConfigFromEnv } from "./capabilities.js";
import { runWorkflow } from "./executor.js";
import { providersPayload } from "./providers.js";
import { runRoutine } from "./routineRunner.js";
import { startScheduler } from "./scheduler.js";
import { AgentStore, sanitizeAgentId } from "./store.js";
import { runTool } from "./toolRuntime.js";
import { buildTool } from "./tools.js";
import { type BuildRequest, type Routine, type RunRequest, SCHEMA_VERSION, type ToolBuildRequest, type ToolCallRequest, type WorkflowSpec } from "./wire.js";

type BuildEvent = { type: "step"; step: WorkflowSpec["steps"][number] } | { type: "spec"; spec: WorkflowSpec };
type BuildStream = (message: string, opts: { toolId?: string; signal?: AbortSignal }) => AsyncGenerator<BuildEvent>;

export interface ServerOptions {
	port?: number;
	// Override the build engine in tests so they never hit a live model.
	buildStream?: BuildStream;
	// Override the per-agent store (tests point it at a tmp dir).
	store?: AgentStore;
}

function json(data: unknown, status = 200): Response {
	return new Response(JSON.stringify(data), { status, headers: { "content-type": "application/json" } });
}

function sse(name: string, data: unknown): string {
	return `event: ${name}\ndata: ${JSON.stringify(data)}\n\n`;
}

function schemaMismatch(v: number | undefined): boolean {
	return v != null && v !== SCHEMA_VERSION;
}

/** True when this string is a usable agent id (sanitizes to a safe filename). */
function validAgentId(agent: string): boolean {
	try {
		sanitizeAgentId(agent);
		return true;
	} catch {
		return false;
	}
}

/** ?agent=<id> for the GET routes; null when missing or unusable. */
function agentParam(url: URL): string | null {
	const a = url.searchParams.get("agent")?.trim();
	return a && validAgentId(a) ? a : null;
}

/** The shared validation-ladder head for the persistence POST/PATCH routes:
 * JSON parse guard -> shape guard (object + agent string) -> schema_version
 * guard. Route-specific field guards follow at each call site. */
async function parseAgentBody(req: Request): Promise<{ body: Record<string, unknown>; agent: string } | { error: Response }> {
	let raw: unknown;
	try {
		raw = await req.json();
	} catch {
		return { error: json({ error: "invalid JSON body" }, 400) };
	}
	if (!raw || typeof raw !== "object" || Array.isArray(raw)) {
		return { error: json({ error: "invalid request body" }, 400) };
	}
	const body = raw as Record<string, unknown>;
	const agent = typeof body.agent === "string" ? body.agent.trim() : "";
	if (!agent || !validAgentId(agent)) {
		return { error: json({ error: "invalid request: agent (string) required" }, 400) };
	}
	if (body.schema_version != null && body.schema_version !== SCHEMA_VERSION) {
		return { error: json({ error: "schema_version mismatch" }, 400) };
	}
	return { body, agent };
}

export function createServer(opts: ServerOptions = {}) {
	const buildStream: BuildStream = opts.buildStream ?? (streamWorkflow as BuildStream);
	const store = opts.store ?? new AgentStore();
	// The routine scheduler is OFF unless ROUTINE_SCHEDULER=1 so tests and plain
	// dev never spin an interval; the timer is also unref'd (scheduler.ts), so it
	// can never pin a process open on its own.
	if (process.env.ROUTINE_SCHEDULER === "1") startScheduler({ store });

	return Bun.serve({
		port: opts.port ?? Number(process.env.PORT ?? 8820),
		async fetch(req) {
			const url = new URL(req.url);
			const { pathname } = url;

			if (req.method === "GET" && pathname === "/health") return json({ status: "ok", version: "0.0.1" });
			if (req.method === "GET" && pathname === "/providers") return json(providersPayload());

			if (req.method === "POST" && pathname === "/build/stream") {
				// Guard the parse BEFORE the stream starts: an unparseable body must be a
				// plain 400, not a 500 (and never a half-open SSE response).
				let body: BuildRequest;
				try {
					body = (await req.json()) as BuildRequest;
				} catch {
					return json({ error: "invalid JSON body" }, 400);
				}
				// Parseable JSON is not enough: null/[]/"x" cast to BuildRequest then read
				// as undefined. Validate the shape before dereferencing.
				if (!body || typeof body !== "object" || typeof body.message !== "string") {
					return json({ error: "invalid build request: message (string) required" }, 400);
				}
				if (schemaMismatch(body.schema_version)) return json({ error: "schema_version mismatch" }, 400);
				const stream = new ReadableStream({
					async start(controller) {
						const enc = new TextEncoder();
						controller.enqueue(enc.encode(sse("start", { message: body.message })));
						try {
							// Thread the request's abort signal so a dropped client tears the build down.
							for await (const ev of buildStream(body.message, { toolId: body.tool_id, signal: req.signal })) {
								controller.enqueue(enc.encode(sse(ev.type === "spec" ? "spec" : "step", ev)));
							}
						} catch (e) {
							controller.enqueue(enc.encode(sse("error", { message: String(e) })));
						}
						controller.close();
					},
				});
				return new Response(stream, { headers: { "content-type": "text/event-stream", "cache-control": "no-cache" } });
			}

			if (req.method === "POST" && pathname === "/tools/build") {
				// The app's chat teaches a workflow; the agent calls create_tool and
				// returns the tool it made, so the FE renders the call and lists it.
				let body: ToolBuildRequest;
				try {
					body = (await req.json()) as ToolBuildRequest;
				} catch {
					return json({ error: "invalid JSON body" }, 400);
				}
				if (!body || typeof body !== "object" || typeof body.message !== "string") {
					return json({ error: "invalid tool build request: message (string) required" }, 400);
				}
				if (schemaMismatch(body.schema_version)) return json({ error: "schema_version mismatch" }, 400);
				// Model authoring is opt-in (TOOL_AUTHOR_MODEL=1): with no model configured
				// the endpoint must answer deterministically FAST, not eat a model timeout.
				const app = typeof body.app === "string" ? body.app.trim() : "";
				if (app && !validAgentId(app)) return json({ error: "invalid tool build request: unusable app id" }, 400);
				const outcome = await buildTool(body.message, { tryModel: process.env.TOOL_AUTHOR_MODEL === "1", signal: req.signal });
				// `app` set -> PERSIST the authored tool under that agent (re-authoring a
				// same-named tool bumps version); the response tool carries `version`.
				if (app && outcome.tool) return json({ ...outcome, tool: store.upsertTool(app, outcome.tool) });
				return json(outcome);
			}

			if (req.method === "GET" && pathname === "/tools") {
				const agent = agentParam(url);
				if (!agent) return json({ error: "agent query param required" }, 400);
				return json({ tools: store.listTools(agent) });
			}

			if (req.method === "POST" && pathname === "/tools/call") {
				// The app's chat CALLS a saved tool. Execution is sandboxed
				// (toolRuntime.ts); a gated capability halts with needs_approval
				// unless the request carries approved=true (CQ1, default deny).
				let body: ToolCallRequest;
				try {
					body = (await req.json()) as ToolCallRequest;
				} catch {
					return json({ error: "invalid JSON body" }, 400);
				}
				const tool = body && typeof body === "object" ? body.tool : undefined;
				if (!tool || typeof tool !== "object" || typeof tool.name !== "string" || typeof tool.code !== "string") {
					return json({ error: "invalid tool call request: tool { name, code } required" }, 400);
				}
				// inputs is caller-supplied JSON: absent -> []; present but not an array
				// of { name: string } entries -> a plain 400, not a 500 inside runTool.
				const rawInputs = (tool as { inputs?: unknown }).inputs;
				const inputsValid =
					rawInputs === undefined ||
					(Array.isArray(rawInputs) &&
						rawInputs.every((i) => i !== null && typeof i === "object" && typeof (i as { name?: unknown }).name === "string"));
				if (!inputsValid) {
					return json({ error: "invalid tool call request: inputs must be an array of { name } entries" }, 400);
				}
				if (schemaMismatch(body.schema_version)) return json({ error: "schema_version mismatch" }, 400);
				// Capabilities compose per host env: real broker/model seams when
				// configured (WUPHF_BROKER_URL/TOKEN, TOOL_RUNTIME_MODEL=1), simulated
				// otherwise. TOOL_CALL_TIMEOUT_MS raises the hard-kill deadline for
				// hosts running slow capabilities (e.g. nex.browser drives real Chrome).
				// Same positive-finite guard as capabilityConfigFromEnv, so the worker
				// deadline and the capability call bound never diverge on a bad value.
				const rawTimeoutMs = Number(process.env.TOOL_CALL_TIMEOUT_MS);
				const timeoutMs = Number.isFinite(rawTimeoutMs) && rawTimeoutMs > 0 ? rawTimeoutMs : undefined;
				return json(
					await runTool({ ...tool, inputs: rawInputs === undefined ? [] : tool.inputs }, body.args ?? {}, {
						approved: body.approved === true,
						capabilities: buildCapabilities(capabilityConfigFromEnv()),
						timeoutMs,
					}),
				);
			}

			if (req.method === "POST" && pathname === "/run") {
				let body: RunRequest;
				try {
					body = (await req.json()) as RunRequest;
				} catch {
					return json({ error: "invalid JSON body" }, 400);
				}
				if (!body || typeof body !== "object" || !body.spec || typeof body.spec !== "object") {
					return json({ error: "invalid run request: spec (object) required" }, 400);
				}
				if (schemaMismatch(body.schema_version)) return json({ error: "schema_version mismatch" }, 400);
				return json(await runWorkflow(body.spec, body.input ?? {}));
			}

			// ----- Routines (persisted; see routineRunner.ts / scheduler.ts) -----

			if (req.method === "GET" && pathname === "/routines") {
				const agent = agentParam(url);
				if (!agent) return json({ error: "agent query param required" }, 400);
				return json({ routines: store.listRoutines(agent) });
			}

			if (req.method === "POST" && pathname === "/routines") {
				const parsed = await parseAgentBody(req);
				if ("error" in parsed) return parsed.error;
				const { body, agent } = parsed;
				if (
					typeof body.name !== "string" ||
					!body.name.trim() ||
					typeof body.prompt !== "string" ||
					!body.prompt.trim() ||
					typeof body.schedule !== "string" ||
					!body.schedule.trim()
				) {
					return json({ error: "invalid routine request: name, prompt, schedule (strings) required" }, 400);
				}
				// Creating a routine mints its chat session too (kind "routine", title = name).
				const { routine } = store.createRoutine(agent, body.name.trim(), body.prompt, body.schedule.trim());
				return json({ routine });
			}

			const routinePatch = req.method === "PATCH" ? /^\/routines\/([^/]+)$/.exec(pathname) : null;
			if (routinePatch) {
				const parsed = await parseAgentBody(req);
				if ("error" in parsed) return parsed.error;
				const { body, agent } = parsed;
				if (body.enabled !== undefined && typeof body.enabled !== "boolean") {
					return json({ error: "invalid routine patch: enabled must be a boolean" }, 400);
				}
				if (body.prompt !== undefined && typeof body.prompt !== "string") {
					return json({ error: "invalid routine patch: prompt must be a string" }, 400);
				}
				if (body.publish !== undefined && typeof body.publish !== "boolean") {
					return json({ error: "invalid routine patch: publish must be a boolean" }, 400);
				}
				const routine = store.updateRoutine(agent, routinePatch[1], (r) => {
					let next: Routine = { ...r };
					if (typeof body.enabled === "boolean") next = { ...next, enabled: body.enabled };
					// A prompt edit is a DRAFT until published.
					if (typeof body.prompt === "string") next = { ...next, prompt: body.prompt, draft: true };
					// Publish freezes the current prompt as vN+1 and clears the draft flag.
					if (body.publish === true) {
						const { draft: _cleared, ...rest } = next;
						next = { ...rest, version: next.version + 1 };
					}
					return next;
				});
				if (!routine) return json({ error: "routine not found" }, 404);
				return json({ routine });
			}

			const routineRun = req.method === "POST" ? /^\/routines\/([^/]+)\/run$/.exec(pathname) : null;
			if (routineRun) {
				const parsed = await parseAgentBody(req);
				if ("error" in parsed) return parsed.error;
				// Run NOW regardless of schedule. runRoutine executes with approved: false
				// (SEND-GATE, default deny): a gated routine records needs_approval into
				// its transcript — it never auto-sends.
				const result = await runRoutine(parsed.agent, routineRun[1], { store });
				if (!result) return json({ error: "routine not found" }, 404);
				return json({ routine: result.routine, session: result.session });
			}

			// ----- Chat sessions (transcripts persist here; chat logic stays FE-side) -----

			if (req.method === "GET" && pathname === "/sessions") {
				const agent = agentParam(url);
				if (!agent) return json({ error: "agent query param required" }, 400);
				return json({ sessions: store.listSessions(agent) });
			}

			const sessionGet = req.method === "GET" ? /^\/sessions\/([^/]+)$/.exec(pathname) : null;
			if (sessionGet) {
				const agent = agentParam(url);
				if (!agent) return json({ error: "agent query param required" }, 400);
				const found = store.getSession(agent, sessionGet[1]);
				if (!found) return json({ error: "session not found" }, 404);
				return json({ session: found.session, messages: found.messages });
			}

			if (req.method === "POST" && pathname === "/sessions") {
				const parsed = await parseAgentBody(req);
				if ("error" in parsed) return parsed.error;
				const { body, agent } = parsed;
				if (body.title !== undefined && typeof body.title !== "string") {
					return json({ error: "invalid session request: title must be a string" }, 400);
				}
				// kind "manual"; default title "Chat <n>" (store.createSession).
				return json({ session: store.createSession(agent, typeof body.title === "string" ? body.title : undefined) });
			}

			const sessionMsg = req.method === "POST" ? /^\/sessions\/([^/]+)\/message$/.exec(pathname) : null;
			if (sessionMsg) {
				const parsed = await parseAgentBody(req);
				if ("error" in parsed) return parsed.error;
				const { body, agent } = parsed;
				if ((body.from !== "you" && body.from !== "nex") || typeof body.body !== "string") {
					return json({ error: 'invalid message: from ("you"|"nex") and body (string) required' }, 400);
				}
				const appended = store.appendMessage(agent, sessionMsg[1], { from: body.from, body: body.body, at: new Date().toISOString() });
				if (!appended) return json({ error: "session not found" }, 404);
				return json({ ok: true });
			}

			// ----- Artifacts (routine runs always save their outcome here) -----

			if (req.method === "GET" && pathname === "/artifacts") {
				const agent = agentParam(url);
				if (!agent) return json({ error: "agent query param required" }, 400);
				return json({ artifacts: store.listArtifacts(agent) });
			}

			return json({ error: "not found" }, 404);
		},
	});
}

if (import.meta.main) {
	const server = createServer();
	console.log(`wuphf-agent service on ${server.url}`);
}
