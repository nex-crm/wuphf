import { expect, type Page, test } from "@playwright/test";

// Cross-dialect parity sweep. run-local.sh's `local-llm-dialects`
// phase iterates over the three fixtures we ship — the markdown-
// fenced JSON tool call (the user-reported regression that started
// this whole effort), the OpenAI-native structured tool_calls path,
// and a text-only reply with no tool dispatch — restarting mlx-stub
// against each via MLX_STUB_FIXTURE before invoking this single
// spec. The fixture in play is named via DIALECT_NAME so the spec
// can pick the assertions to run.
//
// Why three fixtures + one spec instead of three specs: the wuphf →
// mlx-stub HTTP path doesn't traverse Playwright's intercept layer,
// so `page.route` can't swap responses inside a single run. The
// stub-restart pattern is the cleanest way to exercise every parser
// dialect against the production wiring.
//
// Each dialect must:
//   1. Land a real reply in chat (not a wait that's satisfied by a
//      streaming-indicator placeholder — we wait specifically for
//      `data-msg-id` chat rows from someone other than `human`).
//   2. NOT leak raw JSON / markdown code fences into the rendered
//      message body (the structural invariant every parser branch
//      must hold).
//   3. NOT trip the React error boundary (smoke test against the
//      runner blowing up on an unfamiliar dialect).

const dialect = process.env.DIALECT_NAME ?? "markdown";

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

async function openPlannerDM(page: Page) {
  await page.goto("/#/dm/planner");
  await waitForReactMount(page);
  await expect(page.getByPlaceholder(/Message #human__planner/)).toBeVisible({
    timeout: 10_000,
  });
}

async function sendDirectMessage(page: Page, body: string) {
  const composer = page.getByPlaceholder(/Message #/);
  await composer.click();
  await composer.fill(body);
  await composer.press("Enter");
  await expect(composer).toHaveValue("");
}

// agentReplyLanded waits for an actual agent-authored chat row.
// MessageBubble stamps `data-author-kind="agent"` on every persisted
// agent message so we can target it precisely — earlier specs tried
// to filter by `hasNotText: /^You$/` against a row's full textContent
// (which includes avatar label "You", author name, timestamp, AND the
// message body), so the regex was a no-op and the spec passed even
// when only the human prompt had landed (CodeRabbit caught this).
// Streaming-indicator placeholders don't carry data-msg-id either, so
// this gate is doubly precise.
async function agentReplyLanded(page: Page) {
  await expect(
    page.locator('[data-msg-id][data-author-kind="agent"]').first(),
  ).toBeVisible({ timeout: 90_000 });
}

test.describe(`Local-LLM dialect parity (${dialect})`, () => {
  test("agent reply lands as rendered prose, not raw JSON", async ({
    page,
  }) => {
    const errors: string[] = [];
    page.on("pageerror", (err) => errors.push(err.message));

    await openPlannerDM(page);
    await sendDirectMessage(
      page,
      `Hello — testing the ${dialect} dialect path.`,
    );
    await agentReplyLanded(page);

    // No React error boundary thrown by an unfamiliar dialect.
    await expect(page.getByTestId("error-boundary")).toHaveCount(0);
    expect(errors, errors.join("\n")).toEqual([]);

    // Cross-dialect structural invariant. Scan agent-authored rows
    // only — the human prompt contains the literal dialect name
    // ("structured") which would skew a substring check; pinning to
    // `data-author-kind="agent"` keeps the assertion targeted at the
    // surface the parser actually controls. We look for shapes that
    // only appear when a parser dialect failed and the raw JSON tool
    // call leaked verbatim into rendered prose.
    const messageBodies = await page
      .locator('[data-msg-id][data-author-kind="agent"]')
      .allTextContents();
    for (const body of messageBodies) {
      if (body.includes("```json")) {
        throw new Error(
          `[${dialect}] markdown code fence leaked to chat:\n${body}`,
        );
      }
      if (/"name"\s*:\s*"[^"]+"\s*,\s*"arguments"\s*:/.test(body)) {
        throw new Error(
          `[${dialect}] tool-call JSON shape leaked to chat:\n${body}`,
        );
      }
    }
  });
});
