// Capture Phase 2 onboarding — Deterministic CEO conversation frames.
// Covers 6 key states: greet card, identity form field, blueprint chips,
// team checklist, seed committed, and bridge with two-chip choice.
//
// Run via the wrapper:
//   web/e2e/screenshots/publish.sh onboarding-phase2 <pr-number>
//
// Stubs /onboarding/state per phase so no real broker is needed.
// The OnboardingDMRoute is rendered inside the Shell (onboardingV2=true
// + inCeoOnboarding state). We simulate this by:
//   1. Setting onboarded=false + phase=<phase> in /onboarding/state
//   2. Setting the localStorage flag wuphf:onboarding-v2=1
//   3. Setting wuphf:in-ceo-onboarding=1 (NEW flag checked by RootRoute
//      to skip PrePickScreen and go straight to Shell+CEO DM)
//
// NOTE: Frame capture depends on the dev server running on DEFAULT_BASE.

import process from "node:process";

import {
  DEFAULT_BASE,
  installCommonMocks,
  launchBrowser,
  shotPage,
} from "./lib.mjs";

const OUT = process.env.WUPHF_SCREENSHOTS_OUT;
if (!OUT) {
  console.error("WUPHF_SCREENSHOTS_OUT is not set; run via publish.sh");
  process.exit(2);
}

// ── Phase state fixtures ───────────────────────────────────────────────────

const STATE_GREET = {
  onboarded: false,
  phase: "greet",
  form_answers: {},
  pending_suggestion: null,
};

const STATE_IDENTITY = {
  onboarded: false,
  phase: "identity",
  form_answers: {},
  pending_suggestion: {
    id: "sug-identity-name",
    phase: "identity",
    kind: "ceo_form_field",
    payload: {
      field: "company_name",
      label: "Office name?",
      optional: false,
      placeholder: "e.g. Acme Billing",
    },
  },
};

const STATE_BLUEPRINT = {
  onboarded: false,
  phase: "blueprint",
  form_answers: {
    company_name: "Acme Billing",
    description: "Subscription billing for indie SaaS.",
  },
  pending_suggestion: {
    id: "sug-blueprint",
    phase: "blueprint",
    kind: "ceo_chip_row",
    payload: {
      field: "blueprint_id",
      label: "Pick a starter template, or start from scratch:",
      options: [
        { id: "bookkeeping", label: "Bookkeeping" },
        { id: "content-ops", label: "Content Ops" },
        { id: "engineering-team", label: "Engineering Team" },
        { id: "scratch", label: "Start from scratch" },
      ],
    },
  },
};

const STATE_TEAM_CHECKLIST = {
  onboarded: false,
  phase: "team",
  form_answers: {
    company_name: "Acme Billing",
    blueprint_id: "content-ops",
  },
  pending_suggestion: {
    id: "sug-team-trim",
    phase: "team",
    kind: "ceo_team_trim",
    payload: {
      field: "picked_agents",
      label: "This blueprint comes with a team — keep or trim:",
      items: [
        { id: "writer", label: "Writer", default_checked: true },
        { id: "editor", label: "Editor", default_checked: true },
        { id: "designer", label: "Designer", default_checked: true },
      ],
      submit_label: "Confirm team",
    },
  },
};

const STATE_SEED_COMMITTED = {
  onboarded: false,
  phase: "bridge",
  form_answers: {
    company_name: "Acme Billing",
    blueprint_id: "scratch",
    scan_complete: true,
  },
  pending_suggestion: {
    id: "sug-bridge",
    phase: "bridge",
    kind: "ceo_chip_row",
    payload: {
      field: "bridge_choice",
      label: null,
      options: [
        { id: "start-issue", label: "Start an issue" },
        { id: "look-around", label: "Look around first" },
      ],
    },
  },
};

// ── Mock installer ────────────────────────────────────────────────────────

async function installPhase2Mocks(context, stateFixture) {
  await installCommonMocks(context, {
    extra: async (ctx) => {
      // Override /onboarding/state with the fixture for this phase.
      await ctx.route("**/api/onboarding/state", (r) =>
        r.fulfill({
          contentType: "application/json",
          body: JSON.stringify(stateFixture),
        }),
      );
      // Stub /onboarding/answer so card submission doesn't error.
      await ctx.route("**/api/onboarding/answer", (r) =>
        r.fulfill({
          contentType: "application/json",
          body: JSON.stringify({ ok: true }),
        }),
      );
    },
  });
}

async function bootPhase2(page) {
  // Enable both V2 flag and in-CEO-onboarding state so RootRoute renders
  // Shell+OnboardingDMRoute directly without the PrePickScreen.
  await page.addInitScript(() => {
    try {
      window.localStorage.setItem("wuphf:onboarding-v2", "1");
      // Signal to RootRoute that we're past PrePickScreen (Phase 2 active).
      // TODO(phase2-followup): Once RootRoute is wired to derive inCeoOnboarding
      // purely from /onboarding/state.phase, this localStorage seed can be removed.
      window.localStorage.setItem("wuphf:in-ceo-onboarding", "1");
    } catch {
      // private-mode tabs; the query param fallback handles it.
    }
  });
  await page.goto(`${DEFAULT_BASE}/?onboardingV2=1`, { waitUntil: "load" });
  // Wait for the onboarding route to mount.
  await page.waitForSelector('[data-testid="onboarding-dm-route"]', {
    timeout: 15_000,
  });
  await page.waitForTimeout(400);
}

// ── Capture ────────────────────────────────────────────────────────────────

const { browser, context, page } = await launchBrowser({
  viewport: { width: 1280, height: 800 },
});

// Frame 1: greet phase — empty DM, CEO status dot ready
await installPhase2Mocks(context, STATE_GREET);
await bootPhase2(page);
await shotPage(page, OUT, "01-greet-empty-dm");

// Frame 2: identity — form field card with "Office name?" prompt
await installPhase2Mocks(context, STATE_IDENTITY);
await bootPhase2(page);
await shotPage(page, OUT, "02-identity-form-field");

// Frame 3: blueprint — chip row with four options
await installPhase2Mocks(context, STATE_BLUEPRINT);
await bootPhase2(page);
await shotPage(page, OUT, "03-blueprint-chip-row");

// Frame 4: team checklist — keep or trim blueprint agents
await installPhase2Mocks(context, STATE_TEAM_CHECKLIST);
await bootPhase2(page);
await shotPage(page, OUT, "04-team-checklist");

// Frame 5: bridge — seed committed, two-chip choice (start issue / look around)
await installPhase2Mocks(context, STATE_SEED_COMMITTED);
await bootPhase2(page);
await shotPage(page, OUT, "05-bridge-two-chips");

console.log(`captured 5 screenshots to ${OUT}`);
await browser.close();
