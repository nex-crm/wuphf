/**
 * HTTP client for the Nex Developer API.
 * Hardcoded base URL with all HTTP methods.
 */

import { AuthError, RateLimitError, ServerError } from "./errors.js";
import { API_BASE, REGISTER_URL } from "./config.js";

export class NexClient {
  private apiKey: string | undefined;
  private timeoutMs: number;

  constructor(apiKey?: string, timeoutMs = 120_000) {
    this.apiKey = apiKey;
    this.timeoutMs = timeoutMs;
  }

  get isAuthenticated(): boolean {
    return this.apiKey !== undefined && this.apiKey.length > 0;
  }

  setApiKey(key: string): void {
    this.apiKey = key;
  }

  private requireAuth(): void {
    if (!this.isAuthenticated) {
      throw new AuthError();
    }
  }

  private async request<T = unknown>(
    method: string,
    path: string,
    body?: unknown,
    timeoutMs?: number,
  ): Promise<T> {
    this.requireAuth();
    const url = `${API_BASE}${path}`;
    const controller = new AbortController();
    const timer = setTimeout(() => controller.abort(), timeoutMs ?? this.timeoutMs);

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
        throw new AuthError("Invalid or expired API key.");
      }

      if (res.status === 429) {
        const retryAfter = res.headers.get("retry-after");
        const ms = retryAfter ? parseInt(retryAfter, 10) * 1000 : 60_000;
        throw new RateLimitError(ms);
      }

      if (!res.ok) {
        let errorBody: string | undefined;
        try {
          errorBody = await res.text();
        } catch {
          // ignore
        }
        throw new ServerError(res.status, errorBody);
      }

      const text = await res.text();
      if (!text || !text.trim()) return {} as T;
      return JSON.parse(text) as T;
    } finally {
      clearTimeout(timer);
    }
  }

  async register(email: string, name?: string, companyName?: string): Promise<Record<string, unknown>> {
    const body: Record<string, string> = { email, source: "cli" };
    if (name !== undefined) body.name = name;
    if (companyName !== undefined) body.company_name = companyName;

    const res = await fetch(REGISTER_URL, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(body),
      signal: AbortSignal.timeout(this.timeoutMs),
    });

    if (!res.ok) {
      let errorBody: string | undefined;
      try {
        errorBody = await res.text();
      } catch {
        // ignore
      }
      throw new ServerError(res.status, errorBody);
    }

    const data = (await res.json()) as Record<string, unknown>;
    const apiKey = data.api_key;
    if (typeof apiKey === "string" && apiKey.length > 0) {
      this.apiKey = apiKey;
    }
    return data;
  }

  async get<T = unknown>(path: string, timeoutMs?: number): Promise<T> {
    return this.request<T>("GET", path, undefined, timeoutMs);
  }

  async post<T = unknown>(path: string, body?: unknown, timeoutMs?: number): Promise<T> {
    return this.request<T>("POST", path, body, timeoutMs);
  }

  async put<T = unknown>(path: string, body?: unknown, timeoutMs?: number): Promise<T> {
    return this.request<T>("PUT", path, body, timeoutMs);
  }

  async patch<T = unknown>(path: string, body?: unknown, timeoutMs?: number): Promise<T> {
    return this.request<T>("PATCH", path, body, timeoutMs);
  }

  async delete<T = unknown>(path: string, timeoutMs?: number): Promise<T> {
    return this.request<T>("DELETE", path, undefined, timeoutMs);
  }
}
