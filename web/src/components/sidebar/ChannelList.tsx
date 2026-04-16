import { useChannels } from '../../hooks/useChannels'
import { useAppStore } from '../../stores/app'

export function ChannelList() {
  const { data: channels = [] } = useChannels()
  const currentChannel = useAppStore((s) => s.currentChannel)
  const setCurrentChannel = useAppStore((s) => s.setCurrentChannel)
  const currentApp = useAppStore((s) => s.currentApp)

  return (
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
    </div>
  )
}
