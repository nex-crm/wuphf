// planWorkflow — the "AI understanding" behind chat-to-build authoring.
//
// FRONTEND-FIRST RULE: this is a mock of the agentic build phase. It reads the
// operator's plain-language description and compiles it into a deterministic
// workflow (an ordered list of steps), the way the real build phase will once
// gbrain + browsersniff are wired. It is keyword-driven so the prototype feels
// like it understood the request, not like it returned a canned reply. Kept
// pure (no React, no side effects) so it can carry a regression test later.

import type { WorkflowStep } from "../mock/data";

export type ClarifyField = "threshold" | "channel";

export interface ClarifyQuestion {
  field: ClarifyField;
  prompt: string; // the single sharp follow-up the AI asks
  stepId: string; // the step this answer will refine, in place
}

export interface WorkflowPlan {
  name: string; // the tool name the AI proposes
  toolId: string; // the existing mock tool this maps to on finish
  steps: WorkflowStep[];
  narration: string; // the AI's reflect-back line once steps are assembled
  clarify: ClarifyQuestion | null;
}

const TRIGGER_ID = "p-trigger";
const ENRICH_ID = "p-enrich";
const AI_ID = "p-ai";
const DECISION_ID = "p-decision";
const ACTION_ID = "p-action";
const BRANCH_ID = "p-branch";

function has(text: string, ...words: string[]): boolean {
  return words.some((w) => text.includes(w));
}

// What kind of thing kicks this off, derived from the operator's words.
function detectSubject(t: string): {
  source: string;
  name: string;
  toolId: string;
} {
  if (has(t, "ticket", "escalation", "support", "incident")) {
    return {
      source: "support ticket",
      name: "Support escalation triage",
      toolId: "support-escalations",
    };
  }
  if (has(t, "expense", "invoice", "reimburse", "spend", "purchase order")) {
    return {
      source: "expense over policy",
      name: "Expense exception routing",
      toolId: "expense-exceptions",
    };
  }
  if (has(t, "demo", "trial", "lead", "signup", "sign-up", "form", "inbound")) {
    return {
      source: "demo request",
      name: "Inbound demo-request routing",
      toolId: "inbound-routing",
    };
  }
  return {
    source: "inbound item",
    name: "Inbound routing",
    toolId: "inbound-routing",
  };
}

interface Threshold {
  kind: "score" | "amount";
  label: string; // e.g. "70" or "$5k"
}

function detectThreshold(t: string, scoreContext: boolean): Threshold | null {
  const amount = t.match(/\$\s?(\d[\d,]*\s?[km]?)/i);
  if (amount) {
    return { kind: "amount", label: `$${amount[1].replace(/\s/g, "")}` };
  }
  if (scoreContext) {
    const num = t.match(/\b(\d{2,3})\b/);
    if (num) return { kind: "score", label: num[1] };
  }
  return null;
}

/** Compile a plain-language description into a deterministic workflow. */
export function planWorkflow(description: string): WorkflowPlan {
  const t = description.toLowerCase();
  const subject = detectSubject(t);
  const steps: WorkflowStep[] = [];

  // 1. Trigger — always present. The workflow needs a thing to react to.
  steps.push({
    id: TRIGGER_ID,
    kind: "trigger",
    title: `New ${subject.source}`,
    detail: `When a ${subject.source} arrives (or fire manually on test data).`,
  });

  // 2. Enrich — look the subject up in a system of record.
  const wantsEnrich = has(
    t,
    "look up",
    "lookup",
    "look it up",
    "find",
    "enrich",
    "pull",
    "match",
    "crm",
    "company",
    "account",
    "hubspot",
    "salesforce",
  );
  if (wantsEnrich) {
    // Honor the CRM the operator named. Salesforce wins when mentioned by name;
    // otherwise the generic CRM/account/company words default to HubSpot.
    const crmIntegration = has(t, "salesforce")
      ? "Salesforce"
      : has(t, "crm", "hubspot", "account", "company")
        ? "HubSpot"
        : undefined;
    steps.push({
      id: ENRICH_ID,
      kind: "enrich",
      title: "Look up the record",
      detail: "Find the matching account and its details before deciding.",
      integration: crmIntegration,
    });
  }

  // 3. AI judgment — score for fit, or classify/triage.
  const wantsScore = has(t, "score", "rate", "qualify", "fit", "rank", "grade");
  const wantsClassify = has(
    t,
    "classif",
    "severity",
    "categor",
    "triage",
    "priorit",
    "sort",
  );
  if (wantsScore) {
    steps.push({
      id: AI_ID,
      kind: "ai",
      title: "Score fit 0 to 100",
      detail:
        "Rate fit from size, industry, and what they asked for, and explain the score.",
    });
  } else if (wantsClassify) {
    steps.push({
      id: AI_ID,
      kind: "ai",
      title: "Classify and explain",
      detail:
        "Read the item, assign a category or severity, and write the reason.",
    });
  }

  // 4. Decision — a gate the operator hinted at (or that scoring implies).
  const hasAiStep = wantsScore || wantsClassify;
  const wantsDecision =
    hasAiStep ||
    has(
      t,
      " if ",
      "above",
      "over",
      "threshold",
      "good",
      "strong",
      "hot",
      "qualified",
      "high",
      "worst",
      "urgent",
      "sev 1",
      "sev1",
    );
  const threshold = detectThreshold(t, wantsScore);
  if (wantsDecision) {
    const title = threshold
      ? threshold.kind === "amount"
        ? `Is it over ${threshold.label}?`
        : `Is the score at least ${threshold.label}?`
      : "Does it clear the bar?";
    const detail = threshold
      ? "Above the bar moves on; everything else takes the other branch."
      : "Set the cutoff that decides what moves on.";
    steps.push({ id: DECISION_ID, kind: "decision", title, detail });
  }

  // 5. Action — the external thing it does. Sends are gated on a human.
  const onSlack = has(t, "slack", "channel", "#", "post", "ping", "page");
  const onEmail = has(t, "email", "gmail", "mail");
  let actionTitle = "Send the result to you";
  let integration: string | undefined;
  if (has(t, "page")) {
    actionTitle = "Page the on-call engineer";
    integration = "Slack";
  } else if (has(t, "route", "assign", "hand off", "handoff", "send the")) {
    actionTitle = "Route to the on-call owner";
    integration = onSlack ? "Slack" : onEmail ? "Gmail" : undefined;
  } else if (has(t, "approve", "approval", "exception")) {
    actionTitle = "Route exceptions to you for approval";
  } else if (onSlack) {
    actionTitle = "Post it to the team channel";
    integration = "Slack";
  } else if (onEmail) {
    actionTitle = "Email the owner";
    integration = "Gmail";
  }
  steps.push({
    id: ACTION_ID,
    kind: "action",
    title: actionTitle,
    detail: "Include the reason so the person on the other end has context.",
    integration,
    gated: Boolean(integration),
  });

  // 6. Branch — the "otherwise" path, if implied.
  if (
    has(t, "otherwise", "else", "nurture", "queue", "ignore", "rest", "low")
  ) {
    const nurture = has(t, "nurture");
    steps.push({
      id: BRANCH_ID,
      kind: "branch",
      title: nurture ? "Otherwise: add to nurture" : "Otherwise: queue it",
      detail: nurture
        ? "Below the bar gets tagged for the nurture sequence, no handoff."
        : "Everything else waits in a queue for a person to look at.",
      integration: nurture ? "HubSpot" : undefined,
    });
  }

  // The one sharp follow-up worth asking. Prefer a missing numeric cutoff,
  // then a missing channel for a send. Only ever one, to keep it collaborative.
  let clarify: ClarifyQuestion | null = null;
  const decision = steps.find((s) => s.id === DECISION_ID);
  const action = steps.find((s) => s.id === ACTION_ID);
  if (decision && !threshold) {
    clarify = {
      field: "threshold",
      stepId: DECISION_ID,
      prompt: wantsScore
        ? "One thing to lock in: at what fit score should it move on to a person? Most teams start around 70."
        : "One thing to lock in: where should the line be between handling it automatically and routing it to a person?",
    };
  } else if (action && action.integration && !has(t, "#")) {
    clarify = {
      field: "channel",
      stepId: ACTION_ID,
      prompt:
        action.integration === "Slack"
          ? "Which Slack channel should the handoff post to?"
          : "Who should the handoff email go to?",
    };
  }

  const narration = `Here is the workflow I put together from that, ${steps.length} steps, each one scripted so it runs the same way every time.`;

  return {
    name: subject.name,
    toolId: subject.toolId,
    steps,
    narration,
    clarify,
  };
}

/** Apply the operator's answer to a clarifying question, in place. Pure. */
export function applyClarify(
  steps: WorkflowStep[],
  field: ClarifyField,
  answer: string,
): WorkflowStep[] {
  if (field === "threshold") {
    // Preserve what the step actually gates on. A dollar amount stays an
    // amount; a classify/severity flow stays severity; otherwise it is a fit
    // score. Hardcoding "Is the score at least N?" turned expense and support
    // flows into nonsense after a refine.
    const amount = answer.match(/\$\s?(\d[\d,]*\s?[km]?)/i);
    const aiStep = steps.find((s) => s.id === AI_ID);
    const isClassify = aiStep?.title.startsWith("Classify") ?? false;
    return steps.map((s) => {
      if (s.id !== DECISION_ID) return s;
      if (amount) {
        const label = `$${amount[1].replace(/\s/g, "")}`;
        return {
          ...s,
          title: `Is it over ${label}?`,
          detail: `Over ${label} routes to a person; everything else takes the other branch.`,
        };
      }
      const num = answer.match(/\d{1,3}/)?.[0] ?? "70";
      if (isClassify) {
        return {
          ...s,
          title: `Is it severity ${num} or higher?`,
          detail: `Severity ${num} and above routes to a person; everything else takes the other branch.`,
        };
      }
      return {
        ...s,
        title: `Is the score at least ${num}?`,
        detail: `At or above ${num} routes to a person; everything else takes the other branch.`,
      };
    });
  }
  // channel — a Slack send wants a #channel; an email handoff wants a recipient.
  // Forcing a "#" prefix and "Post to …" wording onto a Gmail step produced
  // nonsense like "Post to #owner@acme.com".
  const action = steps.find((s) => s.id === ACTION_ID);
  if (action?.integration === "Gmail") {
    const recipient = answer.trim();
    return steps.map((s) =>
      s.id === ACTION_ID
        ? {
            ...s,
            detail: `Email ${recipient} the result with the reason so they have context.`,
          }
        : s,
    );
  }
  const channel =
    answer.match(/#[\w-]+/)?.[0] ??
    (answer.trim().startsWith("#") ? answer.trim() : `#${answer.trim()}`);
  return steps.map((s) =>
    s.id === ACTION_ID
      ? {
          ...s,
          detail: `Post to ${channel} with the reason so the owner has context.`,
        }
      : s,
  );
}
