// Mock data for the operator-MLP frontend prototype.
//
// FRONTEND-FIRST RULE: this slice ships the operator shell with mock data only,
// so the product shape can be seen and clicked before any backend exists. None
// of this is wired to the broker. Everything here is a fixture centered on the
// "Maya, RevOps — inbound demo-request routing" acceptance scenario from
// docs/specs/operator-mlp-plan.md. When the shape is validated, real data
// models replace these fixtures.

export type ToolStatus = "enabled" | "disabled" | "draft" | "suggested";

export type RequestStatus =
  | "new"
  | "scored"
  | "routed"
  | "nurturing"
  | "needs-you";

export interface InboundRequest {
  id: string;
  company: string;
  contact: string;
  email: string;
  source: string;
  receivedAt: string; // human label, e.g. "9:02am"
  fitScore: number | null; // 0-100, null = not yet scored
  status: RequestStatus;
  reason: string; // AI rationale for the score / routing
  routedTo?: string; // AE name when routed
}

export type WorkflowStepKind =
  | "trigger"
  | "enrich"
  | "ai"
  | "decision"
  | "action"
  | "branch";

export interface WorkflowStep {
  id: string;
  kind: WorkflowStepKind;
  title: string;
  detail: string;
  integration?: string; // e.g. "HubSpot", "Slack"
  gated?: boolean; // external mutation → human approval card
}

export interface WorkflowRun {
  id: string;
  ranAt: string;
  trigger: string;
  outcome: "routed" | "nurtured" | "needs-you";
  durationMs: number;
  summary: string;
}

export interface ToolVersion {
  version: number;
  label: string;
  at: string;
  author: "you" | "your AI";
  note: string;
}

export interface DataColumn {
  key: string;
  label: string;
  type: "text" | "email" | "number" | "status" | "date";
}

export interface InternalTool {
  id: string;
  name: string;
  emoji: string;
  status: ToolStatus;
  summary: string;
  version: number;
  lastRun: string;
  runsToday: number;
  builtFrom: "call" | "chat" | "detected";
  steps: WorkflowStep[];
  runs: WorkflowRun[];
  versions: ToolVersion[];
  dataColumns: DataColumn[];
}

// ── The hero tool: Maya's inbound-routing internal tool ─────────────────

export const INBOUND_REQUESTS: InboundRequest[] = [
  {
    id: "req-1",
    company: "Acme Robotics",
    contact: "Dana Whitfield",
    email: "dana@acmerobotics.com",
    source: "Demo form",
    receivedAt: "9:02am",
    fitScore: 92,
    status: "routed",
    reason:
      "500+ employees, manufacturing, asked about API access. Strong ICP fit.",
    routedTo: "Priya (AE)",
  },
  {
    id: "req-2",
    company: "Northwind Health",
    contact: "Sam Ortega",
    email: "s.ortega@northwindhealth.org",
    source: "Demo form",
    receivedAt: "9:14am",
    fitScore: 88,
    status: "routed",
    reason: "Mid-market healthcare, named budget, timeline this quarter.",
    routedTo: "Marco (AE)",
  },
  {
    id: "req-3",
    company: "Bright Tutors",
    contact: "Lena Park",
    email: "lena@brighttutors.co",
    source: "Pricing page",
    receivedAt: "9:31am",
    fitScore: 41,
    status: "nurturing",
    reason:
      "Solo founder, 2 employees. Below size threshold, added to nurture.",
  },
  {
    id: "req-4",
    company: "Vela Logistics",
    contact: "Owen Drake",
    email: "owen@velalogistics.com",
    source: "Demo form",
    receivedAt: "9:48am",
    fitScore: 76,
    status: "routed",
    reason: "Scored 76, matched in CRM, routed to the on-call AE.",
    routedTo: "Marco (AE)",
  },
  {
    id: "req-5",
    company: "(unknown)",
    contact: "h.tanaka",
    email: "h.tanaka@gmail.com",
    source: "Demo form",
    receivedAt: "10:05am",
    fitScore: null,
    status: "needs-you",
    reason: "Free email, no company in CRM. Could not score — needs your call.",
  },
  {
    id: "req-6",
    company: "Cobalt Finance",
    contact: "Reese Aldridge",
    email: "reese@cobaltfinance.com",
    source: "Demo form",
    receivedAt: "10:22am",
    fitScore: 84,
    status: "routed",
    reason: "Fintech, 300 employees, compliance use case. Routed to an AE.",
    routedTo: "Priya (AE)",
  },
];

const INBOUND_TOOL: InternalTool = {
  id: "inbound-routing",
  name: "Inbound demo-request routing",
  emoji: "🎯",
  status: "enabled",
  summary:
    "Scores every inbound demo request for fit, routes hot ones to an AE in Slack, nurtures the rest, and flags anything it cannot place.",
  version: 4,
  lastRun: "10:22am today",
  runsToday: 14,
  builtFrom: "call",
  steps: [
    {
      id: "s1",
      kind: "trigger",
      title: "New inbound request",
      detail: "When a demo form is submitted (or fire manually on test data).",
    },
    {
      id: "s2",
      kind: "enrich",
      title: "Look up the company",
      detail: "Find the account + firmographics for the request's domain.",
      integration: "HubSpot",
    },
    {
      id: "s3",
      kind: "ai",
      title: "Score fit 0 to 100",
      detail:
        "Rate fit from company size, industry, and what they asked for. Explain the score.",
    },
    {
      id: "s4",
      kind: "decision",
      title: "Is the score at least 70?",
      detail: "Hot leads route to an AE; everything else goes to nurture.",
    },
    {
      id: "s5",
      kind: "action",
      title: "Route to the on-call AE",
      detail:
        "Post the lead + score + reason to #ae-handoffs and assign an AE.",
      integration: "Slack",
      gated: true,
    },
    {
      id: "s6",
      kind: "branch",
      title: "Otherwise: add to nurture",
      detail: "Tag the contact for the nurture sequence; no AE handoff.",
      integration: "HubSpot",
    },
  ],
  runs: [
    {
      id: "run-1",
      ranAt: "10:22am",
      trigger: "Cobalt Finance — demo form",
      outcome: "routed",
      durationMs: 2400,
      summary: "Scored 84, routed to Priya (AE).",
    },
    {
      id: "run-2",
      ranAt: "10:05am",
      trigger: "h.tanaka@gmail.com — demo form",
      outcome: "needs-you",
      durationMs: 1900,
      summary: "No company match in CRM, flagged for you.",
    },
    {
      id: "run-3",
      ranAt: "9:48am",
      trigger: "Vela Logistics — demo form",
      outcome: "routed",
      durationMs: 2600,
      summary: "Scored 76, routed to Marco (AE).",
    },
    {
      id: "run-4",
      ranAt: "9:31am",
      trigger: "Bright Tutors — pricing page",
      outcome: "nurtured",
      durationMs: 1700,
      summary: "Scored 41, below threshold, added to nurture.",
    },
  ],
  versions: [
    {
      version: 4,
      label: "Threshold lowered to 70",
      at: "Yesterday, 4:12pm",
      author: "you",
      note: '"Lower the threshold to 70. We were missing good mid-market leads."',
    },
    {
      version: 3,
      label: "Added the CRM-miss flag",
      at: "Yesterday, 11:40am",
      author: "your AI",
      note: "Now flags requests with no company match instead of guessing.",
    },
    {
      version: 2,
      label: "Route reason included in Slack",
      at: "2 days ago",
      author: "you",
      note: '"Put the why in the Slack message so the AE has context."',
    },
    {
      version: 1,
      label: "Built on a call",
      at: "2 days ago",
      author: "your AI",
      note: "Captured from your screen-share walkthrough of inbound triage.",
    },
  ],
  dataColumns: [
    { key: "company", label: "Company", type: "text" },
    { key: "contact", label: "Contact", type: "text" },
    { key: "email", label: "Email", type: "email" },
    { key: "source", label: "Source", type: "text" },
    { key: "fitScore", label: "Fit score", type: "number" },
    { key: "status", label: "Status", type: "status" },
    { key: "receivedAt", label: "Received", type: "date" },
  ],
};

const ESCALATION_TOOL: InternalTool = {
  id: "support-escalations",
  name: "Support escalation triage",
  emoji: "🛟",
  status: "draft",
  summary:
    "Reads incoming support escalations, tags severity, and drafts the routing to the right on-call engineer. Built from chat, not yet published.",
  version: 1,
  lastRun: "not run yet",
  runsToday: 0,
  builtFrom: "chat",
  steps: [
    {
      id: "e1",
      kind: "trigger",
      title: "New escalation",
      detail: "When a ticket is tagged 'escalation' in the help desk.",
    },
    {
      id: "e2",
      kind: "ai",
      title: "Classify severity",
      detail: "Rate sev 1 to 3 from the ticket text and account tier.",
    },
    {
      id: "e3",
      kind: "action",
      title: "Page the on-call engineer",
      detail: "Sev 1 pages the on-call engineer; sev 2 and 3 go to the queue.",
      integration: "Slack",
      gated: true,
    },
  ],
  runs: [],
  versions: [
    {
      version: 1,
      label: "Drafted from chat",
      at: "1 hour ago",
      author: "your AI",
      note: "Draft. Run it on test data before publishing.",
    },
  ],
  dataColumns: [
    { key: "ticket", label: "Ticket", type: "text" },
    { key: "account", label: "Account", type: "text" },
    { key: "severity", label: "Severity", type: "number" },
    { key: "status", label: "Status", type: "status" },
  ],
};

const EXPENSE_TOOL: InternalTool = {
  id: "expense-exceptions",
  name: "Expense exception routing",
  emoji: "🧾",
  status: "suggested",
  summary:
    "Your AI noticed you approve the same expense exceptions every Friday. Want a tool that pre-sorts them and routes only the edge cases to you?",
  version: 0,
  lastRun: "not run yet",
  runsToday: 0,
  builtFrom: "detected",
  steps: [
    {
      id: "x1",
      kind: "trigger",
      title: "New expense over policy",
      detail: "When an expense exceeds the category limit.",
    },
    {
      id: "x2",
      kind: "ai",
      title: "Check against policy",
      detail: "Auto-approve within tolerance; route the rest to you.",
    },
  ],
  runs: [],
  versions: [],
  dataColumns: [
    { key: "employee", label: "Employee", type: "text" },
    { key: "amount", label: "Amount", type: "number" },
    { key: "status", label: "Status", type: "status" },
  ],
};

export const TOOLS: InternalTool[] = [
  INBOUND_TOOL,
  ESCALATION_TOOL,
  EXPENSE_TOOL,
];

export function getTool(id: string): InternalTool | undefined {
  return TOOLS.find((t) => t.id === id);
}

// ── Chats ───────────────────────────────────────────────────────────────

export interface ChatMessage {
  id: string;
  from: "you" | "ai";
  body: string;
  at: string;
}

export interface ChatThread {
  id: string;
  title: string;
  subtitle: string;
  updatedAt: string;
  unread?: boolean;
  messages: ChatMessage[];
}

export const CHATS: ChatThread[] = [
  {
    id: "chat-inbound",
    title: "Inbound routing",
    subtitle: "Tuning the fit threshold",
    updatedAt: "10:24am",
    unread: true,
    messages: [
      {
        id: "m1",
        from: "ai",
        body: "Morning digest: 14 requests processed, 5 routed hot, 1 needs you. I couldn't find “h.tanaka@gmail.com” in the CRM.",
        at: "9:00am",
      },
      {
        id: "m2",
        from: "you",
        body: "For free-email signups with no company, just nurture them instead of flagging me.",
        at: "10:20am",
      },
      {
        id: "m3",
        from: "ai",
        body: "Got it. I'll add a rule: no company match + free email goes to nurture, no flag. That's a small change. Want me to apply it and bump the tool to v5?",
        at: "10:21am",
      },
      {
        id: "m4",
        from: "you",
        body: "Yes, do it.",
        at: "10:24am",
      },
    ],
  },
  {
    id: "chat-escalations",
    title: "Support escalations",
    subtitle: "Building the triage tool",
    updatedAt: "1 hour ago",
    messages: [
      {
        id: "m5",
        from: "you",
        body: "I want to auto-triage support escalations by severity and page the on-call engineer for sev 1s.",
        at: "11:02am",
      },
      {
        id: "m6",
        from: "ai",
        body: "I drafted “Support escalation triage”: trigger on escalation tag, classify sev 1 to 3, page on sev 1. It's a draft, so run it on a few real tickets before we publish. Open it?",
        at: "11:03am",
      },
    ],
  },
];

export function getChat(id: string): ChatThread | undefined {
  return CHATS.find((c) => c.id === id);
}

// ── Integrations ────────────────────────────────────────────────────────

export type IntegrationStatus = "connected" | "available" | "needs-attention";

export interface Integration {
  id: string;
  name: string;
  emoji: string;
  category: string;
  status: IntegrationStatus;
  detail: string;
}

export const INTEGRATIONS: Integration[] = [
  {
    id: "hubspot",
    name: "HubSpot",
    emoji: "🟠",
    category: "CRM",
    status: "connected",
    detail: "Reading accounts & contacts. Used by Inbound routing.",
  },
  {
    id: "slack",
    name: "Slack",
    emoji: "💬",
    category: "Messaging",
    status: "connected",
    detail: "Posting AE handoffs to #ae-handoffs. Used by Inbound routing.",
  },
  {
    id: "gmail",
    name: "Gmail",
    emoji: "✉️",
    category: "Email",
    status: "connected",
    detail: "Sending your daily digest.",
  },
  {
    id: "salesforce",
    name: "Salesforce",
    emoji: "☁️",
    category: "CRM",
    status: "available",
    detail: "Connect to route from Salesforce leads instead of HubSpot.",
  },
  {
    id: "zendesk",
    name: "Zendesk",
    emoji: "🎫",
    category: "Support",
    status: "available",
    detail: "Connect to power the support-escalation tool.",
  },
  {
    id: "stripe",
    name: "Stripe",
    emoji: "💳",
    category: "Finance",
    status: "needs-attention",
    detail: "Token expired. Reconnect to keep expense routing working.",
  },
];

// ── Knowledge (gbrain-backed, wikipedia-style reader) ────────────────────

// Wikipedia-style: every claim that came from somewhere in the company brain
// carries a citation. `[[n]]` markers in prose resolve to the references list.
export type KnowledgeSourceKind =
  | "chat"
  | "document"
  | "crm"
  | "decision"
  | "roster";

export interface KnowledgeRef {
  n: number;
  title: string; // the source in the brain
  detail: string; // where / when
  kind: KnowledgeSourceKind; // what kind of source this is
  snippet: string; // the actual excerpt the fact was drawn from
  why: string; // why the brain chose this source for the fact
}

export interface KnowledgeSection {
  heading?: string;
  paras: string[]; // may contain [[n]] citation markers
}

export interface KnowledgePage {
  id: string;
  title: string;
  category: string;
  updatedAt: string;
  summary: string;
  infobox: { label: string; value: string }[];
  lead: string; // intro paragraph, may contain [[n]]
  sections: KnowledgeSection[];
  references: KnowledgeRef[];
  categories: string[];
  seeAlso: string[]; // page ids
  /** Other apps this page also belongs to (cross-app share). */
  alsoIn?: { appId: string; name: string }[];
}

export const KNOWLEDGE: KnowledgePage[] = [
  {
    id: "icp",
    title: "Ideal customer profile",
    category: "Sales",
    updatedAt: "Last edited yesterday by your AI",
    summary:
      "The company most likely to buy: mid-market and up, in a core industry, with a named use case.",
    infobox: [
      { label: "Size floor", value: "200 employees" },
      {
        label: "Core industries",
        value: "Manufacturing, Healthcare, Fintech, Logistics",
      },
      { label: "Fit threshold", value: "70 / 100" },
      { label: "Owner", value: "Maya (RevOps)" },
    ],
    lead: "The ideal customer profile (ICP) is the shape of company most likely to become a customer. It is the basis on which the inbound-routing tool scores every demo request and decides whether to route it to an account executive.[[1]]",
    sections: [
      {
        heading: "Definition",
        paras: [
          "An ideal customer is a company of 200 or more employees in manufacturing, healthcare, fintech, or logistics, evaluating the product for a named, time-bound use case.[[1]] The size floor was raised from 50 to 200 after mid-market deals closed at roughly three times the rate of small business.[[2]]",
        ],
      },
      {
        heading: "Signals",
        paras: [
          "Strong signals include API or integration questions, a stated budget, a buying timeline within the quarter, and a work email on a company domain.[[3]]",
          "Weak signals include solo founders, free email addresses, fewer than 10 employees, or “just looking” with no use case. These are nurtured rather than routed to an account executive.[[3]]",
        ],
      },
      {
        heading: "Use in routing",
        paras: [
          "The inbound-routing tool encodes this profile as a 0 to 100 fit score; anything at or above 70 routes to an account executive.[[4]] The threshold was lowered from 80 to 70 in June 2026 to catch good mid-market leads that were being missed.[[2]]",
        ],
      },
    ],
    references: [
      {
        n: 1,
        title: "RevOps onboarding deck",
        detail: "slide 4, “Who we sell to”",
        kind: "document",
        snippet:
          "Who we sell to: mid-market and up (200+ employees), in manufacturing, healthcare, fintech, or logistics, with a named, time-bound use case.",
        why: "This slide is RevOps' own canonical statement of the target buyer, so the brain treats it as the authoritative definition of the ICP rather than inferring one.",
      },
      {
        n: 2,
        title: "Chat with Maya · fit threshold",
        detail: "2026-06-26, in #inbound-routing",
        kind: "chat",
        snippet:
          "Maya: Lower the threshold to 70. We were missing good mid-market leads. — You: Yes, do it.",
        why: "You decided this directly in chat while tuning the tool, and confirmed it. The brain chose this message over the older deck because it is more recent, came from the tool's owner, and was acted on (the tool moved to v4).",
      },
      {
        n: 3,
        title: "Won/lost analysis Q1 to Q2 2026",
        detail: "HubSpot export, processed in brain",
        kind: "crm",
        snippet:
          "Close rate by size: 200–2000 employees closed at 31%, sub-50 at 11%, over Q1–Q2 2026 (n=412 opportunities).",
        why: "This export quantifies the close-rate gap that justified raising the size floor, so it is the evidence behind the “roughly three times” claim about mid-market.",
      },
      {
        n: 4,
        title: "Inbound demo-request routing",
        detail: "tool spec, step “Score fit 0 to 100”",
        kind: "document",
        snippet:
          "Step “Score fit 0–100”: rate fit from company size, industry, and the stated use case, then explain the score.",
        why: "The routing tool's own spec is where the 0–100 fit score is defined, so it is the source for how the ICP gets applied at runtime.",
      },
    ],
    categories: ["Sales", "Routing", "Definitions"],
    seeAlso: ["routing-policy", "nurture"],
  },
  {
    id: "routing-policy",
    title: "AE routing policy",
    category: "Sales",
    updatedAt: "Last edited 2 days ago by Maya",
    summary: "How hot leads are assigned to account executives.",
    infobox: [
      { label: "Channel", value: "#ae-handoffs (Slack)" },
      { label: "Assignment", value: "Round-robin, on-call list" },
    ],
    lead: "The routing policy governs how a request that clears the fit threshold reaches a specific account executive.[[1]]",
    sections: [
      {
        heading: "Handoff",
        paras: [
          "Hot leads (fit score at or above 70) are posted to #ae-handoffs in Slack with the company, contact, score, and the reason for the score.[[1]] Account executives are assigned round-robin from the on-call list, skipping anyone marked out of office.[[2]]",
        ],
      },
      {
        heading: "Edge cases",
        paras: [
          "If no company can be matched in the CRM, the request is flagged for a human rather than routed, to avoid sending an account executive a dead end.[[1]]",
        ],
      },
    ],
    references: [
      {
        n: 1,
        title: "Inbound demo-request routing",
        detail: "tool spec, step “Route to the on-call AE”",
        kind: "document",
        snippet:
          "Step “Route to the on-call AE”: post the lead, score, and reason to #ae-handoffs and assign an AE.",
        why: "The handoff behavior is defined in the tool spec itself, so it is the source of record for how a hot lead reaches an account executive.",
      },
      {
        n: 2,
        title: "On-call roster",
        detail: "Slack user group @ae-oncall",
        kind: "roster",
        snippet:
          "@ae-oncall: Priya, Marco, Dana — rotates weekly, OOO members skipped.",
        why: "Assignments pull live from this Slack group, so it is the source for who is currently on call and eligible for a handoff.",
      },
    ],
    categories: ["Sales", "Routing"],
    seeAlso: ["icp"],
  },
  {
    id: "nurture",
    title: "Nurture sequence",
    category: "Marketing",
    updatedAt: "Last edited last week by your AI",
    summary: "What happens to leads that are not yet a fit.",
    infobox: [
      { label: "Tool", value: "HubSpot workflow" },
      { label: "Re-entry", value: "On firmographic change" },
    ],
    lead: "The nurture sequence holds leads that do not yet clear the fit threshold and keeps them warm until they might.[[1]]",
    sections: [
      {
        heading: "Behavior",
        paras: [
          "Leads below the fit threshold are tagged for the nurture sequence in HubSpot and receive the standard onboarding-education emails.[[1]] A lead can re-enter routing if its firmographics change, for example if the company grows past the size floor.[[2]]",
        ],
      },
    ],
    references: [
      {
        n: 1,
        title: "Nurture workflow",
        detail: "HubSpot, “Inbound nurture v3”",
        kind: "document",
        snippet:
          "Inbound nurture v3: education sequence on a 5-touch cadence, re-entry on firmographic change.",
        why: "The nurture behavior lives in this HubSpot workflow, so it is the source for what actually happens to a below-threshold lead.",
      },
      {
        n: 2,
        title: "Ideal customer profile",
        detail: "this wiki, “Use in routing”",
        kind: "document",
        snippet:
          "A lead can re-enter routing if its firmographics change, for example if the company grows past the size floor.",
        why: "This re-entry rule is defined on the ICP page, so the nurture page cites that page instead of restating the rule and risking drift.",
      },
    ],
    categories: ["Marketing", "Routing"],
    seeAlso: ["icp"],
  },
];

export function getKnowledgePage(id: string): KnowledgePage | undefined {
  return KNOWLEDGE.find((k) => k.id === id);
}

// ── The morning digest (what lands in Slack/email) ───────────────────────

export interface DigestItem {
  label: string;
  value: string;
  tone: "neutral" | "good" | "warn";
}

export const TODAY_DIGEST: DigestItem[] = [
  { label: "processed", value: "14", tone: "neutral" },
  { label: "routed", value: "5", tone: "good" },
  { label: "nurtured", value: "8", tone: "neutral" },
  { label: "needs you", value: "1", tone: "warn" },
];
