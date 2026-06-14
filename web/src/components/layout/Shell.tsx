import type { ReactNode } from "react";

import { useActiveChannelSlug } from "../../routes/useCurrentRoute";
import { AgentPanel } from "../agents/AgentPanel";
import { CommandPaletteHost } from "../command/CommandPalette";
import { TeamMemberWelcome } from "../join/TeamMemberWelcome";
import { InterviewBar } from "../messages/InterviewBar";
import { ThreadPanel } from "../messages/ThreadPanel";
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
  const activeChannelSlug = useActiveChannelSlug();
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
        {/* One mount here keeps the bar in a consistent spot above the status
            bar across the home composer, channel, and task chat. It is scoped
            to the chat the human is currently viewing (useActiveChannelSlug),
            so an agent's question only surfaces in the channel it was asked
            in instead of blocking the composer on every surface. On non-chat
            surfaces the slug is null and the bar stays silent; office-wide
            triage still lives in the Inbox. */}
        <InterviewBar channelSlug={activeChannelSlug} />
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
