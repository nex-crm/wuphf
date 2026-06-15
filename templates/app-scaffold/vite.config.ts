import react from "@vitejs/plugin-react";
import { defineConfig } from "vite";
import { viteSingleFile } from "vite-plugin-singlefile";

// Build a SINGLE self-contained index.html: all JS + CSS inlined, no external
// requests. That one file is what register_app publishes; WUPHF renders it in a
// sandboxed iframe with connect-src 'none', so nothing external would load
// anyway. base: "./" keeps any residual asset refs relative.
export default defineConfig({
  base: "./",
  plugins: [react(), viteSingleFile()],
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
