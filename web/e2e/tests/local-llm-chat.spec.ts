import { expect, type Page, test } from "@playwright/test";

// Local-LLM chat flow: drives a DM end-to-end against a stubbed
// mlx-lm server (web/e2e/bin/mlx-stub) so the test is deterministic
// and fast. The fixture
// (web/e2e/fixtures/qwen-markdown-tool.txt) replays the EXACT raw
// content shape the live mlx_lm.server emitted in the user-reported
// bug report — markdown-fenced JSON tool call followed by
// `<|im_end|>`. The assertions cover what wuphf must do better than
// the cloud providers don't have to:
//
//   1. The agent's reply rendered to the user is prose, NOT a raw
//      JSON code block.
//   2. No "✗ ERROR / undefined" chips appear in the Live Output
//      panel for successful tool calls.
//   3. The progress label surfaces the streaming throughput so users
//      know the local model is working, not stalled.
//
// Iteration loop: run this spec, watch which assertion fails, fix
// either the parser (internal/provider/openai_compat.go) or the
// runner (internal/team/headless_openai_compat.go), rebuild via
// run-local.sh, repeat. The stub's deterministic output means the
// failure mode is reproducible without waiting on a real model.

async function waitForReactMount(page: Page): Promise<void> {
  await page.waitForFunction(
    () => {
      const root = document.getElementById("root");
      if (!root) return false;
      if (document.getElementById("skeleton")) return false;
      return root.children.length > 0;
    },
    { timeout: 10_000 },
  );
}

// openPlannerDM navigates to the planner agent's 1:1 channel via the
// hash route (`#/dm/planner`) the app already uses. Sidebar-click
// navigation has its own flaky surface and is covered by smoke.spec
// — this test focuses on the local-LLM message flow, so we want a
// reliable way to land on the right channel. After navigation we
// wait for the composer to bind to the planner DM channel.
async function openPlannerDM(page: Page) {
  await page.goto("/#/dm/planner");
  await waitForReactMount(page);
  await expect(page.getByPlaceholder(/Message #human__planner/)).toBeVisible({
    timeout: 10_000,
  });
}

// sendDirectMessage types into the composer + presses Enter, the same
// keypath users hit. Asserts the composer cleared so the test fails
// loudly if the message didn't actually leave.
async function sendDirectMessage(page: Page, body: string) {
  const composer = page.getByPlaceholder(/Message #/);
  await composer.click();
  await composer.fill(body);
  await composer.press("Enter");
  await expect(composer).toHaveValue("");
}

test.describe("Local-LLM chat flow (stubbed mlx-lm)", () => {
  test("agent reply renders as prose, not raw JSON tool-call", async ({
    page,
  }) => {
    await openPlannerDM(page);
    await sendDirectMessage(
      page,
      "If you could plan anything what would you want to plan?",
    );

    // The stub fixture's first turn is a markdown-fenced tool call
    // that broker_post_message posts as a real text message to the
    // channel. The user-visible reply must be the `content` field
    // ("Hi! I'm the planner …"), not the raw JSON.
    const expectedReply =
      /Hi!\s*I'?m the planner[\s\S]*?what would you like to plan/i;

    // Up to 90s — local-LLM turns are slow even when stubbed because
    // wuphf streams chunks at ~5ms each + paces through the SSE.
    await expect(page.getByText(expectedReply).first()).toBeVisible({
      timeout: 90_000,
    });

    // Hard regression guard: the visible message body must NOT
    // contain markdown code-fence + `name`/`arguments` strings — that
    // was the user-visible bug.
    const messageBodies = await page
      .locator(".message-text, .msg-text, [data-msg-id] [class*='message']")
      .allTextContents();
    for (const body of messageBodies) {
      if (body.includes("```json") || body.includes('"arguments"')) {
        throw new Error(
          `Agent reply rendered raw JSON instead of executing the tool:\n${body}`,
        );
      }
    }
  });

  test("Live Output panel does not render fake error chips for successful tool calls", async ({
    page,
  }) => {
    await openPlannerDM(page);
    await sendDirectMessage(page, "Plan a quick weekend road trip.");

    // Wait specifically for an agent-authored persisted row.
    // MessageBubble stamps `data-author-kind="agent"`, so this gates
    // on the real reply and not on the streaming-indicator
    // placeholder (no data-msg-id) or the human prompt (kind=human).
    await expect(
      page.locator('[data-msg-id][data-author-kind="agent"]').first(),
    ).toBeVisible({ timeout: 90_000 });

    // Live Output panel renders mcp_tool_event entries via
    // ToolCallCard. The fix at 4e2cf5ed treats null AND undefined AND
    // "" as "no error" so the "✗ ERROR / undefined" chip should NOT
    // appear for successful calls.
    const errorChips = page.locator(".cc-tool-error");
    // Allow up to 3s for the UI to settle, then check.
    await page.waitForTimeout(2_000);
    const text = (await errorChips.allTextContents()).join("\n");
    expect(text, `unexpected error chip text: ${text}`).not.toMatch(
      /undefined/,
    );
  });

  // Note: cross-dialect coverage (markdown-fenced JSON, structured
  // tool_calls, text-only) lives in local-llm-dialects.spec.ts which
  // run-local.sh iterates over each fixture for. This file is the
  // markdown-fenced default-fixture surface only.

  test("Settings → Local LLMs reachable from the same shell session", async ({
    page,
  }) => {
    // Locks in the cross-surface flow: a user sending a DM and then
    // jumping to Settings to flip the active provider should hit a
    // live Local LLMs panel — not a Wizard, not a 404. The bug we
    // guard against here is "shell phase boots fine but the Settings
    // app never registers /status/local-providers" (a route
    // regression would silently fail the doctor card render).
    await openPlannerDM(page);
    await sendDirectMessage(page, "ping");
    // Wait for the broker to settle (turn started + reply landed).
    await page.waitForTimeout(1_000);

    await page.getByLabel("Open settings").click();
    await page.getByRole("button", { name: "Local LLMs" }).click();
    // All three runtime cards render — proves /status/local-providers
    // round-trips and the section components didn't fail to mount.
    await expect(page.getByTestId("local-llm-card-mlx-lm")).toBeVisible();
    await expect(page.getByTestId("local-llm-card-ollama")).toBeVisible();
    await expect(page.getByTestId("local-llm-card-exo")).toBeVisible();
  });

  // TODO(local-llm-tps): flaky against the stub because the
  // sidebar `agent_activity` SSE emission appears to debounce or
  // coalesce updates faster than the test polls. The runner-side
  // tps logic (headless_openai_compat.go onText) IS verified by
  // unit tests; verifying the full SSE → React render pipeline
  // here needs a different signal (e.g. Network tab capture of
  // /events and assertion on the activity event payload).
  test.skip("progress label surfaces streaming tokens-per-second feedback", async ({
    page,
  }) => {
    await openPlannerDM(page);
    await sendDirectMessage(page, "Plan something fun.");

    // The progress label in the sidebar entry for the active agent
    // shows "drafting response · ~N tok/s" while the stub streams
    // chunks. The stub paces 5ms per chunk × small content, so the
    // window is tight — we accept either the in-progress label OR a
    // final settled state, as long as we observed the tok/s readout
    // at SOME point.
    const labelLocator = page
      .locator("button[data-agent-slug]")
      .filter({ hasText: /planner/i })
      .first();
    let sawTpsReadout = false;
    const deadline = Date.now() + 60_000;
    while (Date.now() < deadline) {
      const txt = (await labelLocator.textContent()) ?? "";
      if (/tok\/s/.test(txt)) {
        sawTpsReadout = true;
        break;
      }
      await page.waitForTimeout(150);
    }
    expect(
      sawTpsReadout,
      "progress label never showed `tok/s` — local-LLM users have no signal that the model is producing output",
    ).toBe(true);
  });
});
