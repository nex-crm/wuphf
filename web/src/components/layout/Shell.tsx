import type { ReactNode } from "react";

import { AgentPanel } from "../agents/AgentPanel";
import { CommandPaletteHost } from "../command/CommandPalette";
import { TeamMemberWelcome } from "../join/TeamMemberWelcome";
import { InterviewBar } from "../messages/InterviewBar";
import { ThreadPanel } from "../messages/ThreadPanel";
import { GettingStartedChecklist } from "../onboarding/GettingStartedChecklist";
import { SearchModal } from "../search/SearchModal";
import { HelpModalHost } from "../ui/HelpModal";
import { VersionModalHost } from "../ui/VersionModal";
import { WorkspaceRail } from "../workspaces/WorkspaceRail";
import { ChannelHeader } from "./ChannelHeader";
import { DisconnectBanner } from "./DisconnectBanner";
import { RuntimeStrip } from "./RuntimeStrip";
import { Sidebar } from "./Sidebar";
import { StatusBar } from "./StatusBar";

interface ShellProps {
  children: ReactNode;
}

export function Shell({ children }: ShellProps) {
  // The WorkspaceRail sits to the left of the existing channel sidebar
  // — both rails are flex children of `.office`. The rail is 56px wide
  // and the channel sidebar keeps its own width; the layout reflows
  // automatically.
  return (
    <div className="office">
      <WorkspaceRail />
      <Sidebar />
      <main className="main">
        <DisconnectBanner />
        <TeamMemberWelcome />
        <ChannelHeader />
        <RuntimeStrip />
        {children}
        {/* Pending interviews + external-action approvals are answerable from
            ANY surface, not just the raw channel view. InterviewBar reads the
            office-wide request queue (useRequests) and is channel-agnostic, so
            a single global mount here covers the home composer and the task
            chat — where it used to be absent, leaving an agent's clarifying
            question unanswerable without navigating to #general. */}
        <InterviewBar />
        {/* Post-onboarding "Settle into your office" nudge. The component
            self-hides while loading, once dismissed, or after every item is
            done, so an unconditional mount inside the onboarded Shell is
            safe — it docks above the StatusBar only for the brief window an
            onboarded-but-not-settled founder is still finding their feet. */}
        <GettingStartedChecklist />
        <StatusBar />
      </main>
      <ThreadPanel />
      <AgentPanel />
      <CommandPaletteHost />
      <SearchModal />
      <HelpModalHost />
      <VersionModalHost />
    </div>
  );
}
