// OperatorApp — the operator-MLP product shell, mounted full-bleed at
// /#/operator (see RootRoute). Self-contained: all navigation is local state,
// all data is mock. This is the frontend-first slice — the shape to react to
// before any backend exists. See docs/specs/operator-mlp-plan.md.

import { useState } from "react";

import "../styles/operator-shell.css";

import { CallModal } from "./components/CallModal";
import { OperatorSidebar, type OperatorSurface } from "./OperatorSidebar";
import { ChatsSurface } from "./surfaces/ChatsSurface";
import { IntegrationsSurface } from "./surfaces/IntegrationsSurface";
import { InternalToolDetail } from "./surfaces/InternalToolDetail";
import { InternalToolsSurface } from "./surfaces/InternalToolsSurface";
import { KnowledgeSurface } from "./surfaces/KnowledgeSurface";
import { SettingsSurface } from "./surfaces/SettingsSurface";
import { WorkflowBuilder } from "./surfaces/WorkflowBuilder";
import { getTool } from "./mock/data";

export function OperatorApp() {
  const [surface, setSurface] = useState<OperatorSurface>("tools");
  const [selectedToolId, setSelectedToolId] = useState<string | null>(null);
  const [callOpen, setCallOpen] = useState(false);
  const [building, setBuilding] = useState(false);

  function go(next: OperatorSurface) {
    setSurface(next);
    setSelectedToolId(null);
    setBuilding(false);
  }

  function openTool(id: string) {
    setSelectedToolId(id);
    setSurface("tools");
    setBuilding(false);
  }

  const selectedTool = selectedToolId ? getTool(selectedToolId) : undefined;

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
            onFinish={openTool}
          />
        ) : surface === "chats" ? (
          <ChatsSurface
            onStartCall={() => setCallOpen(true)}
            onBuild={() => setBuilding(true)}
          />
        ) : (
          <div className="opr-surface">
            {surface === "tools" &&
              (selectedTool ? (
                <InternalToolDetail
                  tool={selectedTool}
                  onBack={() => setSelectedToolId(null)}
                  onStartCall={() => setCallOpen(true)}
                />
              ) : (
                <InternalToolsSurface
                  onOpen={openTool}
                  onStartCall={() => setCallOpen(true)}
                  onBuild={() => setBuilding(true)}
                />
              ))}
            {surface === "knowledge" && <KnowledgeSurface />}
            {surface === "integrations" && <IntegrationsSurface />}
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
