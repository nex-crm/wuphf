// AppWorkflowTab — a READ-ONLY picture of how the app works.
//
// This tab SHOWS the app's deterministic workflow; it does not run, schedule, or
// recompile it. Those are actions taken from the app itself, not from this
// diagram. Building an app compiles its automation once and freezes it, and this
// renders that single frozen plan as a top-to-bottom flow:
//
//   TRIGGER (when it runs)
//     → the app's real compiled steps (deterministic, run identically every time)
//       → DELIVER (where the result goes, human-gated)
//
// The workflow compiles automatically the first time the tab is opened, so the
// picture is always current — there is no button to press here.

import { useEffect, useRef, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Globe, Lock, ShieldCheck } from "lucide-react";

import { showNotice } from "../../components/ui/Toast";
import {
  type BrowserApproval,
  browserApprovalPrompt,
  getBrowserApprovals,
  resolveBrowserApproval,
} from "../apps/browserApprovals";
import {
  type AppWorkflow,
  compileAppWorkflow,
  getAppWorkflow,
  runAppWorkflow,
  type WorkflowStepView,
} from "../apps/workflowClient";
import { EmptyState } from "../components/EmptyState";
import { Eyebrow } from "../components/primitives";

// A short node label per frozen step type, so the flow reads at a glance.
const STEP_GLYPH: Record<string, string> = {
  action: "DO",
  template: "··",
  nex_ask: "AI",
  nex_insights: "AI",
};

interface AppWorkflowTabProps {
  appId: string;
}

export function AppWorkflowTab({ appId }: AppWorkflowTabProps) {
  const qc = useQueryClient();
  const workflowQuery = useQuery({
    queryKey: ["operator-app-workflow", appId],
    queryFn: () => getAppWorkflow(appId),
  });

  const wf = workflowQuery.data;
  const compiled = Boolean(wf?.compiled && wf.steps && wf.steps.length > 0);

  // The workflow is intrinsic to the app, so it compiles automatically the first
  // time the tab is opened — the picture is always current, no button to press.
  const compile = useMutation({
    mutationFn: () => compileAppWorkflow(appId),
    onSuccess: (data) =>
      qc.setQueryData(["operator-app-workflow", appId], data),
    onError: (err) =>
      showNotice(
        err instanceof Error ? err.message : "Could not read this workflow.",
        "error",
      ),
  });

  // Fire once per app, only after the query settles and only if nothing is
  // compiled yet. A failed compile can be retried from the error state.
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
          Building the app compiled its automation once and froze it. This is
          that one flow — when it runs, the exact steps it takes, and where the
          result goes — deterministic, the same every time. Run or schedule it
          from the app itself.
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
        <CompiledWorkflow wf={wf} appId={appId} />
      ) : (
        <Compiling
          failed={compile.isError}
          onRetry={() => {
            autoCompiledFor.current = appId;
            compile.mutate();
          }}
        />
      )}
    </div>
  );
}

// The workflow compiles automatically (it is part of the app), so there is no
// "compile" button — only a working state, and a retry if the model was briefly
// unavailable while producing the picture.
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
        title="Could not read the workflow"
        hint="Your AI could not lay out this app's workflow just now. It will retry, or you can try again."
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
      <div className="opr-empty-title">Laying out this app's workflow…</div>
      <div className="opr-empty-hint">
        Your AI is turning what this app does into a deterministic workflow.
      </div>
    </div>
  );
}

function CompiledWorkflow({ wf, appId }: { wf: AppWorkflow; appId: string }) {
  const steps = wf.steps ?? [];
  // Which Slack channel delivery targets — configured inline on the delivery
  // node itself, not as a separate block. Running/scheduling is done from the
  // app; here you just say where the result lands.
  const [channel, setChannel] = useState("#general");

  // The tab is a READ-ONLY picture — EXCEPT for a workflow that contains a
  // `browser` step. Browser execution is inherently interactive: it drives the
  // operator's own browser and pauses to ask permission per step, so it needs a
  // live trigger here. A workflow with no browser step stays fully read-only.
  const hasBrowserStep = steps.some((s) => s.type === "browser");
  const runLive = useMutation({
    mutationFn: () => runAppWorkflow(appId, false, {}),
    onSuccess: () => showNotice("Live run finished.", "success"),
    onError: (err) =>
      showNotice(
        err instanceof Error ? err.message : "Could not run this workflow.",
        "error",
      ),
  });
  const approvalsQuery = useQuery({
    queryKey: ["operator-app-browser-approvals", appId],
    queryFn: () => getBrowserApprovals(appId),
    enabled: runLive.isPending,
    refetchInterval: runLive.isPending ? 1200 : false,
  });
  const resolveApproval = useMutation({
    mutationFn: ({
      id,
      decision,
    }: {
      id: string;
      decision: "approve" | "deny";
    }) => resolveBrowserApproval(appId, id, decision),
    onSuccess: () => approvalsQuery.refetch(),
  });
  const approvals: BrowserApproval[] = approvalsQuery.data ?? [];

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

      {/* ONE flow, read-only: trigger → the app's compiled steps → delivery.
          The delivery node carries its own channel picker (native to the node). */}
      <div className="opr-flow">
        <FlowFrameNode
          glyph="TR"
          nodeKind="trigger"
          kindLabel="trigger"
          title="Runs on demand or on a schedule"
          detail="The app runs this when you trigger it, or on a schedule you set for it."
        />
        {steps.map((step) => (
          <WorkflowStep key={step.id} step={step} last={false} />
        ))}
        <FlowFrameNode
          glyph="DO"
          nodeKind="action"
          kindLabel="deliver"
          title="Deliver to Slack"
          detail="Posts the app's result to this Slack channel."
          gate="Approval required before it sends"
          channel={channel}
          onChannelChange={setChannel}
          last={true}
        />
      </div>

      {hasBrowserStep ? (
        <>
          <div className="opr-delivery-actions">
            <button
              type="button"
              className="opr-btn opr-btn-primary opr-btn-sm"
              onClick={() => runLive.mutate()}
              disabled={runLive.isPending}
              title="Runs for real — a browser step asks to control your browser first."
            >
              <Globe size={13} strokeWidth={1.9} aria-hidden={true} />
              {runLive.isPending ? "Running live…" : "Run live"}
            </button>
          </div>
          {approvals.length > 0 ? (
            <section
              className="opr-browser-asks"
              aria-label="Browser step approvals"
            >
              {approvals.map((a) => (
                <div className="opr-browser-ask" key={a.id}>
                  <div className="opr-browser-ask-head">
                    <Globe size={13} strokeWidth={1.9} aria-hidden={true} />
                    {a.kind === "send"
                      ? "Confirm this send"
                      : "Control your browser?"}
                  </div>
                  <p className="opr-browser-ask-body">
                    {browserApprovalPrompt(a)}
                  </p>
                  <div className="opr-browser-ask-actions">
                    <button
                      type="button"
                      className="opr-btn opr-btn-primary opr-btn-sm"
                      onClick={() =>
                        resolveApproval.mutate({
                          id: a.id,
                          decision: "approve",
                        })
                      }
                      disabled={resolveApproval.isPending}
                    >
                      {a.kind === "send" ? "Send it" : "Allow"}
                    </button>
                    <button
                      type="button"
                      className="opr-btn opr-btn-ghost opr-btn-sm"
                      onClick={() =>
                        resolveApproval.mutate({ id: a.id, decision: "deny" })
                      }
                      disabled={resolveApproval.isPending}
                    >
                      Not now
                    </button>
                  </div>
                </div>
              ))}
            </section>
          ) : null}
        </>
      ) : null}
    </div>
  );
}

// A synthetic framing node (the trigger at the top, the delivery at the tail) —
// same visual language as a compiled step so the flow reads as ONE list. The
// delivery node may carry an inline channel picker (native to the node), so
// choosing where the result lands lives ON the node, not in a separate block.
function FlowFrameNode({
  glyph,
  nodeKind,
  kindLabel,
  title,
  detail,
  gate,
  channel,
  onChannelChange,
  last,
}: {
  glyph: string;
  nodeKind: string;
  kindLabel: string;
  title: string;
  detail: string;
  gate?: string;
  channel?: string;
  onChannelChange?: (value: string) => void;
  last?: boolean;
}) {
  return (
    <div className="opr-step">
      <div className="opr-step-rail">
        <div
          className={`opr-step-node opr-step-node-${nodeKind}`}
          aria-hidden={true}
        >
          {glyph}
        </div>
        {last ? null : <div className="opr-step-line" />}
      </div>
      <div className="opr-step-body">
        <div className="opr-step-kind">{kindLabel}</div>
        <div className="opr-step-title">
          {title}
          {onChannelChange ? (
            <input
              className="opr-step-channel"
              type="text"
              value={channel ?? ""}
              onChange={(e) => onChannelChange(e.target.value)}
              placeholder="#channel"
              aria-label="Slack channel"
              spellCheck={false}
            />
          ) : null}
        </div>
        <div className="opr-step-detail">{detail}</div>
        {gate ? (
          <div className="opr-step-gate">
            <Lock size={11} strokeWidth={2} aria-hidden={true} />
            {gate}
          </div>
        ) : null}
      </div>
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
  // A browser step has no integration — Nex drives the operator's own browser
  // for it — so it reads as its own kind: the Globe node + a "runs in your
  // browser" line, distinct from an API action step.
  const isBrowser = step.type === "browser";
  const nodeVariant = isBrowser ? "browser" : step.gated ? "action" : "enrich";
  return (
    <div className="opr-step">
      <div className="opr-step-rail">
        <div
          className={`opr-step-node opr-step-node-${nodeVariant}`}
          aria-hidden={true}
        >
          {isBrowser ? (
            <Globe size={13} strokeWidth={2} />
          ) : (
            (STEP_GLYPH[step.type] ?? "··")
          )}
        </div>
        {last ? null : <div className="opr-step-line" />}
      </div>
      <div className="opr-step-body">
        <div className="opr-step-kind">{isBrowser ? "browser" : step.type}</div>
        <div className="opr-step-title">
          {title}
          {step.platform ? (
            <span className="opr-step-chip">{step.platform}</span>
          ) : null}
        </div>
        {step.run_if ? (
          <div className="opr-step-detail">Only when {step.run_if}</div>
        ) : null}
        {isBrowser ? (
          <div className="opr-step-browser">
            <Globe size={11} strokeWidth={2} aria-hidden={true} />
            Runs in your browser — Nex drives it, and asks before it sends
          </div>
        ) : step.gated ? (
          <div className="opr-step-gate">
            <Lock size={11} strokeWidth={2} aria-hidden={true} />
            Held for your approval before it sends
          </div>
        ) : null}
      </div>
    </div>
  );
}
