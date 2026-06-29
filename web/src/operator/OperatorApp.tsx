// OperatorApp — the operator-MLP product shell, mounted full-bleed at
// /#/operator (see RootRoute). Self-contained: all navigation is local state,
// all data is mock. This is the frontend-first slice — the shape to react to
// before any backend exists. See docs/specs/operator-mlp-plan.md.

import { useState } from "react";

import "../styles/operator-shell.css";

import { CallModal } from "./components/CallModal";
import { getTool } from "./mock/data";
import { OperatorSidebar, type OperatorSurface } from "./OperatorSidebar";
import { InternalToolDetail } from "./surfaces/InternalToolDetail";
import { InternalToolsSurface } from "./surfaces/InternalToolsSurface";
import { SettingsSurface } from "./surfaces/SettingsSurface";
import {
  type BuiltWorkflow,
  type FinishMode,
  WorkflowBuilder,
} from "./surfaces/WorkflowBuilder";

export function OperatorApp() {
  const [surface, setSurface] = useState<OperatorSurface>("tools");
  const [selectedToolId, setSelectedToolId] = useState<string | null>(null);
  const [callOpen, setCallOpen] = useState(false);
  const [building, setBuilding] = useState(false);
  // The workflow just built in the builder, carried so its clarified steps
  // survive the handoff instead of falling back to the seeded mock.
  const [builtDraft, setBuiltDraft] = useState<BuiltWorkflow | null>(null);
  // "run" handoffs land on the Workflow tab (run history); plain opens stay UI.
  const [openOnWorkflowTab, setOpenOnWorkflowTab] = useState(false);

  function go(next: OperatorSurface) {
    setSurface(next);
    setSelectedToolId(null);
    setBuilding(false);
    setBuiltDraft(null);
    setOpenOnWorkflowTab(false);
  }

  function openTool(id: string) {
    setSelectedToolId(id);
    setSurface("tools");
    setBuilding(false);
    setBuiltDraft(null);
    setOpenOnWorkflowTab(false);
  }

  function finishWorkflow(draft: BuiltWorkflow, mode: FinishMode) {
    setBuiltDraft(draft);
    setSelectedToolId(draft.toolId);
    setSurface("tools");
    setBuilding(false);
    setOpenOnWorkflowTab(mode === "run");
  }

  const baseTool = selectedToolId ? getTool(selectedToolId) : undefined;
  // When the open came from the builder, overlay the built name + clarified
  // steps onto the base tool so the detail reflects what was actually built.
  const selectedTool =
    baseTool && builtDraft && builtDraft.toolId === selectedToolId
      ? {
          ...baseTool,
          name: builtDraft.name,
          steps: builtDraft.steps,
          status: "draft" as const,
        }
      : baseTool;

  return (
    <div className="opr-root">
      <OperatorSidebar
        active={surface}
        onSelect={go}
        onStartCall={() => setCallOpen(true)}
        onBuild={() => setBuilding(true)}
      />

      <main className="opr-main">
        {building ? (
          <WorkflowBuilder
            onClose={() => setBuilding(false)}
            onFinish={finishWorkflow}
          />
        ) : (
          <div className="opr-surface">
            {surface === "tools" &&
              (selectedTool ? (
                <InternalToolDetail
                  key={selectedTool.id}
                  tool={selectedTool}
                  onBack={() => setSelectedToolId(null)}
                  onStartCall={() => setCallOpen(true)}
                  initialTab={openOnWorkflowTab ? "workflow" : "ui"}
                />
              ) : (
                <InternalToolsSurface
                  onOpen={openTool}
                  onStartCall={() => setCallOpen(true)}
                  onBuild={() => setBuilding(true)}
                />
              ))}
            {surface === "settings" && <SettingsSurface />}
          </div>
        )}
      </main>

      {callOpen ? (
        <CallModal
          onClose={() => setCallOpen(false)}
          onBuild={() => {
            setCallOpen(false);
            openTool("inbound-routing");
          }}
        />
      ) : null}
    </div>
  );
}
