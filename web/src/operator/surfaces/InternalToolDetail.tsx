// Work Tool detail — the core object. Everything scoped to one tool lives here
// as a tab:
//   UI           — the built mini-app the operator runs (a live request table)
//   Workflow     — the deterministic spec + run history + version history
//   Data         — the operator-owned typed tables behind the tool
//   Integrations — which integrations this tool uses + add from connected ones
//   Knowledge    — the knowledge this tool can draw on (inherited from workspace)
//
// Integrations and knowledge are connected/owned once for the workspace, but are
// only ever shown scoped under the tool that uses them — there is no global
// Integrations or Knowledge surface. "Edit with AI" opens the SAME build chat we
// ship for new tools, scoped to this tool, as an overlay over any tab.

import { useEffect, useRef, useState } from "react";
import {
  ArrowLeft,
  ArrowRight,
  CheckCircle2,
  ChevronsLeft,
  ChevronsRight,
  Maximize2,
  Minimize2,
  PhoneCall,
  Play,
  Plus,
  Power,
  Send,
  Sparkles,
  X,
} from "lucide-react";

import { ApprovalCard } from "../components/ApprovalCard";
import { EmptyState } from "../components/EmptyState";
import {
  Eyebrow,
  RequestStatusPill,
  ScorePill,
  sigil,
  type TabDef,
  Tabs,
  ToolStatusBadge,
} from "../components/primitives";
import {
  INBOUND_REQUESTS,
  type InboundRequest,
  type InternalTool,
  TODAY_DIGEST,
  type ToolVersion,
  type WorkflowStep,
} from "../mock/data";
import { AppToolsTab } from "./AppToolsTab";
import { KnowledgeSurface } from "./KnowledgeSurface";
import { ToolIntegrations } from "./ToolIntegrations";
import { type BuiltWorkflow, WorkflowBuilder } from "./WorkflowBuilder";

type ToolTab =
  | "ui"
  | "workflow"
  | "tools"
  | "data"
  | "integrations"
  | "knowledge";

const TABS: readonly TabDef<ToolTab>[] = [
  { id: "ui", label: "UI" },
  { id: "workflow", label: "Workflow" },
  // Tools Nex builds from taught workflows; the app's chat calls them. Additive.
  { id: "tools", label: "Tools" },
  { id: "data", label: "Data" },
  { id: "integrations", label: "Integrations" },
  { id: "knowledge", label: "Knowledge" },
];

// Route the chat to the screen its change is about, from the operator's own
// words, so they watch the right screen as the AI works. Workflow is the default
// (the most common edit target). Returns null when nothing clearly matches.
function inferToolTab(message: string): ToolTab | null {
  const t = message.toLowerCase();
  if (
    /\b(screen|ui|button|form|layout|design|how it looks|display|the page)\b/.test(
      t,
    )
  ) {
    return "ui";
  }
  if (/\b(data|rows?|records?|columns?|the table|fields?)\b/.test(t)) {
    return "data";
  }
  if (/\b(integration|connect|hubspot|slack|gmail|composio|app)\b/.test(t)) {
    return "integrations";
  }
  if (
    /\b(step|workflow|threshold|route|trigger|score|branch|decision|send|post|notify|gate|approval|sequence|nurture)\b/.test(
      t,
    )
  ) {
    return "workflow";
  }
  return null;
}

interface InternalToolDetailProps {
  tool: InternalTool;
  onBack: () => void;
  onStartCall: () => void;
  // Which tab to land on. "Run on test data" hands off to the Workflow tab
  // (where run history lives); a plain open stays on the UI tab.
  initialTab?: ToolTab;
  // When set, a "Demo workflow to Nex" call just finished on this tool: open the
  // Ask-AI chat and seed it so the AI starts reworking from the demonstrated
  // change without the operator re-typing it.
  demoSeed?: string;
}

export function InternalToolDetail({
  tool,
  onBack,
  onStartCall,
  initialTab = "ui",
  demoSeed,
}: InternalToolDetailProps) {
  const [tab, setTab] = useState<ToolTab>(initialTab);
  // The chat is the SAME build chat, scoped to this tool, docked as a right-side
  // panel (dock -> wide -> full-screen modal) over the tool's real screens. The
  // chat acts ON the screens: a workflow change navigates to the Workflow tab
  // and updates it there, rather than living in a canvas glued to the chat.
  // A demo handoff opens the chat straight away (seeded below).
  const [chatOpen, setChatOpen] = useState(Boolean(demoSeed));
  const [panelSize, setPanelSize] = useState<"dock" | "wide" | "modal">("dock");
  // The demo seed is one-shot: it kicks the chat off on mount, then is cleared
  // so reopening the chat later does not replay the demonstrated instruction.
  const demoSeedRef = useRef(demoSeed);
  const [seedConsumed, setSeedConsumed] = useState(false);
  useEffect(() => {
    if (demoSeedRef.current) setSeedConsumed(true);
  }, []);
  const [version] = useState(tool.version);
  const [versions] = useState<ToolVersion[]>(tool.versions);
  // The tool's live workflow steps — the chat edits these and they render on the
  // Workflow tab. Changed steps flash so the edit is legible on that screen.
  const [liveSteps, setLiveSteps] = useState<WorkflowStep[]>(tool.steps);
  const [changedStepIds, setChangedStepIds] = useState<readonly string[]>([]);
  // Parent-owned inbound rows, shared by the UI and Data tabs so approving a
  // request on the UI tab is reflected in the Data tab too (no tab-local copy).
  const [inboundRows, setInboundRows] =
    useState<InboundRequest[]>(INBOUND_REQUESTS);
  const isInbound = tool.id === "inbound-routing";

  function approveInbound(req: InboundRequest) {
    setInboundRows((rs) =>
      rs.map((r) =>
        r.id === req.id
          ? { ...r, status: "routed", routedTo: "Priya (AE)" }
          : r,
      ),
    );
  }

  // When the chat reworks the workflow, show it on the Workflow screen the chat
  // was acting on: navigate there, update the steps, and flash what changed.
  function applyWorkflowEdit(draft: BuiltWorkflow) {
    const changed = draft.steps
      .filter((step) => {
        const prev = liveSteps.find((p) => p.id === step.id);
        return (
          !prev || prev.title !== step.title || prev.detail !== step.detail
        );
      })
      .map((step) => step.id);
    setLiveSteps(draft.steps);
    setChangedStepIds(changed);
    setTab("workflow");
  }

  return (
    <div
      className={`opr-detail-wrap${
        chatOpen && panelSize !== "modal" ? ` is-chat-${panelSize}` : ""
      }`}
    >
      <div className="opr-surface-wide">
        <button type="button" className="opr-back" onClick={onBack}>
          <ArrowLeft size={13} strokeWidth={1.9} aria-hidden={true} />
          All tools
        </button>

        <div className="opr-detail-head">
          <span className="opr-tool-emoji" aria-hidden={true}>
            {sigil(tool.name)}
          </span>
          <div className="opr-detail-titles">
            <div className="opr-detail-name">{tool.name}</div>
            <p className="opr-tool-summary">{tool.summary}</p>
            <div className="opr-tool-meta">
              <ToolStatusBadge status={tool.status} />
              <span className="opr-meta-dot">
                v{version} · built from {tool.builtFrom}
              </span>
              <span className="opr-meta-dot">{tool.runsToday} runs today</span>
            </div>
          </div>
          <div className="opr-detail-actions">
            <button
              type="button"
              className="opr-btn opr-btn-sm"
              onClick={onStartCall}
            >
              <PhoneCall size={13} strokeWidth={1.9} aria-hidden={true} />
              Demo to Nex
            </button>
            <button
              type="button"
              className="opr-btn opr-btn-sm"
              onClick={() => setChatOpen(true)}
            >
              <Sparkles size={13} strokeWidth={1.9} aria-hidden={true} />
              Ask AI
            </button>
            {tool.status === "enabled" && (
              <>
                <button type="button" className="opr-btn opr-btn-sm">
                  <Power size={13} strokeWidth={1.9} aria-hidden={true} />
                  Disable
                </button>
                <button
                  type="button"
                  className="opr-btn opr-btn-primary opr-btn-sm"
                >
                  <CheckCircle2
                    size={13}
                    strokeWidth={1.9}
                    aria-hidden={true}
                  />
                  Publish new version
                </button>
              </>
            )}
            {tool.status === "disabled" && (
              <>
                <button
                  type="button"
                  className="opr-btn opr-btn-primary opr-btn-sm"
                >
                  <Power size={13} strokeWidth={1.9} aria-hidden={true} />
                  Enable
                </button>
                <button type="button" className="opr-btn opr-btn-sm">
                  <CheckCircle2
                    size={13}
                    strokeWidth={1.9}
                    aria-hidden={true}
                  />
                  Publish new version
                </button>
              </>
            )}
            {tool.status === "draft" && (
              <>
                <button type="button" className="opr-btn opr-btn-sm">
                  <Play size={13} strokeWidth={1.9} aria-hidden={true} />
                  Run on test data
                </button>
                <button
                  type="button"
                  className="opr-btn opr-btn-primary opr-btn-sm"
                >
                  <CheckCircle2
                    size={13}
                    strokeWidth={1.9}
                    aria-hidden={true}
                  />
                  Publish
                </button>
              </>
            )}
            {tool.status === "suggested" && (
              <button
                type="button"
                className="opr-btn opr-btn-primary opr-btn-sm"
              >
                <Plus size={13} strokeWidth={1.9} aria-hidden={true} />
                Build it
              </button>
            )}
          </div>
        </div>

        <Tabs
          tabs={TABS}
          active={tab}
          onSelect={setTab}
          hint={tab === "workflow" ? "deterministic · audited" : undefined}
        />

        <div
          role="tabpanel"
          id={`opr-panel-${tab}`}
          aria-labelledby={`opr-tab-${tab}`}
        >
          {tab === "ui" &&
            (isInbound ? (
              <UITab rows={inboundRows} onApprove={approveInbound} />
            ) : (
              <EmptyState
                glyph="◧"
                title="No screen built yet"
                hint="Build the screen your team will use to run this tool. Demo it to Nex on a call and your AI assembles it."
                actionLabel="Demo workflow to Nex"
                onAction={onStartCall}
              />
            ))}
          {tab === "workflow" && (
            <WorkflowTab
              tool={{ ...tool, steps: liveSteps }}
              versions={versions}
              changedStepIds={changedStepIds}
            />
          )}
          {tab === "tools" && <AppToolsTab appName={tool.name} />}
          {tab === "data" &&
            (isInbound ? (
              <DataTab tool={tool} rows={inboundRows} />
            ) : (
              <EmptyState
                glyph="▦"
                title="No data yet"
                hint="Run this tool on test data. The rows it produces, with their statuses, show up here as a table you own."
                actionLabel="Run on test data"
              />
            ))}
          {tab === "integrations" && <ToolIntegrationsTab tool={tool} />}
          {tab === "knowledge" && <ToolKnowledgeTab />}
        </div>
      </div>

      {/* Two peer affordances, reachable on any tab: demo a change to the tool
          on a call, or ask its AI in chat. Hidden while the chat panel is open
          (it would overlap them). */}
      {chatOpen ? null : (
        <div className="opr-detail-fabs">
          <button
            type="button"
            className="opr-ask-fab"
            onClick={onStartCall}
            aria-label={`Demo a change to ${tool.name} to Nex`}
          >
            <PhoneCall size={16} strokeWidth={2} aria-hidden={true} />
            Demo to Nex
          </button>
          <button
            type="button"
            className="opr-ask-fab"
            onClick={() => setChatOpen(true)}
            aria-label={`Ask AI about ${tool.name}`}
          >
            <Sparkles size={16} strokeWidth={2} aria-hidden={true} />
            Ask AI
          </button>
        </div>
      )}

      {chatOpen ? (
        <>
          {panelSize === "modal" ? (
            <button
              type="button"
              className="opr-ask-backdrop"
              aria-label="Close chat"
              onClick={() => setChatOpen(false)}
            />
          ) : null}
          <aside
            className={`opr-ask-panel is-${panelSize}`}
            aria-label={`Ask AI about ${tool.name}`}
          >
            <div className="opr-ask-bar">
              <span className="opr-ask-bar-title">
                <Sparkles size={13} strokeWidth={2} aria-hidden={true} />
                Ask AI · {tool.name}
              </span>
              <div className="opr-ask-bar-controls">
                <button
                  type="button"
                  className="opr-icon-btn"
                  onClick={() =>
                    setPanelSize((s) => (s === "wide" ? "dock" : "wide"))
                  }
                  aria-label={
                    panelSize === "wide" ? "Narrow panel" : "Widen panel"
                  }
                  title={panelSize === "wide" ? "Narrow" : "Widen"}
                >
                  {panelSize === "wide" ? (
                    <ChevronsRight
                      size={15}
                      strokeWidth={1.9}
                      aria-hidden={true}
                    />
                  ) : (
                    <ChevronsLeft
                      size={15}
                      strokeWidth={1.9}
                      aria-hidden={true}
                    />
                  )}
                </button>
                <button
                  type="button"
                  className="opr-icon-btn"
                  onClick={() =>
                    setPanelSize((s) => (s === "modal" ? "dock" : "modal"))
                  }
                  aria-label={
                    panelSize === "modal" ? "Exit full screen" : "Full screen"
                  }
                  title={
                    panelSize === "modal" ? "Exit full screen" : "Full screen"
                  }
                >
                  {panelSize === "modal" ? (
                    <Minimize2 size={14} strokeWidth={1.9} aria-hidden={true} />
                  ) : (
                    <Maximize2 size={14} strokeWidth={1.9} aria-hidden={true} />
                  )}
                </button>
                <button
                  type="button"
                  className="opr-icon-btn"
                  onClick={() => setChatOpen(false)}
                  aria-label="Close chat"
                  title="Close"
                >
                  <X size={15} strokeWidth={1.9} aria-hidden={true} />
                </button>
              </div>
            </div>
            <div className="opr-ask-body">
              <WorkflowBuilder
                panelMode={true}
                scopeToolName={tool.name}
                seed={seedConsumed ? undefined : demoSeedRef.current}
                onClose={() => setChatOpen(false)}
                onFinish={applyWorkflowEdit}
                onUserMessage={(text) => {
                  const target = inferToolTab(text);
                  if (target) setTab(target);
                }}
              />
            </div>
          </aside>
        </>
      ) : null}
    </div>
  );
}

// ── UI tab: the built mini-app ──────────────────────────────────────────

function UITab({
  rows,
  onApprove,
}: {
  rows: InboundRequest[];
  onApprove: (req: InboundRequest) => void;
}) {
  const [pending, setPending] = useState<InboundRequest | null>(null);

  function approve(req: InboundRequest) {
    // Mutate the parent-owned rows so the Data tab sees the same routed status.
    onApprove(req);
    setPending(null);
  }

  return (
    <div>
      <div className="opr-readout">
        <span className="opr-readout-lead">today</span>
        {TODAY_DIGEST.map((d) => (
          <span className="opr-readout-stat" key={d.label}>
            <span
              className={`opr-readout-num${
                d.tone === "good"
                  ? " opr-digest-good"
                  : d.tone === "warn"
                    ? " opr-digest-warn"
                    : ""
              }`}
            >
              {d.value}
            </span>
            <span className="opr-readout-label">{d.label}</span>
          </span>
        ))}
      </div>

      {pending ? (
        <ApprovalCard
          integration="Slack"
          title={`Route ${pending.company} to an AE in #ae-handoffs?`}
          detail={`Fit ${pending.fitScore}/100, ${pending.reason}`}
          onApprove={() => approve(pending)}
          onSkip={() => setPending(null)}
        />
      ) : null}

      {rows.length === 0 ? (
        <EmptyState
          glyph="◷"
          title="No requests yet today"
          hint="When a demo request comes in, it lands here, scored and routed automatically. Fire a test request to see it work."
          actionLabel="Run on test data"
        />
      ) : (
        <div className="opr-table-wrap">
          <div className="opr-table-toolbar">
            <Eyebrow>Incoming demo requests · today</Eyebrow>
            <span className="opr-pill opr-pill-muted">
              {rows.length} requests
            </span>
          </div>
          <table className="opr-table">
            <thead>
              <tr>
                <th>Company</th>
                <th>Contact</th>
                <th>Source</th>
                <th>Fit</th>
                <th>Status</th>
                <th />
              </tr>
            </thead>
            <tbody>
              {rows.map((r) => (
                <tr key={r.id}>
                  <td>
                    <div className="opr-cell-primary">{r.company}</div>
                    <div className="opr-cell-sub">{r.receivedAt}</div>
                  </td>
                  <td>
                    <div>{r.contact}</div>
                    <div className="opr-cell-sub">{r.email}</div>
                  </td>
                  <td className="opr-cell-sub">{r.source}</td>
                  <td>
                    <ScorePill score={r.fitScore} />
                  </td>
                  <td>
                    <RequestStatusPill status={r.status} />
                    {r.routedTo ? (
                      <div className="opr-cell-sub">
                        assigned to {r.routedTo}
                      </div>
                    ) : null}
                  </td>
                  <td style={{ textAlign: "right" }}>
                    {r.status === "scored" &&
                    r.fitScore !== null &&
                    r.fitScore >= 70 ? (
                      <button
                        type="button"
                        className="opr-btn opr-btn-sm"
                        onClick={() => setPending(r)}
                      >
                        Route
                        <Send size={13} strokeWidth={1.9} aria-hidden={true} />
                      </button>
                    ) : r.status === "needs-you" ? (
                      <button type="button" className="opr-btn opr-btn-sm">
                        <ArrowRight
                          size={13}
                          strokeWidth={1.9}
                          aria-hidden={true}
                        />
                        Review
                      </button>
                    ) : null}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </div>
  );
}

// ── Workflow tab: the deterministic spec ────────────────────────────────

const STEP_GLYPH: Record<WorkflowStep["kind"], string> = {
  trigger: "TR",
  enrich: "EN",
  ai: "AI",
  decision: "IF",
  action: "DO",
  branch: "EL",
};

function stepNodeClass(kind: WorkflowStep["kind"]): string {
  return `opr-step-node opr-step-node-${kind}`;
}

function WorkflowTab({
  tool,
  versions,
  changedStepIds = [],
}: {
  tool: InternalTool;
  versions: ToolVersion[];
  changedStepIds?: readonly string[];
}) {
  return (
    <div className="opr-detail-cols">
      <div>
        <div className="opr-flow-head">
          <Eyebrow>How it runs · every step is scripted</Eyebrow>
        </div>
        <div className="opr-flow" style={{ marginTop: "var(--space-3)" }}>
          {tool.steps.map((step, i) => (
            <div
              className={`opr-step${
                changedStepIds.includes(step.id) ? " opr-step-flash" : ""
              }`}
              key={step.id}
            >
              <div className="opr-step-rail">
                <div className={stepNodeClass(step.kind)} aria-hidden={true}>
                  {STEP_GLYPH[step.kind]}
                </div>
                {i < tool.steps.length - 1 ? (
                  <div className="opr-step-line" />
                ) : null}
              </div>
              <div className="opr-step-body">
                <div className="opr-step-kind">{step.kind}</div>
                <div className="opr-step-title">
                  {step.title}
                  {step.integration ? (
                    <span className="opr-step-chip">{step.integration}</span>
                  ) : null}
                </div>
                <div className="opr-step-detail">{step.detail}</div>
                {step.gated ? (
                  <div className="opr-step-gate">
                    Approval required before it sends
                  </div>
                ) : null}
              </div>
            </div>
          ))}
        </div>
      </div>

      <div>
        <div className="opr-rail-card">
          <Eyebrow>Recent runs</Eyebrow>
          {tool.runs.length === 0 ? (
            <div className="opr-step-detail" style={{ marginTop: 8 }}>
              No runs yet.
            </div>
          ) : (
            <div style={{ marginTop: "var(--space-2)" }}>
              {tool.runs.map((run) => (
                <div className="opr-run-row" key={run.id}>
                  <span
                    className={`opr-led ${
                      run.outcome === "routed"
                        ? "opr-led-live"
                        : run.outcome === "needs-you"
                          ? "opr-led-warn"
                          : "opr-led-draft"
                    }`}
                    style={{ marginTop: 5 }}
                  />
                  <div style={{ flex: 1, minWidth: 0 }}>
                    <div style={{ fontWeight: 550 }}>{run.trigger}</div>
                    <div className="opr-cell-sub">{run.summary}</div>
                  </div>
                  <div className="opr-cell-sub">{run.ranAt}</div>
                </div>
              ))}
            </div>
          )}
        </div>

        <div className="opr-rail-card">
          <Eyebrow>Version history</Eyebrow>
          <div style={{ marginTop: "var(--space-2)" }}>
            {versions.map((v) => (
              <div className="opr-version-row" key={v.version}>
                <span className="opr-version-num">v{v.version}</span>
                <div style={{ flex: 1, minWidth: 0 }}>
                  <div style={{ fontWeight: 550 }}>{v.label}</div>
                  <div className="opr-cell-sub">
                    {v.author} · {v.at}
                  </div>
                  <div
                    className="opr-step-detail"
                    style={{ marginTop: 2, fontStyle: "italic" }}
                  >
                    {v.note}
                  </div>
                </div>
              </div>
            ))}
          </div>
        </div>
      </div>
    </div>
  );
}

// ── Data tab: operator-owned typed tables ───────────────────────────────

function DataTab({
  tool,
  rows,
}: {
  tool: InternalTool;
  rows: InboundRequest[];
}) {
  return (
    <div>
      <Eyebrow>Requests</Eyebrow>
      <div className="opr-table-wrap" style={{ marginTop: "var(--space-3)" }}>
        <div className="opr-table-toolbar">
          <span className="opr-pill opr-pill-muted">{rows.length} rows</span>
          <button type="button" className="opr-btn opr-btn-sm">
            <Plus size={13} strokeWidth={1.9} aria-hidden={true} />
            Add column
          </button>
        </div>
        <table className="opr-table">
          <thead>
            <tr>
              {tool.dataColumns.map((c) => (
                <th key={c.key}>
                  {c.label}
                  <span className="opr-schema-type">{c.type}</span>
                </th>
              ))}
            </tr>
          </thead>
          <tbody>
            {rows.map((r) => (
              <tr key={r.id}>
                <td className="opr-cell-primary">{r.company}</td>
                <td>{r.contact}</td>
                <td className="opr-cell-sub">{r.email}</td>
                <td className="opr-cell-sub">{r.source}</td>
                <td>
                  <ScorePill score={r.fitScore} />
                </td>
                <td>
                  <RequestStatusPill status={r.status} />
                </td>
                <td className="opr-cell-sub">{r.receivedAt}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </div>
  );
}

// Integrations this tool uses, derived from its workflow steps. Integrations are
// connected once for the workspace and reused across tools; ToolIntegrations
// renders the real catalog (logos, search, connect) in the operator's styling.
function ToolIntegrationsTab({ tool }: { tool: InternalTool }) {
  const usedNames = Array.from(
    new Set(
      (tool.steps ?? [])
        .map((s) => s.integration?.trim())
        .filter((name): name is string => Boolean(name)),
    ),
  );
  return <ToolIntegrations usedNames={usedNames} />;
}

// Knowledge is owned at the workspace level and inherited by every tool; this
// tab frames the global knowledge as what THIS tool can draw on.
function ToolKnowledgeTab() {
  return (
    <div className="opr-tool-scoped">
      <p className="opr-scoped-note">
        Knowledge is owned by your workspace and inherited here. This tool can
        draw on everything below.
      </p>
      <KnowledgeSurface />
    </div>
  );
}
