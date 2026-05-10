import { mkdirSync, mkdtempSync, rmSync, symlinkSync, writeFileSync } from "node:fs";
import { request } from "node:http";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { asApiToken } from "@wuphf/protocol";
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import type { BrokerHandle } from "../src/index.ts";
import { createBroker } from "../src/index.ts";

interface RawResponse {
  readonly status: number;
  readonly body: string;
}

// `fetch` normalizes URLs (collapses `..` segments client-side) before send,
// so it cannot exercise the broker's path-traversal guard. Use http.request
// to pass the raw request line through to the listener.
function rawGet(port: number, rawPath: string): Promise<RawResponse> {
  return new Promise((resolveFn, rejectFn) => {
    const req = request({ host: "127.0.0.1", port, path: rawPath, method: "GET" }, (res) => {
      const chunks: Buffer[] = [];
      res.on("data", (c: Buffer) => chunks.push(c));
      res.on("end", () =>
        resolveFn({ status: res.statusCode ?? 0, body: Buffer.concat(chunks).toString("utf8") }),
      );
    });
    req.on("error", rejectFn);
    req.end();
  });
}

const FIXED_TOKEN = asApiToken("static-test-token-AAAAAAAAAAAAAAAAAAAAAAAA");

describe("static renderer bundle", () => {
  let broker: BrokerHandle | null = null;
  let bundleDir: string;

  beforeEach(() => {
    bundleDir = mkdtempSync(join(tmpdir(), "wuphf-broker-static-"));
    writeFileSync(
      join(bundleDir, "index.html"),
      "<!doctype html><meta charset=utf-8><title>wuphf</title><div id=app></div>",
      "utf8",
    );
    mkdirSync(join(bundleDir, "assets"), { recursive: true });
    writeFileSync(join(bundleDir, "assets", "main.js"), 'console.log("hi")', "utf8");
  });

  afterEach(async () => {
    if (broker !== null) {
      await broker.stop();
      broker = null;
    }
    rmSync(bundleDir, { recursive: true, force: true });
  });

  it("serves index.html at /", async () => {
    broker = await createBroker({ token: FIXED_TOKEN, renderer: { dir: bundleDir } });
    const res = await fetch(`${broker.url}/`);
    expect(res.status).toBe(200);
    expect(res.headers.get("content-type")).toMatch(/^text\/html/);
    expect(await res.text()).toContain("<title>wuphf</title>");
  });

  it("serves index.html at /index.html", async () => {
    broker = await createBroker({ token: FIXED_TOKEN, renderer: { dir: bundleDir } });
    const res = await fetch(`${broker.url}/index.html`);
    expect(res.status).toBe(200);
  });

  it("serves assets with the right mime type", async () => {
    broker = await createBroker({ token: FIXED_TOKEN, renderer: { dir: bundleDir } });
    const res = await fetch(`${broker.url}/assets/main.js`);
    expect(res.status).toBe(200);
    expect(res.headers.get("content-type")).toMatch(/^text\/javascript/);
  });

  it("404s on raw `..`-segment path traversal", async () => {
    broker = await createBroker({ token: FIXED_TOKEN, renderer: { dir: bundleDir } });
    const res = await rawGet(broker.port, "/assets/../../etc-shadow");
    expect(res.status).toBe(404);
  });

  it("404s on percent-encoded `..` segments (Vite-style %2e%2e)", async () => {
    broker = await createBroker({ token: FIXED_TOKEN, renderer: { dir: bundleDir } });
    // %2e%2e decodes to ".." — without an explicit traversal block, this
    // would resolve to <root>/index.html and serve it under /assets/<...>
    // with a wrong MIME type for the asset URL.
    const res = await rawGet(broker.port, "/assets/%2e%2e/index.html");
    expect(res.status).toBe(404);
  });

  it("404s on traversal that resolves back inside root (silent file substitution)", async () => {
    broker = await createBroker({ token: FIXED_TOKEN, renderer: { dir: bundleDir } });
    // /assets/foo/../main.js resolves to <root>/assets/main.js and would
    // serve the real file under a synthetic URL. Reject before resolution.
    const res = await rawGet(broker.port, "/assets/foo/../main.js");
    expect(res.status).toBe(404);
  });

  it("404s on NUL byte in path", async () => {
    broker = await createBroker({ token: FIXED_TOKEN, renderer: { dir: bundleDir } });
    const res = await rawGet(broker.port, "/assets/main.js%00.html");
    expect(res.status).toBe(404);
  });

  it("404s for missing files under assets/", async () => {
    broker = await createBroker({ token: FIXED_TOKEN, renderer: { dir: bundleDir } });
    const res = await fetch(`${broker.url}/assets/missing.css`);
    expect(res.status).toBe(404);
  });

  it("returns 404 on / when renderer is null", async () => {
    broker = await createBroker({ token: FIXED_TOKEN });
    const res = await fetch(`${broker.url}/`);
    expect(res.status).toBe(404);
  });

  it("rejects non-absolute renderer dir at construction time", async () => {
    await expect(createBroker({ renderer: { dir: "relative/path" } })).rejects.toThrow(
      /absolute path/,
    );
  });

  it("returns 404 when /assets/<name> resolves to a directory", async () => {
    // Create assets/sub/ dir under the bundle to exercise the !isFile path.
    mkdirSync(join(bundleDir, "assets", "sub"), { recursive: true });
    broker = await createBroker({ token: FIXED_TOKEN, renderer: { dir: bundleDir } });
    const res = await fetch(`${broker.url}/assets/sub`);
    expect(res.status).toBe(404);
  });

  it("emits a strict Content-Security-Policy and X-Content-Type-Options on bundle responses", async () => {
    broker = await createBroker({ token: FIXED_TOKEN, renderer: { dir: bundleDir } });
    const res = await fetch(`${broker.url}/`);
    expect(res.status).toBe(200);
    const csp = res.headers.get("content-security-policy") ?? "";
    expect(csp).toContain("default-src 'self'");
    expect(csp).toContain("script-src 'self'");
    expect(csp).toContain("frame-ancestors 'none'");
    expect(csp).toContain("object-src 'none'");
    expect(res.headers.get("x-content-type-options")).toBe("nosniff");
    expect(res.headers.get("referrer-policy")).toBe("no-referrer");
  });

  it("CSP does not include unsafe-inline (style-src tightened post-triangulation)", async () => {
    broker = await createBroker({ token: FIXED_TOKEN, renderer: { dir: bundleDir } });
    const res = await fetch(`${broker.url}/`);
    const csp = res.headers.get("content-security-policy") ?? "";
    expect(csp).not.toContain("'unsafe-inline'");
    expect(csp).not.toContain("'unsafe-eval'");
    // connect-src tightened to 'self' (was wildcard 127.0.0.1:* before).
    expect(csp).toContain("connect-src 'self'");
    expect(csp).not.toContain("127.0.0.1:*");
  });

  it("404s on a symlink under the bundle that escapes the renderer dir (realpath defense)", async () => {
    // Construct an outside-of-bundle file the symlink will point at.
    const outsideDir = mkdtempSync(join(tmpdir(), "wuphf-broker-outside-"));
    const outsidePath = join(outsideDir, "secret.txt");
    writeFileSync(outsidePath, "this should never leak", "utf8");
    try {
      // Create a symlink under the bundle dir's assets/ that targets the
      // outside file. Lexical checks would pass (the link itself is inside
      // root) — only the realpath defence catches this.
      symlinkSync(outsidePath, join(bundleDir, "assets", "leak"));
      broker = await createBroker({ token: FIXED_TOKEN, renderer: { dir: bundleDir } });
      const res = await fetch(`${broker.url}/assets/leak`);
      expect(res.status).toBe(404);
    } finally {
      rmSync(outsideDir, { recursive: true, force: true });
    }
  });

  it("still serves a symlink that points to a file INSIDE the bundle (realpath stays inside root)", async () => {
    // Sanity: legitimate symlinks within the bundle (e.g. from build tools
    // that share assets via symlinks) must keep working.
    symlinkSync(join(bundleDir, "assets", "main.js"), join(bundleDir, "assets", "alias.js"));
    broker = await createBroker({ token: FIXED_TOKEN, renderer: { dir: bundleDir } });
    const res = await fetch(`${broker.url}/assets/alias.js`);
    expect(res.status).toBe(200);
    expect(await res.text()).toContain("console.log");
  });
});
