import { expect, test } from "bun:test";
import { runWorkflow } from "./executor.js";
import { type Har, sniff, sniffToSteps } from "./sniff.js";

const SECRET = "sk-live-SUPERSECRET-must-not-leak";

const sampleHar: Har = {
	log: {
		entries: [
			{
				request: {
					method: "GET",
					url: "https://api.acme.com/v1/customers?id=42&access_token=" + SECRET,
					headers: [
						{ name: "x-api-key", value: SECRET },
						{ name: "content-type", value: "application/json" },
						{ name: "user-agent", value: "Mozilla/5.0 fingerprint" },
					],
					queryString: [
						{ name: "id", value: "42" },
						{ name: "access_token", value: SECRET },
					],
				},
				response: { status: 200 },
			},
			{
				request: {
					method: "POST",
					url: "https://hooks.slack.com/services/T/B/X",
					headers: [{ name: "authorization", value: "Bearer aaa.bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb.ccc" }],
					postData: { text: '{"text":"hi"}', mimeType: "application/json" },
				},
				response: { status: 200 },
			},
		],
	},
};

test("sniff strips secrets and turns auth into a named ref (A3)", () => {
	const calls = sniff(sampleHar);
	const blob = JSON.stringify(calls);
	expect(blob).not.toContain(SECRET); // no secret value anywhere
	expect(calls[0].call.auth_ref).toBe("api-acme-com_x-api-key");
	expect(calls[0].call.headers?.["x-api-key"]).toBeUndefined(); // auth header dropped
	expect(calls[0].call.query?.access_token).toBe("<redacted>"); // secret query redacted
	expect(calls[0].call.query?.id).toBe("42"); // benign query kept
});

test("sniff redacts secret-looking request body fields (A3, regression: CodeRabbit sniff.ts:103)", () => {
	const bodySecret = "hunter2-pw-must-not-leak";
	const refresh = "rt-MUST-NOT-LEAK";
	const har: Har = {
		log: {
			entries: [
				{
					request: {
						method: "POST",
						url: "https://api.acme.com/v1/login",
						postData: {
							mimeType: "application/json",
							text: JSON.stringify({ email: "sam@acme.com", password: bodySecret, nested: { refresh_token: refresh } }),
						},
					},
					response: { status: 200 },
				},
				{
					request: {
						method: "POST",
						url: "https://api.acme.com/v1/form",
						postData: { mimeType: "application/x-www-form-urlencoded", text: `password=${bodySecret}&ok=1` },
					},
					response: { status: 200 },
				},
			],
		},
	};
	const calls = sniff(har);
	const blob = JSON.stringify(calls);
	expect(blob).not.toContain(bodySecret); // no body secret anywhere
	expect(blob).not.toContain(refresh); // nested secret redacted too
	const json = calls[0].call.body as Record<string, unknown>;
	expect(json.password).toBe("<redacted>");
	expect(json.email).toBe("sam@acme.com"); // benign field kept
	expect((json.nested as Record<string, unknown>).refresh_token).toBe("<redacted>");
	expect(calls[1].call.body).toBe("<redacted>"); // non-JSON body dropped wholesale
});

test("sniff redacts keyless string secrets in bodies (A3, regression: CodeRabbit sniff.ts redactBody)", () => {
	// redactBody matches object fields by key; a keyless secret (a bare top-level
	// string, or a string inside an array) has no key to match, so it must still
	// be redacted wholesale. Non-string primitives are kept (not credentials).
	const token = "sk-live-KEYLESS-must-not-leak";
	const refresh = "rt-array-MUST-NOT-LEAK";
	const har: Har = {
		log: {
			entries: [
				{ request: { method: "POST", url: "https://api.acme.com/a", postData: { mimeType: "application/json", text: JSON.stringify(token) } }, response: { status: 200 } },
				{ request: { method: "POST", url: "https://api.acme.com/b", postData: { mimeType: "application/json", text: JSON.stringify([refresh, 42]) } }, response: { status: 200 } },
			],
		},
	};
	const calls = sniff(har);
	const blob = JSON.stringify(calls);
	expect(blob).not.toContain(token);
	expect(blob).not.toContain(refresh);
	expect(calls[0].call.body).toBe("<redacted>"); // bare top-level string redacted
	expect(calls[1].call.body).toEqual(["<redacted>", 42]); // array string redacted, number kept
});

test("sniff gives distinct auth_refs per mechanism on the same host (regression: CodeRabbit sniff.ts:99)", () => {
	// Two calls to the same host, one authed by api-key and one by cookie, must not
	// collapse into one auth_ref or the executor can't tell the credentials apart.
	const har: Har = {
		log: {
			entries: [
				{ request: { method: "GET", url: "https://api.acme.com/v1/a", headers: [{ name: "x-api-key", value: "k-" + SECRET }] }, response: { status: 200 } },
				{ request: { method: "GET", url: "https://api.acme.com/v1/b", headers: [{ name: "cookie", value: "session=" + SECRET }] }, response: { status: 200 } },
			],
		},
	};
	const calls = sniff(har);
	expect(calls[0].call.auth_ref).toBe("api-acme-com_x-api-key");
	expect(calls[1].call.auth_ref).toBe("api-acme-com_cookie");
	expect(calls[0].call.auth_ref).not.toBe(calls[1].call.auth_ref);
	expect(JSON.stringify(calls)).not.toContain(SECRET); // names only, never values
});

test("sniff classifies auth: api_key stable vs JWT rotating (A4)", () => {
	const calls = sniff(sampleHar);
	expect(calls[0].auth_kind).toBe("api_key");
	expect(calls[0].rotating).toBe(false); // stable key -> storable
	expect(calls[1].auth_kind).toBe("bearer");
	expect(calls[1].rotating).toBe(true); // JWT -> needs a live session
});

test("sniffToSteps gates mutations and carries the api def", () => {
	const steps = sniffToSteps(sampleHar);
	expect(steps[0].kind).toBe("enrich");
	expect(steps[0].gated).toBeFalsy(); // GET
	expect(steps[1].kind).toBe("action");
	expect(steps[1].gated).toBe(true); // POST -> gated (CQ1)
	expect(steps[1].api?.method).toBe("POST");
});

test("discovery -> execute spine: sniffed steps replay through the executor", async () => {
	const urls: string[] = [];
	const fetchImpl = (async (url: string) => {
		urls.push(String(url));
		return { ok: true, status: 200 } as Response;
	}) as unknown as typeof fetch;

	const steps = sniffToSteps(sampleHar);
	const spec = { name: "captured", tool_id: "acme", narration: "", clarify: null, steps };
	// Approve the gated POST so the run proceeds; both captured calls replay for real.
	const r = await runWorkflow(spec, { approved: ["sniff-1"] }, { fetchImpl, resolveCredential: () => ({ Authorization: "Bearer FROM_STORE" }) });
	expect(r.status).toBe("done");
	expect(r.steps.every((s) => s.http_status === 200)).toBe(true);
	expect(urls.some((u) => u.includes("api.acme.com/v1/customers"))).toBe(true);
	expect(urls.some((u) => u.includes("hooks.slack.com"))).toBe(true);
});
