// DemoCapture — the context a "Demo workflow to Nex" screen-share call hands to
// the AI when the call ends.
//
// The whole point of demonstrating instead of describing is that Nex can watch
// the operator's actual screen and capture FAR more than words: which apps and
// screens they touched, the concrete elements they interacted with (stable
// selectors + sample values), the API calls those screens fired (sniffed from
// the network, the discovery half of "browsersniff"), and the entities that
// matter (integrations, channels, thresholds, fields, actions). That captured
// context is what lets the AI start BUILDING or MODIFYING the tool the moment
// the call ends, instead of asking the operator to re-explain in chat.
//
// In the shipped product (operator spec S5/S6) this payload is produced by a
// computer-use agent over the captured screen plus browsersniff over the HAR.
// Here it is assembled as an honest mock: the SHAPE is the real contract; the
// values are representative of the inbound-routing scenario so the handoff can
// be seen working. Secrets never enter this payload — only endpoint shapes and
// element metadata.

import type { InternalTool } from "../mock/data";
import type { ObservedScreen } from "./observeClient";

export interface CapturedScreen {
  // Human label for the screen/app the operator demonstrated on.
  label: string;
  // Where it lives (origin or route), never with query secrets.
  url: string;
  // What was observed about the DOM (framework, surface kind).
  dom: string;
}

export interface CapturedSelector {
  // What the element is, in the operator's terms ("Company field").
  label: string;
  // Coarse role so the AI knows how to drive it.
  role: "input" | "button" | "link" | "table" | "select" | "form";
  // A stable selector observed on the page (data-*, aria, semantic).
  selector: string;
  // A sample value seen in the element, if any (never a secret).
  sample?: string;
}

export interface CapturedApiCall {
  method: "GET" | "POST" | "PUT" | "PATCH" | "DELETE";
  // The endpoint shape sniffed from the network (no tokens, no query secrets).
  endpoint: string;
  // The integration the call maps to, when recognized.
  integration: string;
  // What the call does, in plain terms.
  purpose: string;
}

export interface CapturedEntity {
  kind: "integration" | "channel" | "threshold" | "field" | "action";
  value: string;
}

export interface DemoCaptureLine {
  who: "you" | "ai";
  text: string;
}

export interface DemoCapture {
  mode: "build" | "modify";
  // Present in modify mode: the tool the change was demonstrated on.
  toolId?: string;
  toolName?: string;
  // The narrated goal, as a clean instruction the build engine can plan from.
  goal: string;
  // The AI's reflect-back at the end of the call (the drafted tool or change).
  summary: string;
  // What the operator said, verbatim.
  transcript: DemoCaptureLine[];
  // Everything Nex observed on the demonstrated screens.
  screens: CapturedScreen[];
  selectors: CapturedSelector[];
  apiCalls: CapturedApiCall[];
  entities: CapturedEntity[];
  // Ground truth read off the real page by cua-driver during the call (the AX
  // component tree + visible text per distinct screen, in workflow order). This
  // is what lets the build read the actual components, not guess from pixels.
  observed?: ObservedScreen[];
}

// Representative observation set for the inbound demo-request scenario. This is
// the mock stand-in for what a real capture (CUA + browsersniff) would surface.
const INBOUND_SCREENS: CapturedScreen[] = [
  {
    label: "Inbound demo-request form",
    url: "forms.yourco.com/demo-request",
    dom: "static form, 4 fields",
  },
  {
    label: "HubSpot — company record",
    url: "app.hubspot.com/contacts",
    dom: "React app (hs-*)",
  },
  {
    label: "Slack — #ae-handoffs",
    url: "app.slack.com/client",
    dom: "React app, channel view",
  },
];

const INBOUND_SELECTORS: CapturedSelector[] = [
  {
    label: "Company field",
    role: "input",
    selector: 'form[name="demo-request"] input[name="company"]',
    sample: "Acme Robotics",
  },
  {
    label: "Work email field",
    role: "input",
    selector: 'form[name="demo-request"] input[name="email"]',
    sample: "dana@acmerobotics.com",
  },
  {
    label: "Company search box (HubSpot)",
    role: "input",
    selector: 'input[data-test-id="company-search"]',
  },
  {
    label: "Headcount property (HubSpot)",
    role: "table",
    selector: '[data-selenium-test="property-numberofemployees"]',
    sample: "200+",
  },
  {
    label: "#ae-handoffs channel (Slack)",
    role: "link",
    selector: '[data-qa="channel_sidebar_name_ae-handoffs"]',
  },
  {
    label: "Message composer (Slack)",
    role: "input",
    selector: '[data-qa="message_input"] [contenteditable="true"]',
  },
];

const INBOUND_API_CALLS: CapturedApiCall[] = [
  {
    method: "GET",
    endpoint: "/crm/v3/objects/companies/search",
    integration: "HubSpot",
    purpose: "Look up the company by domain and read firmographics",
  },
  {
    method: "POST",
    endpoint: "/api/chat.postMessage",
    integration: "Slack",
    purpose: "Post the lead + score + reason to #ae-handoffs",
  },
];

const INBOUND_ENTITIES: CapturedEntity[] = [
  { kind: "integration", value: "HubSpot" },
  { kind: "integration", value: "Slack" },
  { kind: "channel", value: "#ae-handoffs" },
  { kind: "threshold", value: "fit ≥ 70 routes to an AE" },
  { kind: "field", value: "company size" },
  { kind: "field", value: "industry" },
  { kind: "action", value: "nurture the rest" },
];

const BUILD_GOAL =
  "When a demo request comes in, look up the company in HubSpot, score fit 0 " +
  "to 100 from company size and industry, route 70 and up to an AE in Slack " +
  "#ae-handoffs with the reason, and nurture the rest.";

// Assemble the capture from the call. mode + the tool (modify) + the narrated
// transcript drive it; the observation sets are representative of the scenario.
export function assembleDemoCapture(args: {
  mode: "build" | "modify";
  tool?: { id: string; name: string } | InternalTool;
  transcript: DemoCaptureLine[];
}): DemoCapture {
  const { mode, tool, transcript } = args;
  const summary = transcript.filter((l) => l.who === "ai").at(-1)?.text ?? "";

  if (mode === "modify" && tool) {
    // A modify call is scoped to one screen of the existing tool and surfaces
    // the specific branch the operator changed.
    return {
      mode,
      toolId: tool.id,
      toolName: tool.name,
      goal:
        "When a lead scores below 40, archive it instead of adding it to the " +
        "nurture sequence. Leave every other branch unchanged.",
      summary,
      transcript,
      screens: [
        {
          label: `${tool.name} — Workflow screen`,
          url: "wuphf/operator",
          dom: "the tool's deterministic step graph",
        },
      ],
      selectors: [
        {
          label: "Score decision step (IF ≥ 70)",
          role: "button",
          selector: '[data-step-kind="decision"]',
        },
        {
          label: "Nurture branch step (EL)",
          role: "button",
          selector: '[data-step-kind="branch"]',
        },
      ],
      apiCalls: [],
      entities: [
        { kind: "threshold", value: "scores under 40" },
        { kind: "action", value: "archive instead of nurture" },
      ],
    };
  }

  return {
    mode: "build",
    goal: BUILD_GOAL,
    summary,
    transcript,
    screens: INBOUND_SCREENS,
    selectors: INBOUND_SELECTORS,
    apiCalls: INBOUND_API_CALLS,
    entities: INBOUND_ENTITIES,
  };
}

// Turn the capture into the seed the build engine starts from. The narrated goal
// leads (so a keyword planner reads it cleanly), then EVERYTHING the demo call
// captured follows — the AI's reflect-back, the observed apps/APIs, screens,
// elements, key details, and the FULL transcript — so the build works from the
// real session, not a one-line summary.
export function capturePromptSeed(capture: DemoCapture): string {
  const lines: string[] = [capture.goal];

  if (capture.summary) {
    lines.push("", `What Nex drafted on the call: ${capture.summary}`);
  }

  const details: string[] = [];
  if (capture.entities.length > 0) {
    details.push(
      `Key details: ${capture.entities
        .map((e) => `${e.value} (${e.kind})`)
        .join(", ")}`,
    );
  }
  if (capture.apiCalls.length > 0) {
    details.push(
      `Integrations & APIs observed: ${capture.apiCalls
        .map(
          (c) =>
            `${c.integration} ${c.method} ${c.endpoint}${
              c.purpose ? ` — ${c.purpose}` : ""
            }`,
        )
        .join("; ")}`,
    );
  }
  if (capture.screens.length > 0) {
    details.push(
      `Screens used: ${capture.screens
        .map((s) => (s.url ? `${s.label} (${s.url})` : s.label))
        .join(", ")}`,
    );
  }
  if (capture.selectors.length > 0) {
    details.push(
      `Elements interacted with: ${capture.selectors
        .map((s) => (s.sample ? `${s.label} = ${s.sample}` : s.label))
        .join(", ")}`,
    );
  }
  if (details.length > 0) {
    lines.push("", "Captured from the screen share:", ...details);
  }

  if (capture.observed && capture.observed.length > 0) {
    lines.push(
      "",
      "Real page structure Nex read off the screen during the call (ground " +
        "truth — the apps, their components, and content in the order shown):",
    );
    for (const screen of capture.observed) {
      lines.push(`- ${screen.app} — ${screen.title}`);
      if (screen.components.length > 0) {
        lines.push(
          `  components: ${screen.components
            .map((c) => `${c.role}:${c.label}`)
            .join(", ")}`,
        );
      }
      if (screen.text) {
        lines.push(`  text: ${screen.text}`);
      }
    }
  }

  if (capture.transcript.length > 0) {
    lines.push("", "Full transcript of the demo call:");
    for (const line of capture.transcript) {
      lines.push(`${line.who === "ai" ? "Nex" : "Operator"}: ${line.text}`);
    }
  }

  return lines.join("\n");
}

// The argument shape the realtime model fills in via its draft_workflow tool.
// It mirrors DemoCapture's observation fields but every list is optional, since
// a live model may surface only some of them. Roles/kinds arrive as free strings
// and are coerced into the typed unions on the way in.
export interface DraftWorkflowArgs {
  goal: string;
  summary?: string;
  screens?: Array<{ label?: string; url?: string; dom?: string }>;
  selectors?: Array<{
    label?: string;
    role?: string;
    selector?: string;
    sample?: string;
  }>;
  apiCalls?: Array<{
    method?: string;
    endpoint?: string;
    integration?: string;
    purpose?: string;
  }>;
  entities?: Array<{ kind?: string; value?: string }>;
}

const SELECTOR_ROLES: ReadonlySet<CapturedSelector["role"]> = new Set([
  "input",
  "button",
  "link",
  "table",
  "select",
  "form",
]);
const API_METHODS: ReadonlySet<CapturedApiCall["method"]> = new Set([
  "GET",
  "POST",
  "PUT",
  "PATCH",
  "DELETE",
]);
const ENTITY_KINDS: ReadonlySet<CapturedEntity["kind"]> = new Set([
  "integration",
  "channel",
  "threshold",
  "field",
  "action",
]);

// Strip query strings and fragments from a model-reported URL/endpoint so
// secrets carried in the query (e.g. a shared-screen `...?token=...`) never
// cross the handoff boundary. Enforces the "no query secrets" contract.
function stripQueryAndFragment(value: string): string {
  const trimmed = value.trim();
  const cut = trimmed.search(/[?#]/);
  return cut >= 0 ? trimmed.slice(0, cut) : trimmed;
}

// Build a DemoCapture from what the realtime model reported, coercing loose
// strings into the typed unions so the rest of the handoff is unchanged. This is
// the REAL-call counterpart to assembleDemoCapture (the mock).
export function demoCaptureFromDraft(
  args: DraftWorkflowArgs,
  opts: {
    mode: "build" | "modify";
    tool?: { id: string; name: string };
    transcript: DemoCaptureLine[];
    observed?: ObservedScreen[];
  },
): DemoCapture {
  return {
    mode: opts.mode,
    toolId: opts.tool?.id,
    toolName: opts.tool?.name,
    goal: (args.goal ?? "").trim(),
    summary: (args.summary ?? "").trim(),
    transcript: opts.transcript,
    ...(opts.observed && opts.observed.length > 0
      ? { observed: opts.observed }
      : {}),
    screens: (args.screens ?? [])
      .filter((s) => (s.label ?? "").trim())
      .map((s) => ({
        label: (s.label ?? "").trim(),
        url: stripQueryAndFragment(s.url ?? ""),
        dom: (s.dom ?? "").trim(),
      })),
    selectors: (args.selectors ?? [])
      .filter((s) => (s.selector ?? "").trim())
      .map((s) => {
        const role = (s.role ?? "").toLowerCase() as CapturedSelector["role"];
        return {
          label: (s.label ?? "element").trim(),
          role: SELECTOR_ROLES.has(role) ? role : "input",
          selector: (s.selector ?? "").trim(),
          ...(s.sample ? { sample: s.sample.trim() } : {}),
        };
      }),
    apiCalls: (args.apiCalls ?? [])
      .filter((c) => (c.endpoint ?? "").trim())
      .map((c) => {
        const method = (
          c.method ?? "GET"
        ).toUpperCase() as CapturedApiCall["method"];
        return {
          method: API_METHODS.has(method) ? method : "GET",
          endpoint: stripQueryAndFragment(c.endpoint ?? ""),
          integration: (c.integration ?? "").trim(),
          purpose: (c.purpose ?? "").trim(),
        };
      }),
    entities: (args.entities ?? [])
      .filter((e) => (e.value ?? "").trim())
      .map((e) => {
        const kind = (e.kind ?? "").toLowerCase() as CapturedEntity["kind"];
        return {
          kind: ENTITY_KINDS.has(kind) ? kind : "field",
          value: (e.value ?? "").trim(),
        };
      }),
  };
}

// Small accessor for UI summaries: how much context the call captured.
export function captureCounts(capture: DemoCapture): {
  screens: number;
  selectors: number;
  apiCalls: number;
  entities: number;
} {
  return {
    screens: capture.screens.length,
    selectors: capture.selectors.length,
    apiCalls: capture.apiCalls.length,
    entities: capture.entities.length,
  };
}
