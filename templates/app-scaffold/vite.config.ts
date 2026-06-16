import react from "@vitejs/plugin-react";
import { defineConfig } from "vite";
import { viteSingleFile } from "vite-plugin-singlefile";

// During a WUPHF live preview the broker runs `bun run dev` and fronts it with a
// reverse proxy on a distinct 127.0.0.1 port (it injects the security CSP — do
// not rely on serving headers here). WUPHF_HMR_CLIENT_PORT is that proxy port:
// Vite's HMR client must connect back through it, not the raw Vite port (which
// the proxy CSP blocks as cross-origin). Unset outside a preview (normal build).
const hmrClientPort = Number(process.env.WUPHF_HMR_CLIENT_PORT) || undefined;

// Dev-only Babel plugin: stamp every host (lowercase) JSX element with its
// source location as `data-wuphf-source="relPath:line:col"`, so the live
// preview's "select to edit" can map a click straight back to the exact line.
// React 19 removed fiber `_debugSource`, so a DOM attribute is the robust,
// version-proof source map. Added ONLY on `vite serve` (dev); the production
// single-file build never carries it, so the sealed bundle stays clean.
// vite.config.ts is not part of the app's `tsc` include, so the untyped Babel
// AST here costs the app nothing.
// biome-ignore lint: build-config Babel plugin, intentionally loosely typed.
function wuphfSourceStamp({ types: t }: any) {
  const ATTR = "data-wuphf-source";
  return {
    name: "wuphf-source-stamp",
    visitor: {
      // biome-ignore lint: Babel visitor node/state are untyped here.
      JSXOpeningElement(path: any, state: any) {
        const node = path.node;
        if (node.name.type !== "JSXIdentifier") return;
        if (!/^[a-z]/.test(node.name.name)) return; // host elements only
        if (!node.loc) return;
        if (
          node.attributes.some(
            // biome-ignore lint: untyped Babel attribute node.
            (a: any) =>
              a.type === "JSXAttribute" && a.name && a.name.name === ATTR,
          )
        ) {
          return;
        }
        const file: string = state.filename || "";
        const i = file.lastIndexOf("/src/");
        const rel = i >= 0 ? file.slice(i + 5) : file.split("/").pop() || file;
        const value = `${rel}:${node.loc.start.line}:${node.loc.start.column + 1}`;
        node.attributes.push(
          t.jsxAttribute(t.jsxIdentifier(ATTR), t.stringLiteral(value)),
        );
      },
    },
  };
}

// Dev-only: inject the "select to edit" / error-capture inspector into the page
// at the HTML level, so it loads no matter how the agent rewrites src/main.tsx.
// Wiring it through index.html (not app source) is what keeps the live-preview
// tooling robust across rebuilds; it never reaches the production build.
function wuphfInspectorInject() {
  return {
    name: "wuphf-inspector-inject",
    apply: "serve" as const,
    transformIndexHtml() {
      // A STATIC external module script (not a fire-and-forget dynamic import):
      // it joins the document's module graph and self-installs on load, so it
      // can't lose the first-paint fetch race and silently no-op.
      return [
        {
          tag: "script",
          attrs: { type: "module", src: "/src/wuphf-inspector.ts" },
          injectTo: "body" as const,
        },
      ];
    },
  };
}

// Build a SINGLE self-contained index.html: all JS + CSS inlined, no external
// requests. That one file is what register_app publishes; WUPHF renders it in a
// sandboxed iframe with connect-src 'none', so nothing external would load
// anyway. base: "./" keeps any residual asset refs relative.
export default defineConfig(({ command }) => ({
  base: "./",
  plugins: [
    react(
      command === "serve" ? { babel: { plugins: [wuphfSourceStamp] } } : {},
    ),
    wuphfInspectorInject(),
    viteSingleFile(),
  ],
  server: {
    host: "127.0.0.1",
    ...(hmrClientPort ? { hmr: { clientPort: hmrClientPort } } : {}),
  },
  build: {
    target: "esnext",
    cssCodeSplit: false,
    assetsInlineLimit: 100_000_000,
    chunkSizeWarningLimit: 4096,
    rollupOptions: {
      output: { inlineDynamicImports: true },
    },
  },
}));
