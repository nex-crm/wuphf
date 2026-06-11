import { launchBrowser, shotElement, shotPage } from "./lib.mjs";
import process from "node:process";

const OUT = process.env.WUPHF_SCREENSHOTS_OUT;
const BASE = process.env.BASE_URL ?? "http://localhost:5273";
if (!OUT) {
  console.error("WUPHF_SCREENSHOTS_OUT is not set; run via publish.sh");
  process.exit(2);
}

function prereq(name, found) {
  return {
    name,
    required: false,
    found,
    ok: found,
    version: found ? `${name}-1.0.0` : undefined,
  };
}

function localProvider(kind, connected) {
  return {
    kind,
    binary_installed: connected,
    endpoint: `http://localhost/${kind}`,
    model: `${kind}-model`,
    reachable: connected,
    probed: true,
    platform_supported: true,
  };
}

const SHELL_RESPONSES = {
  "/actions": { actions: [] },
  "/channels": { channels: [{ slug: "general", name: "general" }] },
  "/commands": [],
  "/company": { name: "Screenshot Office" },
  "/decisions": { decisions: [] },
  "/health": { ok: true },
  "/humans/me": { human: { slug: "you" } },
  "/inbox/items": { items: [] },
  "/messages": { messages: [] },
  "/office-members": { members: [] },
  "/requests": { requests: [] },
  "/review/list": { items: [] },
  "/scheduler": { jobs: [] },
  "/skills": { skills: [] },
  "/tasks": { tasks: [] },
  "/tasks/inbox": { items: [] },
  "/upgrade-check": { update_available: false },
  "/usage": { total_usd: 0, daily_usd: 0 },
  "/watchdogs": { watchdogs: [] },
  "/workspaces/list": { workspaces: [] },
};

function apiMockResponse(path, request, onboarded) {
  if (path === "/onboarding/state") return { body: { onboarded } };
  if (path === "/onboarding/prereqs") {
    return {
      body: {
        prereqs: [
          prereq("claude", true),
          prereq("codex", false),
          prereq("opencode", true),
        ],
      },
    };
  }
  if (path === "/status/local-providers") {
    return {
      body: [
        localProvider("ollama", true),
        localProvider("exo", true),
        localProvider("mlx-lm", false),
      ],
    };
  }
  if (path === "/config" && request.method() === "POST") {
    return { body: { ok: true } };
  }
  if (path === "/config") {
    return {
      body: {
        llm_provider: "claude-code",
        llm_provider_priority: [
          "claude-code",
          "codex",
          "opencode",
          "ollama",
          "exo",
          "mlx-lm",
        ],
      },
    };
  }
  if (path === "/task-plan") {
    return {
      body: { tasks: [{ id: "shot-task-1", title: "Provider smoke task" }] },
    };
  }
  if (path === "/events") return { eventStream: true };
  if (Object.hasOwn(SHELL_RESPONSES, path)) {
    return { body: SHELL_RESPONSES[path] };
  }
  throw new Error(`Unhandled screenshot API mock: ${path}`);
}

async function installProviderMocks(browserContext, { onboarded }) {
  await browserContext.unrouteAll().catch((err) => {
    const message = err instanceof Error ? err.message : String(err);
    console.warn(`provider-selection route cleanup failed: ${message}`);
  });
  await browserContext.route("**/*", async (route) => {
    const request = route.request();
    const url = new URL(request.url());
    if (url.pathname === "/api-token") {
      return route.fulfill({
        contentType: "application/json",
        body: JSON.stringify({
          token: "stub",
          broker_url: `${BASE}/api`,
        }),
      });
    }
    if (!url.pathname.startsWith("/api/")) return route.continue();

    const path = url.pathname.replace(/^\/api/, "") || "/";
    const response = apiMockResponse(path, request, onboarded);
    if (response.eventStream) {
      return route.fulfill({
        status: 200,
        contentType: "text/event-stream",
        body: "",
      });
    }
    return json(route, response.body);
  });
}

async function json(route, body) {
  await route.fulfill({
    contentType: "application/json",
    body: JSON.stringify(body),
  });
}

async function openShellRoute(targetPage, path) {
  await targetPage.goto(`${BASE}${path}`, { waitUntil: "load" });
  await targetPage.evaluate(async () => {
    const m = await import("/src/stores/app.ts");
    m.useAppStore.setState({
      brokerConnected: true,
      onboardingComplete: true,
    });
  });
  await targetPage.locator(".status-bar").waitFor({ timeout: 10_000 });
}

const { browser, context, page } = await launchBrowser({
  viewport: { width: 1400, height: 980 },
});

try {
  await installProviderMocks(context, { onboarded: false });
  await page.goto(`${BASE}/#/channels/general`, { waitUntil: "load" });
  await page.getByTestId("pre-pick-card-claude-code").click();
  await page.getByTestId("pre-pick-card-opencode").click();
  await page.getByTestId("pre-pick-local-tile-ollama").click();
  await page.getByText("Selected: Claude Code, Opencode, Ollama").waitFor();
  await shotPage(page, OUT, "01-onboarding-multi-provider-selection");

  await installProviderMocks(context, { onboarded: true });
  await openShellRoute(page, "/#/apps/activity");
  await page.getByText("4 providers connected").waitFor();
  await shotElement(
    page,
    "#connected-providers",
    OUT,
    "02-overview-connected-providers",
  );

  await openShellRoute(page, "/#/apps/settings");
  await page.getByText("Available providers").waitFor();
  await shotElement(
    page,
    "label:has-text('Claude Code')",
    OUT,
    "03-settings-provider-row",
  );

  await openShellRoute(page, "/#/tasks/new");
  await page.getByTestId("issue-new").waitFor();
  await page.getByTestId("issue-new-provider").selectOption("exo");
  await shotElement(
    page,
    ".issue-new-form",
    OUT,
    "04-task-create-provider-select",
  );

  console.log(`captured 4 screenshots to ${OUT}`);
} finally {
  await browser.close();
}
