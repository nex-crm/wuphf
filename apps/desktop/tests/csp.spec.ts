import { readFile } from "node:fs/promises";
import { describe, expect, it } from "vitest";

describe("renderer Content-Security-Policy", () => {
  it("matches the AGENTS.md rule 7 directive set", async () => {
    const [html, agents] = await Promise.all([
      readFile(new URL("../src/renderer/index.html", import.meta.url), "utf8"),
      readFile(new URL("../AGENTS.md", import.meta.url), "utf8"),
    ]);

    const htmlDirectives = parseCsp(extractHtmlCsp(html));
    const agentsDirectives = parseCsp(extractAgentsCsp(agents));

    expect(htmlDirectives).toEqual(agentsDirectives);
    expect(htmlDirectives).toMatchObject({
      "connect-src": ["'self'", "http://127.0.0.1:0"],
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

function extractAgentsCsp(agents: string): string {
  const match = agents.match(/\*\*CSP is strict\.\*\*.*?containing `([^`]+)`/s);
  if (match?.[1] === undefined) {
    throw new Error("Expected AGENTS.md rule 7 to contain the CSP string");
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
