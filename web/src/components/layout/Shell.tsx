import type { ReactNode } from "react";

import { isDMChannel, useAppStore } from "../../stores/app";
import { AgentPanel } from "../agents/AgentPanel";
import { ThreadPanel } from "../messages/ThreadPanel";
import { SearchModal } from "../search/SearchModal";
import { HelpModalHost } from "../ui/HelpModal";
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
  const currentChannel = useAppStore((s) => s.currentChannel);
  const currentApp = useAppStore((s) => s.currentApp);
  const channelMeta = useAppStore((s) => s.channelMeta);
  const inDM = !currentApp && !!isDMChannel(currentChannel, channelMeta);

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
        {!inDM && <ChannelHeader />}
        {!inDM && <RuntimeStrip />}
        {children}
        <StatusBar />
      </main>
      <ThreadPanel />
      <AgentPanel />
      <SearchModal />
      <HelpModalHost />
    </div>
  );
}
