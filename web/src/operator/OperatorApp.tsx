// OperatorApp — the operator-MLP product shell, mounted full-bleed at
// /#/operator (see RootRoute). Navigation is local state. Apps are REAL: the
// "Build an app" flow drives the shipped app-builder backend, and an app's
// detail renders the live, persisted app in its UI tab. The mock workflow
// tooling (WorkflowBuilder, mock tools) stays reachable for the workflow-tab
// story we built earlier. See docs/specs/operator-mlp-plan.md.

import { useState } from "react";

import "../styles/operator-shell.css";

import { capturePromptSeed } from "./apps/demoCapture";
import { isRealAppId } from "./apps/useOperatorApps";
import { useRealtimeConfig } from "./apps/useRealtimeConfig";
import { ApprovalPrompt } from "./components/ApprovalPrompt";
import { CallModal } from "./components/CallModal";
import { RealCallModal } from "./components/RealCallModal";
import { getTool } from "./mock/data";
import { OperatorSidebar, type OperatorSurface } from "./OperatorSidebar";
import { InternalToolDetail } from "./surfaces/InternalToolDetail";
import { InternalToolsSurface } from "./surfaces/InternalToolsSurface";
import { OperatorAppDetail } from "./surfaces/OperatorAppDetail";
import { OperatorBuildExperience } from "./surfaces/OperatorBuildExperience";
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
  // The real voice call needs an OpenAI Realtime key; without one we fall back
  // to the scripted mock so the flow is still demonstrable.
  const realtime = useRealtimeConfig();
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
  // the SAME tool it started from, so the detail re-opens with the AI already
  // working on the demonstrated change.
  const [detailNonce, setDetailNonce] = useState(0);
  // Seeds handed to the build engine by a "Demo workflow to Nex" call so the AI
  // starts working from the captured context: buildSeed feeds a fresh workflow
  // build; demoSeed feeds the chat scoped to the tool being modified.
  const [buildSeed, setBuildSeed] = useState<string | null>(null);
  const [demoSeed, setDemoSeed] = useState<string | null>(null);

  function resetSubState() {
    setSelectedId(null);
    setAppBuilding(false);
    setBuilding(false);
    setBuiltDraft(null);
    setOpenOnWorkflowTab(false);
    setBuildSeed(null);
    setDemoSeed(null);
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
          demoSeed={demoSeed ?? undefined}
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
    <div className="opr-root" data-testid="operator-root">
      <OperatorSidebar
        active={surface}
        onSelect={go}
        onStartCall={() => setCall({ mode: "build" })}
        onBuild={() => setAppBuilding(true)}
      />

      <main className="opr-main">
        {appBuilding ? (
          <OperatorBuildExperience
            onClose={() => setAppBuilding(false)}
            onFinish={finishApp}
          />
        ) : building ? (
          <WorkflowBuilder
            seed={buildSeed ?? undefined}
            onClose={() => setBuilding(false)}
            onFinish={finishWorkflow}
          />
        ) : (
          <div className="opr-surface">{renderSurface()}</div>
        )}
      </main>

      {/* Auto-surfaced approvals (e.g. an app's Slack post) — global overlay. */}
      <ApprovalPrompt />

      {call
        ? (() => {
            // Real call when a Realtime key is configured; mock otherwise. Both
            // share the same props and the same capture → build-engine handoff.
            const CallSurface = realtime.available ? RealCallModal : CallModal;
            return (
              <CallSurface
                tool={
                  call.mode === "modify"
                    ? { id: call.toolId, name: call.toolName }
                    : undefined
                }
                onClose={() => setCall(null)}
                onBuild={(capture) => {
                  // The call captured everything; hand it to the AI, which starts
                  // working at once. A modify call reopens the tool with its chat
                  // already reworking the demonstrated change; a build call opens the
                  // workflow builder, already assembling the new tool.
                  const seed = capturePromptSeed(capture);
                  resetSubState();
                  setCall(null);
                  if (capture.mode === "modify" && capture.toolId) {
                    setDemoSeed(seed);
                    setSelectedId(capture.toolId);
                    setSurface("tools");
                    setDetailNonce((n) => n + 1);
                  } else {
                    setBuildSeed(seed);
                    setBuilding(true);
                    setSurface("tools");
                  }
                }}
              />
            );
          })()
        : null}
    </div>
  );
}
