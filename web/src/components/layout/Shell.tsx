import { type ReactNode } from 'react'
import { Sidebar } from './Sidebar'
import { ChannelHeader } from './ChannelHeader'
import { DisconnectBanner } from './DisconnectBanner'
import { ThreadPanel } from '../messages/ThreadPanel'
import { AgentPanel } from '../agents/AgentPanel'
import { SearchModal } from '../search/SearchModal'
import { useAppStore } from '../../stores/app'

interface ShellProps {
  children: ReactNode
}

export function Shell({ children }: ShellProps) {
  const dmMode = useAppStore((s) => s.dmMode)

  return (
    <div className="office">
      <Sidebar />
      <main className="main">
        <DisconnectBanner />
        {!dmMode && <ChannelHeader />}
        {children}
      </main>
      <ThreadPanel />
      <AgentPanel />
      <SearchModal />
    </div>
  )
}
