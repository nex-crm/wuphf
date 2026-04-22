import { Component, useEffect, useState, type ComponentType, type ReactNode } from 'react'
import { initApi, get } from './api/client'
import { useAppStore, isDMChannel } from './stores/app'
import { Shell } from './components/layout/Shell'
import { MessageFeed } from './components/messages/MessageFeed'
import { Composer } from './components/messages/Composer'
import { TypingIndicator } from './components/messages/TypingIndicator'
import { DMView } from './components/messages/DMView'
import { TasksApp } from './components/apps/TasksApp'
import { RequestsApp } from './components/apps/RequestsApp'
import { GraphApp } from './components/apps/GraphApp'
import { PoliciesApp } from './components/apps/PoliciesApp'
import { CalendarApp } from './components/apps/CalendarApp'
import { SkillsApp } from './components/apps/SkillsApp'
import { ArtifactsApp } from './components/apps/ArtifactsApp'
import { ReceiptsApp } from './components/apps/ReceiptsApp'
import { HealthCheckApp } from './components/apps/HealthCheckApp'
import { SettingsApp } from './components/apps/SettingsApp'
import { ThreadsApp } from './components/apps/ThreadsApp'
import Wiki from './components/wiki/Wiki'
import WikiTabs from './components/wiki/WikiTabs'
import type { WikiTab } from './components/wiki/WikiTabs'
import Notebook from './components/notebook/Notebook'
import ReviewQueueKanban from './components/review/ReviewQueueKanban'
import { Wizard } from './components/onboarding/Wizard'
import { AgentPanel } from './components/agents/AgentPanel'
import { SearchModal } from './components/search/SearchModal'
import { InterviewBar } from './components/messages/InterviewBar'
import { DisconnectBanner } from './components/layout/DisconnectBanner'
import { SplashScreen } from './components/onboarding/SplashScreen'
import { ToastContainer } from './components/ui/Toast'
import { ConfirmHost } from './components/ui/ConfirmDialog'
import { ProviderSwitcherHost } from './components/ui/ProviderSwitcher'
import { useKeyboardShortcuts } from './hooks/useKeyboardShortcuts'
import { useHashRouter } from './hooks/useHashRouter'
import { useBrokerEvents } from './hooks/useBrokerEvents'
import './styles/shadcn.css'
import './styles/global.css'
import './styles/layout.css'
import './styles/messages.css'
import './styles/agents.css'
import './styles/search.css'
import './styles/wiki-shell.css'
import './styles/kbd.css'

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
        <div
          data-testid="error-boundary"
          style={{
            position: 'fixed', top: 0, left: 0, right: 0, bottom: 0,
            background: '#fee', color: '#900', padding: 20,
            fontFamily: '-apple-system, BlinkMacSystemFont, sans-serif',
            fontSize: 13, overflowY: 'auto', zIndex: 9999,
          }}
        >
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
  const currentChannel = useAppStore((s) => s.currentChannel)
  const channelMeta = useAppStore((s) => s.channelMeta)
  const wikiPath = useAppStore((s) => s.wikiPath)
  const setWikiPath = useAppStore((s) => s.setWikiPath)
  const setCurrentApp = useAppStore((s) => s.setCurrentApp)
  const notebookAgentSlug = useAppStore((s) => s.notebookAgentSlug)
  const notebookEntrySlug = useAppStore((s) => s.notebookEntrySlug)
  const setNotebookRoute = useAppStore((s) => s.setNotebookRoute)

  if (!currentApp && isDMChannel(currentChannel, channelMeta)) {
    return <DMView />
  }

  // Wiki, Notebooks, and Reviews share one app shell with a tab bar on top.
  // The surfaces underneath keep their own design systems (.wiki-surface vs
  // .notebook-surface) but the parent chrome is unified.
  if (currentApp === 'wiki' || currentApp === 'notebooks' || currentApp === 'reviews') {
    const handleTabChange = (tab: WikiTab) => {
      if (tab === 'wiki') {
        setCurrentApp('wiki')
      } else if (tab === 'notebooks') {
        setNotebookRoute(null, null)
        setCurrentApp('notebooks')
      } else {
        setCurrentApp('reviews')
      }
    }

    return (
      <div className="wiki-shell">
        <WikiTabs current={currentApp} onSelect={handleTabChange} />
        <div className="wiki-shell-body">
          {currentApp === 'wiki' && (
            <Wiki
              articlePath={wikiPath}
              onNavigate={(path) => {
                if (path === null) {
                  setWikiPath(null)
                } else {
                  setWikiPath(path || null)
                }
              }}
            />
          )}
          {currentApp === 'notebooks' && (
            <Notebook
              agentSlug={notebookAgentSlug}
              entrySlug={notebookEntrySlug}
              onOpenCatalog={() => setNotebookRoute(null, null)}
              onOpenAgent={(slug) => setNotebookRoute(slug, null)}
              onOpenEntry={(slug, entry) => setNotebookRoute(slug, entry)}
              onNavigateWiki={(path) => {
                setCurrentApp('wiki')
                setWikiPath(path || null)
              }}
            />
          )}
          {currentApp === 'reviews' && <ReviewQueueKanban />}
        </div>
      </div>
    )
  }

  if (currentApp) {
    const panels: Record<string, ComponentType> = {
      tasks: TasksApp,
      requests: RequestsApp,
      graph: GraphApp,
      policies: PoliciesApp,
      calendar: CalendarApp,
      skills: SkillsApp,
      activity: ArtifactsApp,
      receipts: ReceiptsApp,
      'health-check': HealthCheckApp,
      settings: SettingsApp,
      threads: ThreadsApp,
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
      <InterviewBar />
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
  useHashRouter()
  useBrokerEvents(apiReady)

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

  // Init API and determine onboarding state.
  // Source of truth: GET /onboarding/state.onboarded (backed by ~/.wuphf/onboarded.json).
  // Broker health / default agents must not skip the wizard — the broker seeds 7
  // default agents on every boot, so a health-based check was making the wizard
  // permanently unreachable for fresh installs.
  useEffect(() => {
    let cancelled = false
    initApi()
      .then(() => {
        if (cancelled) return
        setBrokerConnected(true)
        return get<{ onboarded?: boolean }>('/onboarding/state')
      })
      .then((s) => {
        if (cancelled || !s) return
        if (s.onboarded === true) {
          setOnboardingComplete(true)
        }
      })
      .catch(() => {
        // Endpoint unreachable — fall through to wizard. Safer default for
        // fresh installs where the broker may not have mounted onboarding yet.
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
      {body}
      <ToastContainer />
      <ConfirmHost />
      <ProviderSwitcherHost />
    </ErrorBoundary>
  )
}
