// Internal Tool detail — the core object. Three tabs:
//   UI       — the built mini-app the operator runs (a live request table)
//   Workflow — the deterministic spec + run history + version history
//   Data     — the operator-owned typed tables behind the tool
//
// Mock data only. The UI tab demonstrates the human approval gate (CQ1) when
// routing a hot lead to an external system.

import { useState } from "react";
import {
  ArrowLeft,
  ArrowRight,
  CheckCircle2,
  Pencil,
  Play,
  Plus,
  Power,
  Send,
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
import { ToolEditChat } from "./ToolEditChat";

type ToolTab = "ui" | "workflow" | "data";

const TABS: readonly TabDef<ToolTab>[] = [
  { id: "ui", label: "UI" },
  { id: "workflow", label: "Workflow" },
  { id: "data", label: "Data" },
];

interface InternalToolDetailProps {
  tool: InternalTool;
  onBack: () => void;
  onStartCall: () => void;
  // Which tab to land on. "Run on test data" hands off to the Workflow tab
  // (where run history lives); a plain open stays on the UI tab.
  initialTab?: ToolTab;
}

export function InternalToolDetail({
  tool,
  onBack,
  onStartCall,
  initialTab = "ui",
}: InternalToolDetailProps) {
  const [tab, setTab] = useState<ToolTab>(initialTab);
  const [editing, setEditing] = useState(false);
  // Parent-owned version state: an edit applied in ToolEditChat lifts up here so
  // the version meta and version history reflect the publish. Seeded from the
  // tool prop; remounted per tool via a key at the mount site (OperatorApp).
  const [version, setVersion] = useState(tool.version);
  const [versions, setVersions] = useState<ToolVersion[]>(tool.versions);
  const isInbound = tool.id === "inbound-routing";
  const canEdit = tool.status !== "suggested";

  function handleApply(applied: ToolVersion) {
    setVersion(applied.version);
    setVersions((prev) => [applied, ...prev]);
  }

  return (
    <div className={`opr-detail-wrap${editing ? " is-editing" : ""}`}>
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
            {canEdit && (
              <button
                type="button"
                className="opr-btn opr-btn-sm"
                onClick={() => setEditing(true)}
              >
                <Pencil size={13} strokeWidth={1.9} aria-hidden={true} />
                Edit with AI
              </button>
            )}
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
              <UITab />
            ) : (
              <EmptyState
                glyph="◧"
                title="No screen built yet"
                hint="Build the screen your team will use to run this tool. Talk it through on a call and your AI assembles it."
                actionLabel="Teach your workflow to Nex"
                onAction={onStartCall}
              />
            ))}
          {tab === "workflow" && (
            <WorkflowTab tool={tool} versions={versions} />
          )}
          {tab === "data" &&
            (isInbound ? (
              <DataTab tool={tool} />
            ) : (
              <EmptyState
                glyph="▦"
                title="No data yet"
                hint="Run this tool on test data. The rows it produces, with their statuses, show up here as a table you own."
                actionLabel="Run on test data"
              />
            ))}
        </div>
      </div>

      {editing ? (
        <ToolEditChat
          tool={tool}
          onClose={() => setEditing(false)}
          onApply={handleApply}
        />
      ) : null}
    </div>
  );
}

// ── UI tab: the built mini-app ──────────────────────────────────────────

function UITab() {
  // Local copy so the approval demo can mutate a row's status on approve.
  const [rows, setRows] = useState<InboundRequest[]>(INBOUND_REQUESTS);
  const [pending, setPending] = useState<InboundRequest | null>(null);

  function approve(req: InboundRequest) {
    setRows((rs) =>
      rs.map((r) =>
        r.id === req.id
          ? { ...r, status: "routed", routedTo: "Priya (AE)" }
          : r,
      ),
    );
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
}: {
  tool: InternalTool;
  versions: ToolVersion[];
}) {
  return (
    <div className="opr-detail-cols">
      <div>
        <Eyebrow>How it runs · every step is scripted</Eyebrow>
        <div className="opr-flow" style={{ marginTop: "var(--space-3)" }}>
          {tool.steps.map((step, i) => (
            <div className="opr-step" key={step.id}>
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

function DataTab({ tool }: { tool: InternalTool }) {
  return (
    <div>
      <Eyebrow>Requests</Eyebrow>
      <div className="opr-table-wrap" style={{ marginTop: "var(--space-3)" }}>
        <div className="opr-table-toolbar">
          <span className="opr-pill opr-pill-muted">
            {INBOUND_REQUESTS.length} rows
          </span>
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
            {INBOUND_REQUESTS.map((r) => (
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
