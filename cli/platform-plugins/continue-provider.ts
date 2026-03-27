/**
 * WUPHF context provider for Continue.dev
 *
 * Add this to your ~/.continue/config.ts to enable @wuphf context:
 *
 * import { nexProvider } from "./.plugins/wuphf-provider";
 * export default { contextProviders: [nexProvider] };
 *
 * This file is copied to ~/.continue/.plugins/wuphf-provider.ts by `wuphf setup`.
 */

import { readFileSync } from "node:fs";
import { join } from "node:path";
import { homedir } from "node:os";

function loadApiKey(): string | null {
  try {
    const raw = readFileSync(join(homedir(), ".wuphf-mcp.json"), "utf-8");
    return JSON.parse(raw).api_key ?? null;
  } catch {
    return null;
  }
}

export const nexProvider = {
  title: "wuphf",
  displayTitle: "WUPHF Context",
  description: "Query organizational context, CRM, and memory from WUPHF",
  type: "query" as const,

  async getContextItems(query: string) {
    const apiKey = loadApiKey();
    if (!apiKey || !query.trim()) return [];

    try {
      const baseUrl = process.env.WUPHF_API_BASE_URL ?? "https://app.nex.ai";
      const res = await fetch(`${baseUrl}/api/developers/v1/context/ask`, {
        method: "POST",
        headers: {
          Authorization: `Bearer ${apiKey}`,
          "Content-Type": "application/json",
        },
        body: JSON.stringify({ query }),
        signal: AbortSignal.timeout(10_000),
      });

      if (!res.ok) return [];
      const data = (await res.json()) as { answer?: string };
      if (!data.answer) return [];

      return [
        {
          name: "WUPHF Context",
          description: `Results for: ${query}`,
          content: data.answer,
        },
      ];
    } catch {
      return [];
    }
  },
};
