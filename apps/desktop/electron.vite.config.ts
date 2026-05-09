import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { defineConfig, externalizeDepsPlugin } from "electron-vite";

const rootDir = dirname(fileURLToPath(import.meta.url));

export default defineConfig({
  main: {
    plugins: [externalizeDepsPlugin()],
    build: {
      outDir: "out/main",
      rollupOptions: {
        input: {
          index: resolve(rootDir, "src/main/index.ts"),
          "broker-stub": resolve(rootDir, "src/main/broker-stub.ts"),
        },
        output: {
          format: "es",
          entryFileNames: "[name].js",
        },
      },
    },
  },
  preload: {
    plugins: [externalizeDepsPlugin()],
    build: {
      outDir: "out/preload",
      rollupOptions: {
        input: resolve(rootDir, "src/preload/preload.ts"),
        output: {
          format: "cjs",
          entryFileNames: "preload.js",
        },
      },
    },
  },
  renderer: {
    root: resolve(rootDir, "src/renderer"),
    server: {
      host: "localhost",
      port: 5173,
      strictPort: true,
    },
    build: {
      outDir: resolve(rootDir, "out/renderer"),
      emptyOutDir: true,
      rollupOptions: {
        input: resolve(rootDir, "src/renderer/index.html"),
      },
    },
  },
});
