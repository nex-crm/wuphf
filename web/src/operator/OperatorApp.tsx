// OperatorApp — the operator-MLP product shell, mounted full-bleed at
// /#/operator (see RootRoute). Navigation is local state. Apps are REAL: the
// "Build an app" flow drives the shipped app-builder backend, and an app's
// detail renders the live, persisted app in its UI tab. The mock workflow
// tooling (WorkflowBuilder, mock tools) stays reachable for the workflow-tab
// story we built earlier. See docs/specs/operator-mlp-plan.md.

import { useState } from "react";

import "../styles/operator-shell.css";

import { isRealAppId } from "./apps/useOperatorApps";
import { ApprovalPrompt } from "./components/ApprovalPrompt";
import { CallModal } from "./components/CallModal";
import { getTool } from "./mock/data";
import { OperatorSidebar, type OperatorSurface } from "./OperatorSidebar";
import { AppBuilderChat } from "./surfaces/AppBuilderChat";
import { InternalToolDetail } from "./surfaces/InternalToolDetail";
import { InternalToolsSurface } from "./surfaces/InternalToolsSurface";
import { OperatorAppDetail } from "./surfaces/OperatorAppDetail";
import { SettingsSurface } from "./surfaces/SettingsSurface";
import {
  type BuiltWorkflow,
  type FinishMode,
  WorkflowBuilder,
} from "./surfaces/WorkflowBuilder";

// The "Demo workflow to Nex" call runs in one of two modes: BUILD a new tool
// (the entry points in the sidebar / tools list / chats) or MODIFY an existing
// one (the peer-to-Ask-AI affordance on a tool detail, which carries the tool).
type CallContext =
  | { mode: "build" }
  | { mode: "modify"; toolId: string; toolName: string };

export function OperatorApp() {
  const [surface, setSurface] = useState<OperatorSurface>("tools");
  const [selectedId, setSelectedId] = useState<string | null>(null);
  const [call, setCall] = useState<CallContext | null>(null);
  // Two builders: the app builder (primary, real) and the legacy workflow
  // builder (kept for the workflow-tab path).
  const [appBuilding, setAppBuilding] = useState(false);
  const [building, setBuilding] = useState(false);
  // The workflow just built in the builder, carried so its clarified steps
  // survive the handoff instead of falling back to the seeded mock.
  const [builtDraft, setBuiltDraft] = useState<BuiltWorkflow | null>(null);
  // "run" handoffs land on the Workflow tab (run history); plain opens stay UI.
  const [openOnWorkflowTab, setOpenOnWorkflowTab] = useState(false);
  // Bumped to force the tool detail to remount — used when a modify call ends on
  // the SAME tool it started from, so the detail re-opens on the Workflow tab
  // (where the demonstrated change shows) instead of keeping its prior tab.
  const [detailNonce, setDetailNonce] = useState(0);

  function resetSubState() {
    setSelectedId(null);
    setAppBuilding(false);
    setBuilding(false);
    setBuiltDraft(null);
    setOpenOnWorkflowTab(false);
  }

  function go(next: OperatorSurface) {
    setSurface(next);
    resetSubState();
  }

  function openTool(id: string) {
    resetSubState();
    setSelectedId(id);
    setSurface("tools");
  }

  function finishApp(appId: string) {
    resetSubState();
    setSelectedId(appId);
    setSurface("tools");
  }

  function finishWorkflow(draft: BuiltWorkflow, mode: FinishMode) {
    setBuiltDraft(draft);
    setSelectedId(draft.toolId);
    setSurface("tools");
    setBuilding(false);
    setAppBuilding(false);
    setOpenOnWorkflowTab(mode === "run");
  }

  const baseTool =
    selectedId && !isRealAppId(selectedId) ? getTool(selectedId) : undefined;
  // When the open came from the builder, overlay the built name + clarified
  // steps onto the base tool so the detail reflects what was actually built.
  const selectedTool =
    baseTool && builtDraft && builtDraft.toolId === selectedId
      ? {
          ...baseTool,
          name: builtDraft.name,
          steps: builtDraft.steps,
          status: "draft" as const,
        }
      : baseTool;

  function renderSurface() {
    if (surface === "settings") return <SettingsSurface />;
    if (isRealAppId(selectedId)) {
      return (
        <OperatorAppDetail
          key={selectedId}
          appId={selectedId as string}
          onBack={() => setSelectedId(null)}
        />
      );
    }
    if (selectedTool) {
      return (
        <InternalToolDetail
          key={`${selectedTool.id}:${detailNonce}`}
          tool={selectedTool}
          onBack={() => setSelectedId(null)}
          onStartCall={() =>
            setCall({
              mode: "modify",
              toolId: selectedTool.id,
              toolName: selectedTool.name,
            })
          }
          initialTab={openOnWorkflowTab ? "workflow" : "ui"}
        />
      );
    }
    return (
      <InternalToolsSurface
        onOpen={openTool}
        onStartCall={() => setCall({ mode: "build" })}
        onBuild={() => setAppBuilding(true)}
      />
    );
  }

  return (
    <div className="opr-root">
      <OperatorSidebar
        active={surface}
        onSelect={go}
        onStartCall={() => setCall({ mode: "build" })}
        onBuild={() => setAppBuilding(true)}
      />

      <main className="opr-main">
        {appBuilding ? (
          <AppBuilderChat
            onClose={() => setAppBuilding(false)}
            onFinish={finishApp}
          />
        ) : building ? (
          <WorkflowBuilder
            onClose={() => setBuilding(false)}
            onFinish={finishWorkflow}
          />
        ) : (
          <div className="opr-surface">{renderSurface()}</div>
        )}
      </main>

      {/* Auto-surfaced approvals (e.g. an app's Slack post) — global overlay. */}
      <ApprovalPrompt />

      {call ? (
        <CallModal
          tool={call.mode === "modify" ? { name: call.toolName } : undefined}
          onClose={() => setCall(null)}
          onBuild={() => {
            // Build mode drafts a new tool (the inbound-routing mock); modify
            // mode lands back on the tool whose change was just demonstrated,
            // on its Workflow tab where the change shows.
            const target =
              call.mode === "modify" ? call.toolId : "inbound-routing";
            setCall(null);
            setOpenOnWorkflowTab(call.mode === "modify");
            setSelectedId(target);
            setSurface("tools");
            if (call.mode === "modify") setDetailNonce((n) => n + 1);
          }}
        />
      ) : null}
    </div>
  );
}
