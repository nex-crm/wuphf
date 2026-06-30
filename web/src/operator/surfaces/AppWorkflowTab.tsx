// AppWorkflowTab — the app's DETERMINISTIC workflow, the real engine behind the
// Workflow tab (not a hardcoded diagram).
//
// The promise: building an app makes its automation deterministic. So this tab
// COMPILES the app once from its real capabilities and freezes the plan, then
// renders those exact frozen steps and runs the SAME plan every time. "Run once"
// is a dry-run preview — deterministic, nothing sent. The Slack delivery
// schedule (the shipped grant + routine) stays below as the way to run it on a
// cadence.

import { useEffect, useRef, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Lock, Play, Plug, ShieldCheck, Sparkles } from "lucide-react";

import { showNotice } from "../../components/ui/Toast";
import {
  type AppWorkflow,
  type ConnectionChoice,
  compileAppWorkflow,
  getAppWorkflow,
  getAppWorkflowConnections,
  runAppWorkflow,
  type WorkflowConnectionsResult,
  type WorkflowRunResult,
  type WorkflowStepView,
} from "../apps/workflowClient";
import { EmptyState } from "../components/EmptyState";
import { Eyebrow } from "../components/primitives";
import { AppDeliverySchedule } from "./AppDeliverySchedule";

// A short node label per frozen step type, so the flow reads at a glance.
const STEP_GLYPH: Record<string, string> = {
  action: "DO",
  template: "··",
  nex_ask: "AI",
  nex_insights: "AI",
};

interface AppWorkflowTabProps {
  appId: string;
  appName: string;
}

export function AppWorkflowTab({ appId, appName }: AppWorkflowTabProps) {
  const qc = useQueryClient();
  const [chosen, setChosen] = useState<ConnectionChoice>({});
  const workflowQuery = useQuery({
    queryKey: ["operator-app-workflow", appId],
    queryFn: () => getAppWorkflow(appId),
  });

  const wf = workflowQuery.data;
  const compiled = Boolean(wf?.compiled && wf.steps && wf.steps.length > 0);

  // Which account to use per external platform — only needed once compiled.
  const connectionsQuery = useQuery({
    queryKey: ["operator-app-workflow-connections", appId],
    queryFn: () => getAppWorkflowConnections(appId),
    enabled: compiled,
  });

  // Effective choice: the operator's pick, else the first account for a platform.
  function effectiveConnections(): ConnectionChoice {
    const out: ConnectionChoice = {};
    for (const p of connectionsQuery.data?.platforms ?? []) {
      const pick = chosen[p.platform] || p.connections[0]?.key;
      if (pick) out[p.platform] = pick;
    }
    return out;
  }

  const compile = useMutation({
    mutationFn: () => compileAppWorkflow(appId),
    onSuccess: (data) => {
      qc.setQueryData(["operator-app-workflow", appId], data);
      showNotice(
        "Compiled — this workflow now runs the same way every time.",
        "success",
      );
    },
    onError: (err) => {
      showNotice(
        err instanceof Error ? err.message : "Could not compile this workflow.",
        "error",
      );
    },
  });

  const run = useMutation({
    mutationFn: () => runAppWorkflow(appId, true, effectiveConnections()),
    onSuccess: () =>
      showNotice("Previewed the workflow — nothing was sent.", "success"),
    onError: (err) => {
      showNotice(
        err instanceof Error ? err.message : "Could not run this workflow.",
        "error",
      );
    },
  });

  // The workflow is intrinsic to the app, so it compiles automatically the first
  // time the tab is opened — no button. Fire once per app, only after the query
  // settles and only if nothing is compiled yet.
  const autoCompiledFor = useRef<string | null>(null);
  const compileMutate = compile.mutate;
  useEffect(() => {
    if (!workflowQuery.isSuccess || compiled) return;
    if (autoCompiledFor.current === appId) return;
    autoCompiledFor.current = appId;
    compileMutate();
  }, [workflowQuery.isSuccess, compiled, appId, compileMutate]);

  return (
    <div className="opr-tool-scoped opr-app-workflow">
      <div className="opr-data-intro">
        <Eyebrow>How this app runs</Eyebrow>
        <p className="opr-scoped-note">
          Building the app compiles a workflow once, then freezes it. Every run
          executes the exact same steps — deterministic, no surprises.
        </p>
      </div>

      {workflowQuery.isLoading ? (
        <div className="opr-app-building" role="status">
          <span className="opr-work-dots" aria-hidden={true}>
            <span />
            <span />
            <span />
          </span>
          <div className="opr-empty-title">Reading the workflow…</div>
        </div>
      ) : compiled && wf ? (
        <CompiledWorkflow
          wf={wf}
          connections={connectionsQuery.data}
          chosen={chosen}
          onChoose={(platform, key) =>
            setChosen((prev) => ({ ...prev, [platform]: key }))
          }
          onRun={() => run.mutate()}
          running={run.isPending}
          lastRun={run.data}
          onRecompile={() => compile.mutate()}
          recompiling={compile.isPending}
        />
      ) : (
        <Compiling
          failed={compile.isError}
          onRetry={() => compile.mutate()}
        />
      )}

      <div className="opr-workflow-divider">
        <Eyebrow>Deliver on a schedule</Eyebrow>
      </div>
      <AppDeliverySchedule appName={appName} appId={appId} />
    </div>
  );
}

// The workflow compiles automatically (it is part of the app), so there is no
// "compile" button — only a working state, and a retry if the model was briefly
// unavailable.
function Compiling({
  failed,
  onRetry,
}: {
  failed: boolean;
  onRetry: () => void;
}) {
  if (failed) {
    return (
      <EmptyState
        glyph="⚠"
        title="Could not build the workflow"
        hint="Your AI could not design this app's workflow just now. It will retry, or you can try again."
        actionLabel="Try again"
        onAction={onRetry}
      />
    );
  }
  return (
    <div className="opr-app-building" role="status">
      <span className="opr-work-dots" aria-hidden={true}>
        <span />
        <span />
        <span />
      </span>
      <div className="opr-empty-title">Designing this app's workflow…</div>
      <div className="opr-empty-hint">
        Your AI is turning what this app does into a deterministic workflow.
      </div>
    </div>
  );
}

function CompiledWorkflow({
  wf,
  connections,
  chosen,
  onChoose,
  onRun,
  running,
  lastRun,
  onRecompile,
  recompiling,
}: {
  wf: AppWorkflow;
  connections?: WorkflowConnectionsResult;
  chosen: ConnectionChoice;
  onChoose: (platform: string, key: string) => void;
  onRun: () => void;
  running: boolean;
  lastRun?: WorkflowRunResult;
  onRecompile: () => void;
  recompiling: boolean;
}) {
  const steps = wf.steps ?? [];
  const platforms = connections?.platforms ?? [];
  return (
    <div className="opr-workflow-frozen">
      <div className="opr-workflow-banner">
        <span className="opr-pill opr-pill-good">
          <ShieldCheck size={12} strokeWidth={2} aria-hidden={true} />
          Deterministic
        </span>
        <span className="opr-scoped-note">
          Frozen plan · {steps.length} step{steps.length === 1 ? "" : "s"} ·
          runs identically every time
        </span>
      </div>

      <div className="opr-flow">
        {steps.map((step, i) => (
          <WorkflowStep
            key={step.id}
            step={step}
            last={i === steps.length - 1}
          />
        ))}
      </div>

      {platforms.length > 0 ? (
        <ConnectionChooser
          platforms={platforms}
          chosen={chosen}
          onChoose={onChoose}
        />
      ) : null}

      <div className="opr-delivery-actions">
        <button
          type="button"
          className="opr-btn opr-btn-primary opr-btn-sm"
          onClick={onRun}
          disabled={running}
        >
          <Play size={13} strokeWidth={1.9} aria-hidden={true} />
          {running ? "Running…" : "Run once (preview)"}
        </button>
        <button
          type="button"
          className="opr-btn opr-btn-sm"
          onClick={onRecompile}
          disabled={recompiling}
        >
          <Sparkles size={13} strokeWidth={1.9} aria-hidden={true} />
          {recompiling ? "Recompiling…" : "Recompile"}
        </button>
      </div>

      {lastRun ? (
        <p className="opr-scoped-note">
          Previewed {Object.keys(lastRun.steps ?? {}).length} step
          {Object.keys(lastRun.steps ?? {}).length === 1 ? "" : "s"} — this was
          a dry run, so nothing was sent.
        </p>
      ) : null}
    </div>
  );
}

// Per-platform account picker. A platform with one account shows it read-only;
// with several, a select disambiguates which the run uses; with none, a prompt
// to connect. The default (first account) is applied even without interaction,
// so a run never fails on ambiguity.
function ConnectionChooser({
  platforms,
  chosen,
  onChoose,
}: {
  platforms: WorkflowConnectionsResult["platforms"];
  chosen: ConnectionChoice;
  onChoose: (platform: string, key: string) => void;
}) {
  return (
    <div className="opr-data-block opr-conn-chooser">
      <div className="opr-data-block-head">
        <Plug size={12} strokeWidth={2} aria-hidden={true} />
        Accounts
        <span className="opr-data-block-sub">which account each step uses</span>
      </div>
      {platforms.map((p) => {
        const selected = chosen[p.platform] || p.connections[0]?.key || "";
        return (
          <div className="opr-conn-row" key={p.platform}>
            <span className="opr-conn-platform">{p.platform}</span>
            {p.connections.length === 0 ? (
              <span className="opr-pill opr-pill-bad">
                <span className="opr-led opr-led-bad" />
                Not connected
              </span>
            ) : p.connections.length === 1 ? (
              <span className="opr-conn-single">
                {p.connections[0].name || p.connections[0].key}
              </span>
            ) : (
              <select
                className="opr-conn-select"
                value={selected}
                onChange={(e) => onChoose(p.platform, e.target.value)}
                aria-label={`Account for ${p.platform}`}
              >
                {p.connections.map((c) => (
                  <option key={c.key} value={c.key}>
                    {c.name || c.key}
                  </option>
                ))}
              </select>
            )}
          </div>
        );
      })}
    </div>
  );
}

function WorkflowStep({
  step,
  last,
}: {
  step: WorkflowStepView;
  last: boolean;
}) {
  const title = step.description || step.template || step.action_id || step.id;
  return (
    <div className="opr-step">
      <div className="opr-step-rail">
        <div
          className={`opr-step-node opr-step-node-${step.gated ? "action" : "enrich"}`}
          aria-hidden={true}
        >
          {STEP_GLYPH[step.type] ?? "··"}
        </div>
        {last ? null : <div className="opr-step-line" />}
      </div>
      <div className="opr-step-body">
        <div className="opr-step-kind">{step.type}</div>
        <div className="opr-step-title">
          {title}
          {step.platform ? (
            <span className="opr-step-chip">{step.platform}</span>
          ) : null}
        </div>
        {step.run_if ? (
          <div className="opr-step-detail">Only when {step.run_if}</div>
        ) : null}
        {step.gated ? (
          <div className="opr-step-gate">
            <Lock size={11} strokeWidth={2} aria-hidden={true} />
            Held for your approval before it sends
          </div>
        ) : null}
      </div>
    </div>
  );
}
