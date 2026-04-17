import { useChannels } from '../../hooks/useChannels'
import { useAppStore } from '../../stores/app'
import { ChannelWizard, useChannelWizard } from '../channels/ChannelWizard'

export function ChannelList() {
  const { data: channels = [] } = useChannels()
  const currentChannel = useAppStore((s) => s.currentChannel)
  const setCurrentChannel = useAppStore((s) => s.setCurrentChannel)
  const currentApp = useAppStore((s) => s.currentApp)
  const wizard = useChannelWizard()

  return (
    <>
      <div className="sidebar-channels">
        {channels.map((ch) => {
          const isActive = currentChannel === ch.slug && !currentApp
          return (
            <button
              key={ch.slug}
              className={`sidebar-item${isActive ? ' active' : ''}`}
              onClick={() => setCurrentChannel(ch.slug)}
            >
              <span style={{ fontSize: 13, color: 'var(--text-tertiary)', width: 18, textAlign: 'center', flexShrink: 0 }}>
                #
              </span>
              <span>{ch.name || ch.slug}</span>
            </button>
          )
        })}
        <button
          className="sidebar-item sidebar-add-btn"
          onClick={wizard.show}
          title="Create a new channel"
        >
          <span style={{ fontSize: 14, width: 18, textAlign: 'center', flexShrink: 0 }}>+</span>
          <span>New Channel</span>
        </button>
      </div>
      <ChannelWizard open={wizard.open} onClose={wizard.hide} />
    </>
  )
}
