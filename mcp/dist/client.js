import { loadConfig } from "./config.js";
function getBaseUrl() {
    const config = loadConfig();
    const base = process.env.WUPHF_API_BASE_URL || process.env.NEX_API_BASE_URL || config.base_url || config.dev_url || "https://app.nex.ai";
    return `${base.replace(/\/+$/, "")}/api/developers`;
}
function getRegisterUrl() {
    const config = loadConfig();
    const base = process.env.WUPHF_API_BASE_URL || process.env.NEX_API_BASE_URL || config.base_url || config.dev_url || "https://app.nex.ai";
    return `${base.replace(/\/+$/, "")}/api/v1/agents/register`;
}
export class NexApiError extends Error {
    status;
    statusText;
    body;
    constructor(status, statusText, body) {
        super(`WUPHF API error ${status}: ${statusText}`);
        this.status = status;
        this.statusText = statusText;
        this.body = body;
        this.name = "NexApiError";
    }
}
export class NexApiClient {
    apiKey;
    constructor(apiKey) {
        this.apiKey = apiKey;
    }
    get isAuthenticated() {
        return this.apiKey !== undefined && this.apiKey.length > 0;
    }
    setApiKey(key) {
        this.apiKey = key;
    }
    requireAuth() {
        if (!this.isAuthenticated) {
            throw new NexApiError(401, "Not registered", {
                message: "No API key configured. Call the 'register' tool first with your email to get an API key.",
            });
        }
    }
    async request(method, path, body) {
        this.requireAuth();
        const url = `${getBaseUrl()}${path}`;
        const headers = {
            Authorization: `Bearer ${this.apiKey}`,
        };
        if (body !== undefined) {
            headers["Content-Type"] = "application/json";
        }
        const res = await fetch(url, {
            method,
            headers,
            body: body !== undefined ? JSON.stringify(body) : undefined,
            signal: AbortSignal.timeout(120_000),
        });
        if (res.status === 401 || res.status === 403) {
            throw new NexApiError(res.status, res.statusText, {
                message: "API key expired or invalid. Run 'wuphf register --email <email>' to get a new key.",
            });
        }
        if (!res.ok) {
            let errorBody;
            try {
                errorBody = await res.json();
            }
            catch {
                errorBody = await res.text();
            }
            throw new NexApiError(res.status, res.statusText, errorBody);
        }
        const text = await res.text();
        if (!text)
            return {};
        try {
            return JSON.parse(text);
        }
        catch {
            return { message: text };
        }
    }
    async register(email, name, companyName, source) {
        const body = {
            email,
            source: source ?? "mcp",
        };
        if (name !== undefined)
            body.name = name;
        if (companyName !== undefined)
            body.company_name = companyName;
        const res = await fetch(getRegisterUrl(), {
            method: "POST",
            headers: { "Content-Type": "application/json" },
            body: JSON.stringify(body),
            signal: AbortSignal.timeout(120_000),
        });
        if (!res.ok) {
            let errorBody;
            try {
                errorBody = await res.json();
            }
            catch {
                errorBody = await res.text();
            }
            throw new NexApiError(res.status, res.statusText, errorBody);
        }
        const data = await res.json();
        const apiKey = data.api_key;
        if (typeof apiKey === "string" && apiKey.length > 0) {
            this.apiKey = apiKey;
        }
        return data;
    }
    async get(path) {
        return this.request("GET", path);
    }
    async post(path, body) {
        return this.request("POST", path, body);
    }
    async put(path, body) {
        return this.request("PUT", path, body);
    }
    async patch(path, body) {
        return this.request("PATCH", path, body);
    }
    async delete(path) {
        return this.request("DELETE", path);
    }
    async getRaw(path) {
        this.requireAuth();
        const url = `${this.getBaseUrl()}${path}`;
        const res = await fetch(url, {
            method: "GET",
            headers: { Authorization: `Bearer ${this.apiKey}` },
            signal: AbortSignal.timeout(120_000),
        });
        if (!res.ok) {
            throw new NexApiError(res.status, res.statusText, await res.text());
        }
        return res.text();
    }
    getBaseUrl() {
        return `${(process.env.WUPHF_API_BASE_URL || process.env.NEX_API_BASE_URL || "https://app.nex.ai").replace(/\/+$/, "")}/api/developers`;
    }
}
//# sourceMappingURL=client.js.map