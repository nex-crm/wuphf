/**
 * HTTP client for the Nex Developer API.
 * Uses native fetch with AbortController timeouts.
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
    super(`Nex API error ${status}${body ? `: ${body}` : ""}`);
    this.name = "NexServerError";
    this.status = status;
  }
}

// --- Response types ---

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
        let body: string | undefined;
        try {
          body = await res.text();
        } catch {
          // ignore
        }
        throw new NexServerError(res.status, body);
      }

      const text = await res.text();
      if (!text) return {} as T;
      return JSON.parse(text) as T;
    } finally {
      clearTimeout(timer);
    }
  }

  /** Generic GET request. */
  async get<T = unknown>(path: string, timeoutMs?: number): Promise<T> {
    return this.request<T>("GET", path, undefined, timeoutMs);
  }

  /** Generic POST request. */
  async post<T = unknown>(path: string, body?: unknown, timeoutMs?: number): Promise<T> {
    return this.request<T>("POST", path, body, timeoutMs);
  }

  /** Generic DELETE request. */
  async delete<T = unknown>(path: string, timeoutMs?: number): Promise<T> {
    return this.request<T>("DELETE", path, undefined, timeoutMs);
  }

  /** Ingest text content into the Nex knowledge graph. */
  async ingest(content: string, context?: string): Promise<IngestResponse> {
    const body: Record<string, string> = { content };
    if (context) body.context = context;
    return this.request<IngestResponse>("POST", "/v1/context/text", body, 60_000);
  }

  /** Ask a question against the Nex knowledge graph. */
  async ask(query: string, sessionId?: string, timeoutMs?: number): Promise<AskResponse> {
    const body: Record<string, unknown> = { query };
    if (sessionId) body.session_id = sessionId;
    return this.request<AskResponse>("POST", "/v1/context/ask", body, timeoutMs);
  }

  /** Lightweight health check — validates API key connectivity. */
  async healthCheck(): Promise<boolean> {
    try {
      // Use a minimal ask call with short timeout
      await this.request("POST", "/v1/context/ask", { query: "ping" }, 5000);
      return true;
    } catch (err) {
      if (err instanceof NexAuthError) throw err; // Auth errors should propagate
      return false; // Network/server errors = unhealthy but not fatal
    }
  }
}
