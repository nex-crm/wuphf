import { useEffect, useState } from 'react'
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

function AppShell() {
  const onboardingComplete = useAppStore((s) => s.onboardingComplete)
  const brokerConnected = useAppStore((s) => s.brokerConnected)
  const [showSplash, setShowSplash] = useState(false)

  // If broker is connected, skip onboarding (office already running)
  useEffect(() => {
    if (brokerConnected) {
      getHealth().then((h) => {
        if (h.status === 'ok' || h.agents) {
          useAppStore.getState().setOnboardingComplete(true)
        }
      }).catch(() => {
        // Broker not responding — show onboarding
      })
    }
  }, [brokerConnected])

  if (showSplash) {
    return <SplashScreen onDone={() => setShowSplash(false)} />
  }

  if (!onboardingComplete) {
    return (
      <Wizard onComplete={() => {
        setShowSplash(true)
      }} />
    )
  }

  return (
    <Shell>
      <MainContent />
    </Shell>
  )
}

export default function App() {
  const [ready, setReady] = useState(false)
  const setBrokerConnected = useAppStore((s) => s.setBrokerConnected)
  const theme = useAppStore((s) => s.theme)

  useEffect(() => {
    // Load theme CSS
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

  useEffect(() => {
    initApi()
      .then(() => {
        setBrokerConnected(true)
        setReady(true)
      })
      .catch(() => {
        setReady(true)
      })
  }, [setBrokerConnected])

  if (!ready) {
    return (
      <div style={{
        height: '100vh',
        display: 'flex',
        alignItems: 'center',
        justifyContent: 'center',
        background: 'var(--bg)',
        color: 'var(--text-tertiary)',
        fontFamily: 'var(--font-sans)',
      }}>
        Loading...
      </div>
    )
  }

  return (
    <QueryClientProvider client={queryClient}>
      <AppShell />
    </QueryClientProvider>
  )
}
