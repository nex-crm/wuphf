const BASE_URL = "http://localhost:30000/api/developers";
const REGISTER_URL = "http://localhost:30000/api/v1/agents/register";

export class NexApiError extends Error {
  constructor(
    public status: number,
    public statusText: string,
    public body: unknown,
  ) {
    super(`Nex API error ${status}: ${statusText}`);
    this.name = "NexApiError";
  }
}

export class NexApiClient {
  private apiKey: string | undefined;

  constructor(apiKey?: string) {
    this.apiKey = apiKey;
  }

  get isAuthenticated(): boolean {
    return this.apiKey !== undefined && this.apiKey.length > 0;
  }

  setApiKey(key: string): void {
    this.apiKey = key;
  }

  private requireAuth(): void {
    if (!this.isAuthenticated) {
      throw new NexApiError(401, "Not registered", {
        message: "No API key configured. Call the 'register' tool first with your email to get an API key.",
      });
    }
  }

  private async request(
    method: string,
    path: string,
    body?: unknown,
  ): Promise<unknown> {
    this.requireAuth();
    const url = `${BASE_URL}${path}`;
    const headers: Record<string, string> = {
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

    if (!res.ok) {
      let errorBody: unknown;
      try {
        errorBody = await res.json();
      } catch {
        errorBody = await res.text();
      }
      throw new NexApiError(res.status, res.statusText, errorBody);
    }

    const text = await res.text();
    if (!text) return {};
    try {
      return JSON.parse(text);
    } catch {
      return { message: text };
    }
  }

  async register(email: string, name?: string, companyName?: string, source?: string): Promise<unknown> {
    const body: Record<string, string> = {
      email,
      source: source ?? "mcp",
    };
    if (name !== undefined) body.name = name;
    if (companyName !== undefined) body.company_name = companyName;

    const res = await fetch(REGISTER_URL, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(body),
      signal: AbortSignal.timeout(120_000),
    });

    if (!res.ok) {
      let errorBody: unknown;
      try {
        errorBody = await res.json();
      } catch {
        errorBody = await res.text();
      }
      throw new NexApiError(res.status, res.statusText, errorBody);
    }

    const data = await res.json();
    const apiKey = (data as Record<string, unknown>).api_key;
    if (typeof apiKey === "string" && apiKey.length > 0) {
      this.apiKey = apiKey;
    }
    return data;
  }

  async get(path: string): Promise<unknown> {
    return this.request("GET", path);
  }

  async post(path: string, body?: unknown): Promise<unknown> {
    return this.request("POST", path, body);
  }

  async put(path: string, body?: unknown): Promise<unknown> {
    return this.request("PUT", path, body);
  }

  async patch(path: string, body?: unknown): Promise<unknown> {
    return this.request("PATCH", path, body);
  }

  async delete(path: string): Promise<unknown> {
    return this.request("DELETE", path);
  }
}
