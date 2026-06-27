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
	expect(calls[0].call.auth_ref).toBe("api-acme-com_auth");
	expect(calls[0].call.headers?.["x-api-key"]).toBeUndefined(); // auth header dropped
	expect(calls[0].call.query?.access_token).toBe("<redacted>"); // secret query redacted
	expect(calls[0].call.query?.id).toBe("42"); // benign query kept
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
