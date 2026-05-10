// Renderer bundle server. Serves `/`, `/index.html`, and `/assets/*` from a
// configured directory. All other paths return 404 from this module — the
// listener routes API paths before reaching here.
//
// Path-traversal defence operates at four layers:
//   1. Lexical: `containsTraversalOrNul` rejects `..` segments and NUL bytes
//      before any path joining.
//   2. Lexical inside-root: after `resolve(root, fileRelativePath)`, assert
//      the joined absolute path is still inside `root` (closes the case
//      where `..`-encoded variants slipped past layer 1).
//   3. Realpath inside-root: resolve symlinks on both the requested file AND
//      the renderer-bundle root, then assert the realpath is still inside.
//      Without this, a symlink under the bundle dir (created in CI, a buggy
//      installer, or a malicious local writer) would let `/assets/leak`
//      read arbitrary filesystem content even though all lexical checks
//      pass.
//   4. FD-locked read: after the realpath check passes, open the file via
//      a FileHandle and run stat()/createReadStream FROM THAT HANDLE — not
//      from the path string. The handle operates on the inode that existed
//      at open() time, so an attacker swapping the path target between the
//      realpath snapshot and the read can't redirect the bytes. Closes the
//      TOCTOU window that layers 1-3 left open.

import { realpathSync } from "node:fs";
import { type FileHandle, open, realpath } from "node:fs/promises";
import type { ServerResponse } from "node:http";
import { isAbsolute, resolve, sep } from "node:path";

import type { RendererBundleSource } from "./types.ts";

const MIME_TYPES: Readonly<Record<string, string>> = {
  ".html": "text/html; charset=utf-8",
  ".js": "text/javascript; charset=utf-8",
  ".mjs": "text/javascript; charset=utf-8",
  ".css": "text/css; charset=utf-8",
  ".json": "application/json; charset=utf-8",
  ".svg": "image/svg+xml",
  ".png": "image/png",
  ".jpg": "image/jpeg",
  ".jpeg": "image/jpeg",
  ".gif": "image/gif",
  ".webp": "image/webp",
  ".ico": "image/x-icon",
  ".woff": "font/woff",
  ".woff2": "font/woff2",
  ".ttf": "font/ttf",
  ".otf": "font/otf",
  ".map": "application/json; charset=utf-8",
};

export interface StaticHandler {
  serve(pathname: string, res: ServerResponse): Promise<boolean>;
}

// Strict CSP for the loopback-served renderer bundle. Goals:
//   - script-src 'self' only — no inline scripts, no remote.
//   - style-src 'self' — no inline styles. The packaged renderer bundle
//     uses external <link> stylesheets (Vite extracts CSS at build).
//     Dev mode (electron-vite) loads the renderer through the dev server
//     and does NOT see this CSP, so dev HMR is unaffected.
//   - connect-src 'self' — covers fetch/XHR/WebSocket/EventSource to the
//     same origin. The broker's `/api/*` and `/terminal/agents/...` WS
//     all live on the same loopback origin, so a wildcard `127.0.0.1:*`
//     is unnecessary attack surface (would let renderer JS open sockets
//     to any other loopback listener on the machine).
//   - frame-ancestors 'none' + object-src 'none' — no embedding, no
//     plugins.
//   - base-uri 'self' — block `<base href>` redirection of relative URLs.
const RENDERER_CSP = [
  "default-src 'self'",
  "script-src 'self'",
  "style-src 'self'",
  "img-src 'self' data: blob:",
  "font-src 'self'",
  "connect-src 'self'",
  "object-src 'none'",
  "base-uri 'self'",
  "frame-ancestors 'none'",
].join("; ");

export function createStaticHandler(source: RendererBundleSource | null): StaticHandler {
  if (source === null) {
    return DISABLED_STATIC_HANDLER;
  }
  if (!isAbsolute(source.dir)) {
    throw new Error(
      `createStaticHandler: rendererDistDir must be an absolute path (got ${source.dir})`,
    );
  }
  const root = resolve(source.dir);
  // Resolve the bundle root's realpath once at construction. Used for the
  // realpath-inside-root check below. If the dir does not yet exist or the
  // realpath probe fails for any reason, fall back to the lexical root so
  // the broker still starts — every serve() will 404 on stat anyway, but
  // the broker should not refuse to bind just because the renderer bundle
  // is staged later (dev hot-reload, packaging-bug recovery).
  let rootRealpath: string;
  try {
    rootRealpath = realpathSync(root);
  } catch {
    rootRealpath = root;
  }
  return {
    async serve(pathname, res) {
      // Layer 1: lexical NUL/`..` reject — see top-of-file comment.
      if (containsTraversalOrNul(pathname)) {
        notFound(res);
        return true;
      }
      const fileRelativePath = mapRequestToFile(pathname);
      if (fileRelativePath === null) return false;
      const absolute = resolve(root, fileRelativePath);
      // Layer 2: lexical inside-root.
      if (!isInsideRoot(absolute, root)) {
        // Path traversal attempt; report 404 (not 403) so an attacker cannot
        // use response codes to discriminate file existence from policy.
        notFound(res);
        return true;
      }
      // Layer 3: realpath inside-root. Resolves symlinks; rejects when a
      // symlink under the bundle dir points outside it.
      let realAbsolute: string;
      try {
        realAbsolute = await realpath(absolute);
      } catch {
        notFound(res);
        return true;
      }
      if (!isInsideRoot(realAbsolute, rootRealpath)) {
        notFound(res);
        return true;
      }
      // Layer 4: open via FileHandle and read from THAT handle. The handle
      // operates on the inode that existed at open() time, so an attacker
      // swapping the path target between the realpath check and the read
      // can't redirect the bytes — the inode behind the FD is locked.
      // `handle.createReadStream` auto-closes on stream end/error.
      let handle: FileHandle;
      try {
        handle = await open(realAbsolute, "r");
      } catch {
        notFound(res);
        return true;
      }
      let handleClosed = false;
      try {
        const info = await handle.stat();
        if (!info.isFile()) {
          notFound(res);
          return true;
        }
        res.statusCode = 200;
        res.setHeader("Content-Type", contentTypeFor(realAbsolute));
        res.setHeader("Content-Length", String(info.size));
        res.setHeader("Cache-Control", "no-store");
        // Defence-in-depth: block MIME sniffing so an unmapped extension
        // (which falls back to application/octet-stream above) cannot be
        // re-interpreted as script by a permissive browser.
        res.setHeader("X-Content-Type-Options", "nosniff");
        res.setHeader("Referrer-Policy", "no-referrer");
        // The renderer is loaded from this server in packaged mode, so the
        // bundle's effective CSP is whatever this header sets. Even if the
        // bundle includes a <meta http-equiv="Content-Security-Policy"> the
        // server header takes precedence.
        res.setHeader("Content-Security-Policy", RENDERER_CSP);
        await pipeFromHandle(handle, res);
        handleClosed = true; // createReadStream auto-closes on end.
      } catch {
        // pipeFromHandle may have already flushed headers + bytes before
        // the stream errored mid-pipe (e.g., disk read failure on a large
        // bundle). Calling notFound() at that point invokes
        // res.writeHead() on a response whose headers are already on the
        // wire — Node throws ERR_HTTP_HEADERS_SENT. Only synthesize a 404
        // when nothing has been written yet; otherwise just end the
        // response so the client sees a truncated body rather than a
        // crashed handler.
        if (!res.headersSent) {
          notFound(res);
        } else if (!res.writableEnded) {
          res.end();
        }
      } finally {
        if (!handleClosed) {
          await handle.close().catch(() => undefined);
        }
      }
      return true;
    },
  };
}

function containsTraversalOrNul(pathname: string): boolean {
  if (pathname.includes("\0")) return true;
  // Match "..", "/..", "../", "/../" anywhere in the path.
  const segments = pathname.split("/");
  return segments.some((seg) => seg === "..");
}

const DISABLED_STATIC_HANDLER: StaticHandler = Object.freeze({
  serve: async () => false,
});

function mapRequestToFile(pathname: string): string | null {
  if (pathname === "/" || pathname === "/index.html") {
    return "index.html";
  }
  if (pathname.startsWith("/assets/")) {
    return pathname.slice(1); // strip leading "/"
  }
  return null;
}

function isInsideRoot(candidate: string, root: string): boolean {
  const rootWithSep = root.endsWith(sep) ? root : root + sep;
  return candidate === root || candidate.startsWith(rootWithSep);
}

function contentTypeFor(absolutePath: string): string {
  const dot = absolutePath.lastIndexOf(".");
  if (dot < 0) return "application/octet-stream";
  const ext = absolutePath.slice(dot).toLowerCase();
  return MIME_TYPES[ext] ?? "application/octet-stream";
}

function notFound(res: ServerResponse): void {
  res.statusCode = 404;
  res.setHeader("Content-Type", "text/plain; charset=utf-8");
  res.end("not_found");
}

function pipeFromHandle(handle: FileHandle, res: ServerResponse): Promise<void> {
  return new Promise((resolveFn, rejectFn) => {
    const stream = handle.createReadStream();
    stream.on("error", rejectFn);
    stream.on("end", () => resolveFn());
    stream.pipe(res);
  });
}
