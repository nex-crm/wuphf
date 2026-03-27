/**
 * HTTP client for the WUPHF Developer API.
 * Adapted from openclaw-plugin — uses native fetch with timeout.
 */

// --- Error types ---

export class NexAuthError extends Error {
  constructor(message = "Invalid or missing API key") {
    super(message);
    this.name = "NexAuthError";
  }
}

export class NexRateLimitError extends Error {
  public retryAfterMs: number;

  constructor(retryAfterMs = 60_000) {
    super(`Rate limited — retry after ${retryAfterMs}ms`);
    this.name = "NexRateLimitError";
    this.retryAfterMs = retryAfterMs;
  }
}

export class NexServerError extends Error {
  public status: number;

  constructor(status: number, body?: string) {
    super(`WUPHF API error ${status}${body ? `: ${body}` : ""}`);
    this.name = "NexServerError";
    this.status = status;
  }
}

// --- Response types ---

export interface RegisterResponse {
  api_key: string;
  workspace_id?: string | number;
  workspace_slug?: string;
  [key: string]: unknown;
}

export interface IngestResponse {
  artifact_id: string;
}

export interface EntityReference {
  id?: number;
  name: string;
  type: string;
  count?: number;
}

export interface AskResponse {
  answer: string;
  session_id?: string;
  entity_references?: EntityReference[];
}

// --- Client ---

export class NexClient {
  private apiKey: string;
  private baseUrl: string;

  constructor(apiKey: string, baseUrl: string) {
    this.apiKey = apiKey;
    this.baseUrl = baseUrl;
  }

  private async request<T>(
    method: string,
    path: string,
    body?: unknown,
    timeoutMs = 10_000
  ): Promise<T> {
    const url = `${this.baseUrl}/api/developers${path}`;
    const controller = new AbortController();
    const timer = setTimeout(() => controller.abort(), timeoutMs);

    try {
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
        signal: controller.signal,
      });

      if (res.status === 401 || res.status === 403) {
        throw new NexAuthError();
      }

      if (res.status === 429) {
        const retryAfter = res.headers.get("retry-after");
        const ms = retryAfter ? parseInt(retryAfter, 10) * 1000 : 60_000;
        throw new NexRateLimitError(ms);
      }

      if (!res.ok) {
        let errorBody: string | undefined;
        try {
          errorBody = await res.text();
        } catch {
          // ignore
        }
        throw new NexServerError(res.status, errorBody);
      }

      const text = await res.text();
      if (!text || !text.trim()) return {} as T;
      return JSON.parse(text) as T;
    } finally {
      clearTimeout(timer);
    }
  }

  /**
   * Register a new account and get an API key.
   * Does NOT require an existing API key — uses the public registration endpoint.
   */
  static async register(
    baseUrl: string,
    email: string,
    name?: string,
    companyName?: string,
  ): Promise<RegisterResponse> {
    const url = `${baseUrl}/api/v1/agents/register`;
    const body: Record<string, string> = { email, source: "claude-code" };
    if (name) body.name = name;
    if (companyName) body.company_name = companyName;

    const controller = new AbortController();
    const timer = setTimeout(() => controller.abort(), 30_000);

    try {
      const res = await fetch(url, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify(body),
        signal: controller.signal,
      });

      if (!res.ok) {
        let errorBody: string | undefined;
        try { errorBody = await res.text(); } catch { /* ignore */ }
        throw new NexServerError(res.status, errorBody);
      }

      return await res.json() as RegisterResponse;
    } finally {
      clearTimeout(timer);
    }
  }

  /** Ingest text content into the WUPHF knowledge graph. */
  async ingest(content: string, context?: string, timeoutMs?: number): Promise<IngestResponse> {
    const body: Record<string, string> = { content };
    if (context) body.context = context;
    return this.request<IngestResponse>("POST", "/v1/context/text", body, timeoutMs ?? 60_000);
  }

  /** Ask a question against the WUPHF knowledge graph. */
  async ask(query: string, sessionId?: string, timeoutMs?: number): Promise<AskResponse> {
    const body: Record<string, unknown> = { query };
    if (sessionId) body.session_id = sessionId;
    return this.request<AskResponse>("POST", "/v1/context/ask", body, timeoutMs);
  }
}
