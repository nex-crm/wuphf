import { readFile } from "node:fs/promises";
import { describe, expect, it } from "vitest";

import { BASE_RENDERER_CSP } from "../src/main/csp.ts";

describe("renderer Content-Security-Policy", () => {
  it("keeps the meta tag as a strict fallback without a broker port", async () => {
    const html = await readFile(new URL("../src/renderer/index.html", import.meta.url), "utf8");

    const htmlDirectives = parseCsp(extractHtmlCsp(html));

    expect(extractHtmlCsp(html)).toBe(BASE_RENDERER_CSP);
    expect(htmlDirectives).toMatchObject({
      "connect-src": ["'self'"],
      "base-uri": ["'none'"],
      "form-action": ["'none'"],
      "object-src": ["'none'"],
      "frame-ancestors": ["'none'"],
      "worker-src": ["'none'"],
    });
  });
});

function extractHtmlCsp(html: string): string {
  const match = html.match(
    /<meta\s+http-equiv="Content-Security-Policy"\s+content="([^"]+)"\s*\/>/s,
  );
  if (match?.[1] === undefined) {
    throw new Error("Expected renderer index.html to contain a CSP meta tag");
  }
  return match[1];
}

function parseCsp(value: string): Record<string, readonly string[]> {
  return Object.fromEntries(
    value.split(";").map((rawDirective) => {
      const parts = rawDirective.trim().split(/\s+/);
      const name = parts[0];
      if (name === undefined || name.length === 0) {
        throw new Error(`Invalid empty CSP directive in ${value}`);
      }
      return [name, parts.slice(1)];
    }),
  );
}
