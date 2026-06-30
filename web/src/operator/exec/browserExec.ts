// browserExec.ts — the contract + mock for the EXECUTE half: running an app's
// workflow by driving the operator's real browser (the computer-use loop). The
// action vocabulary mirrors OpenAI computer-use-preview so the real backend (E2)
// maps 1:1. Phase 1 is a mock runner so the surface is real and clickable before
// the backend exists. See docs/specs/operator-browser-execution.md.

import type { WorkflowStep } from "../mock/data";

// One primitive action the computer-use loop takes on the page. `navigate` and
// `done` are our higher-level wrappers; the rest mirror computer-use-preview.
export type ExecActionKind =
  | "navigate"
  | "click"
  | "type"
  | "keypress"
  | "scroll"
  | "read"
  | "wait"
  | "done";

export interface ExecAction {
  kind: ExecActionKind;
  // Human label of the target ("the company search box", "#ae-handoffs").
  target?: string;
  // Typed text, URL, key combo, or what was read.
  value?: string;
  // The model's one-line reason for this action — keeps the run auditable.
  reasoning?: string;
  // The screen this acted on. Mock: a human label; real: a screenshot data URL.
  screen?: string;
  // True when this action sends something external (Slack/email/CRM). It must
  // pass the human approval gate before it runs — execution does not bypass it.
  gated?: boolean;
}

export type ExecStatus =
  | "idle"
  | "running"
  | "paused"
  | "needs-you"
  | "done"
  | "error";

export interface ExecStep {
  id: string;
  kind: WorkflowStep["kind"];
  title: string;
  // API-first steps run a Composio call directly instead of browser actions.
  apiCall?: string;
  actions: ExecAction[];
}

export interface ExecSession {
  goal: string;
  status: ExecStatus;
  steps: ExecStep[];
  result?: string;
}

// One action flattened with its step context, for the run timeline.
export interface FlatAction {
  stepIndex: number;
  stepTitle: string;
  stepKind: WorkflowStep["kind"];
  action: ExecAction;
}

// Build a realistic mock run of the inbound demo-request routing app — the same
// scenario as the demo call, so observe → build → execute reads as one story.
// The real computer-use loop (E2) replaces this.
export function buildMockRun(opts: {
  goal: string;
  toolName?: string;
}): ExecStep[] {
  return [
    {
      id: "trigger",
      kind: "trigger",
      title: "New demo request received",
      actions: [
        {
          kind: "read",
          value: "Demo request from Acme Robotics (dana@acmerobotics.com).",
          reasoning: "A new request kicked off the run.",
          screen: "Demo request form",
        },
      ],
    },
    {
      id: "enrich",
      kind: "enrich",
      title: "Find the company in HubSpot",
      actions: [
        {
          kind: "navigate",
          value: "app.hubspot.com/contacts",
          reasoning: "Open HubSpot to look up the company.",
          screen: "HubSpot",
        },
        {
          kind: "click",
          target: "the company search box",
          reasoning: "Focus the search to find Acme Robotics.",
          screen: "HubSpot",
        },
        {
          kind: "type",
          target: "company search",
          value: "Acme Robotics",
          screen: "HubSpot",
        },
        { kind: "keypress", value: "Enter", screen: "HubSpot" },
        {
          kind: "read",
          value: "200+ employees · Robotics · named a use case.",
          reasoning: "Read the firmographics from the company record.",
          screen: "HubSpot · Acme Robotics",
        },
      ],
    },
    {
      id: "score",
      kind: "ai",
      title: "Score fit 0 to 100",
      actions: [
        {
          kind: "wait",
          reasoning: "Score fit from size + industry + use case.",
          value: "Fit 92 / 100",
        },
      ],
    },
    {
      id: "decide",
      kind: "decision",
      title: "Check the fit threshold",
      actions: [
        {
          kind: "read",
          value: "92 ≥ 70 — route to an AE.",
          reasoning: "Above threshold, so this is a hot lead.",
        },
      ],
    },
    {
      id: "route",
      kind: "action",
      title: "Notify the AE in Slack #ae-handoffs",
      actions: [
        {
          kind: "navigate",
          value: "app.slack.com",
          reasoning: "Open Slack to hand the lead off.",
          screen: "Slack",
        },
        {
          kind: "click",
          target: "the #ae-handoffs channel",
          screen: "Slack · #ae-handoffs",
        },
        {
          kind: "type",
          target: "the message composer",
          value:
            "New hot lead: Acme Robotics — fit 92. 200+ in robotics, named a use case.",
          screen: "Slack · #ae-handoffs",
        },
        {
          kind: "click",
          target: "Send",
          reasoning: "Post the hand-off message.",
          screen: "Slack · #ae-handoffs",
          gated: true,
        },
      ],
    },
    {
      id: "nurture",
      kind: "branch",
      title: "Nurture the rest",
      actions: [
        {
          kind: "read",
          value: "Lower-fit leads were added to the nurture sequence.",
          reasoning: "Everything below 70 takes the nurture branch.",
        },
        { kind: "done", value: "Routed Acme Robotics to an AE. Run complete." },
      ],
    },
  ];
}

// Flatten steps into the ordered action timeline the run reveals.
export function flattenRun(steps: ExecStep[]): FlatAction[] {
  const flat: FlatAction[] = [];
  steps.forEach((step, stepIndex) => {
    for (const action of step.actions) {
      flat.push({
        stepIndex,
        stepTitle: step.title,
        stepKind: step.kind,
        action,
      });
    }
  });
  return flat;
}

// A short human verb for an action, for the timeline ("Clicked", "Typed").
export function actionVerb(action: ExecAction): string {
  switch (action.kind) {
    case "navigate":
      return "Opened";
    case "click":
      return "Clicked";
    case "type":
      return "Typed";
    case "keypress":
      return "Pressed";
    case "scroll":
      return "Scrolled";
    case "read":
      return "Read";
    case "wait":
      return "Worked out";
    case "done":
      return "Done";
  }
}

// The human one-liner for an action in the timeline.
export function actionLabel(action: ExecAction): string {
  const verb = actionVerb(action);
  if (action.kind === "navigate") return `${verb} ${action.value ?? ""}`.trim();
  if (action.kind === "type")
    return `${verb} “${action.value ?? ""}”${
      action.target ? ` into ${action.target}` : ""
    }`;
  if (action.kind === "keypress") return `${verb} ${action.value ?? ""}`.trim();
  if (action.kind === "click")
    return `${verb} ${action.target ?? "the page"}`.trim();
  if (
    action.kind === "read" ||
    action.kind === "wait" ||
    action.kind === "done"
  )
    return `${verb}: ${action.value ?? ""}`.trim();
  return verb;
}
