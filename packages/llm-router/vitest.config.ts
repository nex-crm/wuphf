import { defineConfig } from "vitest/config";

// Coverage thresholds intentionally start unset — the gateway will grow new
// surface (real Anthropic adapter, Ollama, etc.) and coverage numbers should
// settle once the post-§15.A surface is locked. Bring them in via a follow-up
// once the burn-down (§10.4) and the supervisor wire are both stable.
export default defineConfig({
  test: {
    include: ["tests/**/*.spec.ts"],
    environment: "node",
    coverage: {
      provider: "v8",
      include: ["src/**/*.ts"],
      reporter: process.env["CI"] ? ["text", "json-summary"] : ["text"],
      reportsDirectory: "coverage",
    },
  },
});
