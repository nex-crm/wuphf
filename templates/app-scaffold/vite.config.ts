import react from "@vitejs/plugin-react";
import { defineConfig } from "vite";
import { viteSingleFile } from "vite-plugin-singlefile";

// During a WUPHF live preview the broker runs `bun run dev` and fronts it with a
// reverse proxy on a distinct 127.0.0.1 port (it injects the security CSP — do
// not rely on serving headers here). WUPHF_HMR_CLIENT_PORT is that proxy port:
// Vite's HMR client must connect back through it, not the raw Vite port (which
// the proxy CSP blocks as cross-origin). Unset outside a preview (normal build).
const hmrClientPort = Number(process.env.WUPHF_HMR_CLIENT_PORT) || undefined;

// Build a SINGLE self-contained index.html: all JS + CSS inlined, no external
// requests. That one file is what register_app publishes; WUPHF renders it in a
// sandboxed iframe with connect-src 'none', so nothing external would load
// anyway. base: "./" keeps any residual asset refs relative.
export default defineConfig({
  base: "./",
  plugins: [react(), viteSingleFile()],
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
});
