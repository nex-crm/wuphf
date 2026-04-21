import { useChannels } from '../../hooks/useChannels'
import { useOverflow } from '../../hooks/useOverflow'
import { useAppStore } from '../../stores/app'
import { ChannelWizard, useChannelWizard } from '../channels/ChannelWizard'
import { SidebarItemLabel } from './SidebarItemLabel'

export function ChannelList() {
  const { data: channels = [] } = useChannels()
  const currentChannel = useAppStore((s) => s.currentChannel)
  const setCurrentChannel = useAppStore((s) => s.setCurrentChannel)
  const currentApp = useAppStore((s) => s.currentApp)
  const wizard = useChannelWizard()
  const overflowRef = useOverflow<HTMLDivElement>()

  return (
    <>
      <div className="sidebar-scroll-wrap is-channels">
      <div className="sidebar-channels" ref={overflowRef}>
        {channels.map((ch) => {
          const isActive = currentChannel === ch.slug && !currentApp
          return (
            <button
              key={ch.slug}
              className={`sidebar-item${isActive ? ' active' : ''}`}
              onClick={() => setCurrentChannel(ch.slug)}
            >
              <span style={{ color: 'currentColor', width: 18, textAlign: 'center', flexShrink: 0 }}>
                #
              </span>
              <SidebarItemLabel>{ch.name || ch.slug}</SidebarItemLabel>
            </button>
          )
        })}
        <button
          className="sidebar-item sidebar-add-btn"
          onClick={wizard.show}
          title="Create a new channel"
        >
          <span style={{ width: 18, textAlign: 'center', flexShrink: 0, display: 'inline-block' }}>+</span>
          <span>New Channel</span>
        </button>
      </div>
      </div>
      <ChannelWizard open={wizard.open} onClose={wizard.hide} />
    </>
  )
}
