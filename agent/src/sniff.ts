// browsersniff — HAR (captured browser traffic) -> deterministic ApiCalls and
// executable WorkflowSteps. The DISCOVERY half of the spine: the operator's real
// session is captured, sniffed into an API spec, and the executor replays it.
//
// Security (operator-mlp A3/A4): secrets NEVER land in the spec. Auth headers
// become a NAMED auth_ref the executor resolves at run time; secret-looking query
// params are redacted; the auth is CLASSIFIED (stable key -> storable, rotating
// session -> flagged "needs a live session").

import type { ApiCall, WorkflowStep, WorkflowStepKind } from "./wire.js";

interface HarHeader {
	name: string;
	value: string;
}
interface HarEntry {
	request: {
		method: string;
		url: string;
		headers?: HarHeader[];
		queryString?: HarHeader[];
		postData?: { text?: string; mimeType?: string };
	};
	response?: { status?: number };
}
export interface Har {
	log: { entries: HarEntry[] };
}

export type AuthKind = "api_key" | "bearer" | "session" | "none";

export interface SniffResult {
	call: ApiCall;
	auth_kind: AuthKind;
	rotating: boolean; // true -> a live session is required at run time (A4)
}

const _MUTATING = new Set(["POST", "PUT", "PATCH", "DELETE"]);
const _SECRET_QUERY = /(token|key|apikey|api_key|access[_-]?token|sig|signature|secret|password)/i;
const _AUTH_HEADERS = new Set(["authorization", "cookie", "x-api-key", "api-key", "x-auth-token"]);
const _SAFE_HEADERS = new Set(["content-type", "accept"]);

function host(url: string): string {
	try {
		return new URL(url).host.replace(/[^a-z0-9]+/gi, "-").toLowerCase();
	} catch {
		return "api";
	}
}

/** Classify an auth header into (kind, rotating). Stable keys store; rotating
 * sessions/JWTs are flagged so the operator is told a live session is needed. */
function classifyAuth(name: string, value: string): { kind: AuthKind; rotating: boolean } {
	const n = name.toLowerCase();
	if (n === "cookie") return { kind: "session", rotating: true };
	if (n === "x-api-key" || n === "api-key" || n === "x-auth-token") return { kind: "api_key", rotating: false };
	if (n === "authorization") {
		// A JWT (three dot-separated b64 segments) is a rotating token; an opaque
		// bearer / basic value is treated as a storable key.
		const v = value.replace(/^Bearer\s+/i, "");
		const jwt = v.split(".").length === 3 && v.length > 40;
		return { kind: "bearer", rotating: jwt };
	}
	return { kind: "none", rotating: false };
}

/** Turn a HAR into deterministic, secret-stripped ApiCalls. */
export function sniff(har: Har): SniffResult[] {
	const out: SniffResult[] = [];
	for (const entry of har.log?.entries ?? []) {
		const req = entry.request;
		if (!req?.url) continue;

		let auth_ref: string | undefined;
		let auth_kind: AuthKind = "none";
		let rotating = false;
		const headers: Record<string, string> = {};
		for (const h of req.headers ?? []) {
			const lower = h.name.toLowerCase();
			if (_AUTH_HEADERS.has(lower)) {
				const c = classifyAuth(h.name, h.value);
				auth_ref = `${host(req.url)}_auth`; // a NAMED ref — the value is dropped here
				auth_kind = c.kind;
				rotating = rotating || c.rotating;
			} else if (_SAFE_HEADERS.has(lower)) {
				headers[h.name] = h.value;
			}
			// all other headers are dropped (avoid leaking fingerprints / secrets)
		}

		const query: Record<string, string> = {};
		for (const q of req.queryString ?? []) {
			query[q.name] = _SECRET_QUERY.test(q.name) ? "<redacted>" : q.value;
		}

		let body: unknown;
		if (req.postData?.text) {
			try {
				body = JSON.parse(req.postData.text);
			} catch {
				body = req.postData.text;
			}
		}

		const call: ApiCall = {
			method: (req.method || "GET").toUpperCase(),
			url: req.url.split("?")[0], // query carried separately
			...(Object.keys(query).length ? { query } : {}),
			...(Object.keys(headers).length ? { headers } : {}),
			...(body !== undefined ? { body } : {}),
			...(auth_ref ? { auth_ref } : {}),
		};
		out.push({ call, auth_kind, rotating });
	}
	return out;
}

/** Turn sniffed calls into executable workflow steps (mutations are gated -> CQ1). */
export function sniffToSteps(har: Har): WorkflowStep[] {
	return sniff(har).map((r, i) => {
		const mutating = _MUTATING.has(r.call.method);
		const kind: WorkflowStepKind = mutating ? "action" : "enrich";
		const path = (() => {
			try {
				return new URL(r.call.url).pathname;
			} catch {
				return r.call.url;
			}
		})();
		return {
			id: `sniff-${i}`,
			kind,
			title: `${r.call.method} ${path}`,
			detail: r.rotating ? `Captured call — auth is a rotating ${r.auth_kind}; needs a live session.` : `Captured call (auth: ${r.auth_kind}).`,
			integration: host(r.call.url),
			gated: mutating, // external mutation -> human approval card
			api: r.call,
		};
	});
}
