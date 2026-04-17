import { useOfficeMembers } from '../../hooks/useMembers'
import { useOverflow } from '../../hooks/useOverflow'
import { useAppStore } from '../../stores/app'
import { PixelAvatar } from '../ui/PixelAvatar'
import { AgentWizard, useAgentWizard } from '../agents/AgentWizard'
import type { OfficeMember } from '../../api/client'

function classifyActivity(member: OfficeMember | undefined) {
  if (!member) return { state: 'lurking', label: 'lurking', dotClass: 'lurking' }
  const status = (member.status || '').toLowerCase()
  const activity = (member.task || '').toLowerCase()

  if (status === 'active' && /tool|code|write|edit|commit|build|deploy|ship|push|run|test/.test(activity))
    return { state: 'shipping', label: 'shipping', dotClass: 'shipping' }
  if (status === 'active' && /think|plan|queue|review|sync|debug|trace|investigat/.test(activity))
    return { state: 'plotting', label: 'plotting', dotClass: 'plotting' }
  if (status === 'active')
    return { state: 'talking', label: 'talking', dotClass: 'active pulse' }
  return { state: 'lurking', label: 'lurking', dotClass: 'lurking' }
}

export function AgentList() {
  const { data: members = [] } = useOfficeMembers()
  const setActiveAgentSlug = useAppStore((s) => s.setActiveAgentSlug)
  const currentChannel = useAppStore((s) => s.currentChannel)
  const channelMeta = useAppStore((s) => s.channelMeta)
  const wizard = useAgentWizard()
  const overflowRef = useOverflow<HTMLDivElement>()

  const agents = members.filter((m) => m.slug && m.slug !== 'human')

  return (
    <>
      <div className="sidebar-scroll-wrap is-agents">
      <div className="sidebar-agents" ref={overflowRef}>
        {agents.length === 0 ? (
          <div style={{ fontSize: 11, color: 'var(--text-tertiary)', padding: '4px 8px' }}>
            No agents online
          </div>
        ) : (
          agents.map((agent) => {
            const ac = classifyActivity(agent)
            const meta = channelMeta[currentChannel]
            const isDMActive = meta?.type === 'D' && meta.agentSlug === agent.slug

            return (
              <button
                key={agent.slug}
                data-agent-slug={agent.slug}
                className={`sidebar-agent${isDMActive ? ' active' : ''}`}
                title={`${agent.name} — ${ac.label}`}
                onClick={() => setActiveAgentSlug(agent.slug)}
              >
                <span className="sidebar-agent-avatar">
                  <PixelAvatar
                    slug={agent.slug}
                    size={24}
                    className="pixel-avatar-sidebar"
                  />
                </span>
                <div className="sidebar-agent-wrap">
                  <span className="sidebar-agent-name">{agent.name || agent.slug}</span>
                  {agent.task && (
                    <span className="sidebar-agent-task">{agent.task}</span>
                  )}
                </div>
                <span className={`status-dot ${ac.dotClass}`} />
              </button>
            )
          })
        )}
        <button
          className="sidebar-item sidebar-add-btn"
          onClick={wizard.show}
          title="Create a new agent"
        >
          <span style={{ width: 18, textAlign: 'center', flexShrink: 0 }}>+</span>
          <span>New Agent</span>
        </button>
      </div>
      </div>
      <AgentWizard open={wizard.open} onClose={wizard.hide} />
    </>
  )
}
