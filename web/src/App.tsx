import { Component, useEffect, useState, type ReactNode } from 'react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { initApi, getHealth } from './api/client'
import { useAppStore } from './stores/app'
import { Shell } from './components/layout/Shell'
import { MessageFeed } from './components/messages/MessageFeed'
import { Composer } from './components/messages/Composer'
import { TypingIndicator } from './components/messages/TypingIndicator'
import { DMView } from './components/messages/DMView'
import { StudioApp } from './components/apps/StudioApp'
import { TasksApp } from './components/apps/TasksApp'
import { RequestsApp } from './components/apps/RequestsApp'
import { PoliciesApp } from './components/apps/PoliciesApp'
import { CalendarApp } from './components/apps/CalendarApp'
import { SkillsApp } from './components/apps/SkillsApp'
import { ArtifactsApp } from './components/apps/ArtifactsApp'
import { ReceiptsApp } from './components/apps/ReceiptsApp'
import { HealthCheckApp } from './components/apps/HealthCheckApp'
import { Wizard } from './components/onboarding/Wizard'
import { AgentPanel } from './components/agents/AgentPanel'
import { SearchModal } from './components/search/SearchModal'
import { DisconnectBanner } from './components/layout/DisconnectBanner'
import { SplashScreen } from './components/onboarding/SplashScreen'
import { ToastContainer } from './components/ui/Toast'
import { useKeyboardShortcuts } from './hooks/useKeyboardShortcuts'
import './styles/global.css'
import './styles/layout.css'
import './styles/messages.css'
import './styles/agents.css'
import './styles/search.css'

const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      retry: 1,
      staleTime: 2000,
    },
  },
})

// ── Error boundary ─────────────────────────────────────────────

interface ErrorBoundaryState {
  error: Error | null
}

class ErrorBoundary extends Component<{ children: ReactNode }, ErrorBoundaryState> {
  state: ErrorBoundaryState = { error: null }

  static getDerivedStateFromError(error: Error): ErrorBoundaryState {
    return { error }
  }

  componentDidCatch(error: Error, info: { componentStack?: string | null }) {
    // eslint-disable-next-line no-console
    console.error('[WUPHF ErrorBoundary]', error, info)
  }

  render() {
    if (this.state.error) {
      return (
        <div style={{
          position: 'fixed', top: 0, left: 0, right: 0, bottom: 0,
          background: '#fee', color: '#900', padding: 20,
          fontFamily: '-apple-system, BlinkMacSystemFont, sans-serif',
          fontSize: 13, overflowY: 'auto', zIndex: 9999,
        }}>
          <h2 style={{ margin: '0 0 8px 0', fontSize: 14 }}>
            Something broke in the UI
          </h2>
          <pre style={{ margin: '8px 0 0', fontFamily: 'SFMono-Regular, Menlo, monospace', fontSize: 11, whiteSpace: 'pre-wrap' }}>
            {this.state.error.message}
            {'\n\n'}
            {this.state.error.stack}
          </pre>
          <button
            onClick={() => this.setState({ error: null })}
            style={{ marginTop: 12, padding: '6px 12px', fontSize: 12, cursor: 'pointer' }}
          >
            Try again
          </button>
        </div>
      )
    }
    return this.props.children
  }
}

// ── Routed main content ─────────────────────────────────────────

function MainContent() {
  const currentApp = useAppStore((s) => s.currentApp)
  const dmMode = useAppStore((s) => s.dmMode)

  if (dmMode) {
    return <DMView />
  }

  if (currentApp) {
    const panels: Record<string, React.ComponentType> = {
      studio: StudioApp,
      tasks: TasksApp,
      requests: RequestsApp,
      policies: PoliciesApp,
      calendar: CalendarApp,
      skills: SkillsApp,
      activity: ArtifactsApp,
      receipts: ReceiptsApp,
      'health-check': HealthCheckApp,
    }
    const Panel = panels[currentApp]
    return (
      <div className="app-panel active">
        {Panel ? <Panel /> : (
          <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'center', flex: 1, color: 'var(--text-tertiary)', fontSize: 14 }}>
            Unknown app: {currentApp}
          </div>
        )}
      </div>
    )
  }

  return (
    <>
      <MessageFeed />
      <TypingIndicator />
      <Composer />
    </>
  )
}

// ── App root ────────────────────────────────────────────────────
//
// Critical rules (violations caused the blank-page regression):
// 1. ALL hooks are called unconditionally at the top of App(). No early
//    returns before hook calls.
// 2. initApi() runs in an effect, but we render the shell immediately so
//    the user sees something even while init is pending.
// 3. ErrorBoundary wraps the whole tree so render errors are visible.

export default function App() {
  // --- All hooks first, in a fixed order, every render ---
  const [apiReady, setApiReady] = useState(false)
  const [showSplash, setShowSplash] = useState(false)
  const theme = useAppStore((s) => s.theme)
  const onboardingComplete = useAppStore((s) => s.onboardingComplete)
  const setBrokerConnected = useAppStore((s) => s.setBrokerConnected)
  const setOnboardingComplete = useAppStore((s) => s.setOnboardingComplete)

  useKeyboardShortcuts()

  // Load theme CSS when theme changes
  useEffect(() => {
    const existing = document.getElementById('theme-css') as HTMLLinkElement | null
    if (existing) {
      existing.href = `/themes/${theme}.css`
    } else {
      const el = document.createElement('link')
      el.id = 'theme-css'
      el.rel = 'stylesheet'
      el.href = `/themes/${theme}.css`
      document.head.appendChild(el)
    }
  }, [theme])

  // Init API and determine onboarding state
  useEffect(() => {
    let cancelled = false
    initApi()
      .then(() => {
        if (cancelled) return
        setBrokerConnected(true)
        return getHealth()
      })
      .then((h) => {
        if (cancelled || !h) return
        // If the broker has any agents, skip onboarding
        if (h.status === 'ok' || h.agents) {
          setOnboardingComplete(true)
        }
      })
      .catch(() => {
        // Broker unreachable — fall through to wizard
      })
      .finally(() => {
        if (!cancelled) setApiReady(true)
      })
    return () => {
      cancelled = true
    }
  }, [setBrokerConnected, setOnboardingComplete])

  // --- Render (no hooks past this point) ---

  let body: ReactNode
  if (!apiReady) {
    // The static skeleton in index.html already covers this case, but
    // render a matching React fallback so nothing flashes.
    body = (
      <div style={{
        height: '100vh',
        display: 'flex',
        alignItems: 'center',
        justifyContent: 'center',
        color: 'var(--text-tertiary)',
        fontSize: 14,
      }}>
        Connecting to broker...
      </div>
    )
  } else if (showSplash) {
    body = <SplashScreen onDone={() => setShowSplash(false)} />
  } else if (!onboardingComplete) {
    body = (
      <Wizard onComplete={() => {
        setShowSplash(true)
      }} />
    )
  } else {
    body = (
      <Shell>
        <MainContent />
      </Shell>
    )
  }

  return (
    <ErrorBoundary>
      <QueryClientProvider client={queryClient}>
        {body}
        <ToastContainer />
      </QueryClientProvider>
    </ErrorBoundary>
  )
}
