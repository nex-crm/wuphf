import { useCallback, useEffect, useRef, useState } from 'react'
import { useAppStore } from '../../stores/app'
import { initApi } from '../../api/client'

export function DisconnectBanner() {
  const brokerConnected = useAppStore((s) => s.brokerConnected)
  const setBrokerConnected = useAppStore((s) => s.setBrokerConnected)

  const [hadConnection, setHadConnection] = useState(false)
  const [dismissed, setDismissed] = useState(false)
  const [retrying, setRetrying] = useState(false)
  const dismissedForRef = useRef<boolean | null>(null)

  // Track that we previously had a connection
  useEffect(() => {
    if (brokerConnected) {
      setHadConnection(true)
      setDismissed(false)
      dismissedForRef.current = null
    }
  }, [brokerConnected])

  // If dismissed and then connection state changes, reset dismiss
  useEffect(() => {
    if (dismissed && dismissedForRef.current !== brokerConnected) {
      setDismissed(false)
    }
  }, [brokerConnected, dismissed])

  const handleRetry = useCallback(async () => {
    setRetrying(true)
    try {
      await initApi()
      setBrokerConnected(true)
    } catch {
      // Still disconnected
    } finally {
      setRetrying(false)
    }
  }, [setBrokerConnected])

  const handleDismiss = useCallback(() => {
    dismissedForRef.current = brokerConnected
    setDismissed(true)
  }, [brokerConnected])

  // Only show when: previously connected, currently disconnected, not dismissed
  const visible = hadConnection && !brokerConnected && !dismissed
  if (!visible) return null

  return (
    <div className="disconnect-banner" role="alert">
      <div className="disconnect-banner-content">
        <svg
          width="16"
          height="16"
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          strokeWidth="2"
          strokeLinecap="round"
          strokeLinejoin="round"
        >
          <path d="M10.29 3.86 1.82 18a2 2 0 0 0 1.71 3h16.94a2 2 0 0 0 1.71-3L13.71 3.86a2 2 0 0 0-3.42 0z" />
          <line x1="12" y1="9" x2="12" y2="13" />
          <line x1="12" y1="17" x2="12.01" y2="17" />
        </svg>
        <span>Connection lost. Reconnecting...</span>
      </div>
      <div className="disconnect-banner-actions">
        <button
          className="btn btn-sm disconnect-banner-retry"
          onClick={handleRetry}
          disabled={retrying}
          type="button"
        >
          {retrying ? 'Retrying...' : 'Retry'}
        </button>
        <button
          className="disconnect-banner-dismiss"
          onClick={handleDismiss}
          type="button"
          aria-label="Dismiss"
        >
          <svg
            width="14"
            height="14"
            viewBox="0 0 24 24"
            fill="none"
            stroke="currentColor"
            strokeWidth="2"
            strokeLinecap="round"
            strokeLinejoin="round"
          >
            <line x1="18" y1="6" x2="6" y2="18" />
            <line x1="6" y1="6" x2="18" y2="18" />
          </svg>
        </button>
      </div>
    </div>
  )
}
