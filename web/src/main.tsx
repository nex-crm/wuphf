import { createRoot } from 'react-dom/client'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import App from './App'

// Hoisted out of App() so hooks called *during* App's render (e.g.
// useKeyboardShortcuts → useQueryClient) find a client. Previously the
// provider lived inside App's return tree, which meant any useQueryClient
// call at the top of App threw "No QueryClient set" before the provider
// got a chance to mount.
const queryClient = new QueryClient({
  defaultOptions: {
    queries: { retry: 1, staleTime: 2000 },
  },
})

// Signal boot-done to the index.html fatal-error handler when main.tsx loads.
declare global {
  interface Window {
    __wuphfBootDone?: () => void
  }
}

function showFatalError(title: string, detail: string) {
  const existing = document.getElementById('fatal-error')
  if (existing) existing.remove()
  const box = document.createElement('div')
  box.id = 'fatal-error'
  box.style.cssText = 'position:fixed;top:0;left:0;right:0;padding:16px 20px;background:#fee;color:#900;font-family:-apple-system,BlinkMacSystemFont,sans-serif;font-size:13px;border-bottom:2px solid #900;z-index:10000;white-space:pre-wrap;word-break:break-word;max-height:50vh;overflow-y:auto;'
  const h = document.createElement('h2')
  h.textContent = title
  h.style.cssText = 'margin:0 0 8px 0;font-size:14px;'
  box.appendChild(h)
  const pre = document.createElement('pre')
  pre.textContent = detail
  pre.style.cssText = 'margin:8px 0 0;font-family:SFMono-Regular,Menlo,monospace;font-size:11px;color:#600;'
  box.appendChild(pre)
  document.body.appendChild(box)
}

try {
  const root = document.getElementById('root')
  if (!root) {
    throw new Error('#root element not found in DOM')
  }
  createRoot(root).render(
    <QueryClientProvider client={queryClient}>
      <App />
    </QueryClientProvider>,
  )
  // Tell the HTML-level timeout handler that we're alive.
  window.__wuphfBootDone?.()
} catch (err) {
  const message = err instanceof Error ? err.message : String(err)
  const stack = err instanceof Error && err.stack ? err.stack : ''
  showFatalError('React failed to mount', message + '\n\n' + stack)
  // Also log so `$B console` picks it up
  // eslint-disable-next-line no-console
  console.error('[WUPHF boot]', err)
}
