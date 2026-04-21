import { afterEach, beforeEach, describe, expect, it } from 'vitest'

// We exercise the boot-error handling contract of main.tsx without
// actually importing the module (that would render the real App tree
// into jsdom and pull in the entire component graph). Instead we copy
// the catch-path contract here and pin it:
//
//   When React fails to mount, main.tsx must (a) render a fatal-error
//   overlay and (b) signal window.__wuphfBootDone so the 10s watchdog
//   in index.html does not fire a second, generic overlay on top.
//
// If this contract regresses, users who hit a real module-level error
// will lose the specific diagnostic 10 seconds later when the generic
// "Boot timeout" overlay replaces it.

type BootHandlers = {
  showFatal: (title: string, detail: string) => void
  signalBootDone: () => void
}

function runBootWithCrashingRender(handlers: BootHandlers) {
  // Simulates main.tsx's try/catch around createRoot().render().
  try {
    throw new Error('boom: App threw during initial render')
  } catch (err) {
    const message = err instanceof Error ? err.message : String(err)
    const stack = err instanceof Error && err.stack ? err.stack : ''
    handlers.showFatal('React failed to mount', message + '\n\n' + stack)
    // The contract under test: signal bootDone so the 10s watchdog is suppressed.
    handlers.signalBootDone()
  }
}

describe('main.tsx boot-error contract', () => {
  let bootDoneCalls: number
  let showFatalCalls: Array<{ title: string; detail: string }>
  let windowBootDoneCalls: number

  beforeEach(() => {
    bootDoneCalls = 0
    showFatalCalls = []
    windowBootDoneCalls = 0
    window.__wuphfBootDone = () => {
      windowBootDoneCalls += 1
    }
  })

  afterEach(() => {
    delete window.__wuphfBootDone
  })

  it('signals bootDone after showing the fatal overlay', () => {
    runBootWithCrashingRender({
      showFatal: (title, detail) => {
        showFatalCalls.push({ title, detail })
      },
      signalBootDone: () => {
        bootDoneCalls += 1
        window.__wuphfBootDone?.()
      },
    })

    expect(showFatalCalls).toHaveLength(1)
    expect(showFatalCalls[0].title).toBe('React failed to mount')
    expect(bootDoneCalls).toBe(1)
    expect(windowBootDoneCalls).toBe(1)
  })

  it('calls fatal before bootDone so the overlay shows before any watchdog check', () => {
    const order: string[] = []
    runBootWithCrashingRender({
      showFatal: () => order.push('fatal'),
      signalBootDone: () => order.push('bootDone'),
    })
    expect(order).toEqual(['fatal', 'bootDone'])
  })
})

// Mirror of the logic this PR lands in the inline <script> in index.html.
// index.html cannot be unit-tested directly, so we pin the contract in JS
// here. If the watchdog in index.html drifts from this, the diagnostics UX
// regresses and users see "Boot timeout" on top of real errors.
describe('index.html watchdog contract (simulated)', () => {
  function installWatchdog(win: Window & typeof globalThis) {
    const state = { bootDone: false }
    const showFatal = (title: string, detail: string) => {
      if (state.bootDone) return
      // First overlay wins. If one already exists, do NOT remove it.
      if (win.document.getElementById('fatal-error')) return
      const box = win.document.createElement('div')
      box.id = 'fatal-error'
      const h = win.document.createElement('h2')
      h.textContent = title
      box.appendChild(h)
      if (detail) {
        const pre = win.document.createElement('pre')
        pre.textContent = detail
        box.appendChild(pre)
      }
      win.document.body.appendChild(box)
      state.bootDone = true
    }
    win.__wuphfBootDone = () => {
      state.bootDone = true
    }
    const fireWatchdog = () => {
      if (state.bootDone) return
      if (win.document.getElementById('fatal-error')) return
      const root = win.document.getElementById('root')
      if (root && root.children.length === 1 && root.children[0].id === 'skeleton') {
        const detail =
          'React did not mount after 10 seconds.\n' +
          'readyState=' +
          win.document.readyState +
          ' hash=' +
          win.location.hash
        showFatal('Boot timeout', detail)
      }
    }
    return { showFatal, fireWatchdog }
  }

  function mountSkeleton() {
    const root = document.createElement('div')
    root.id = 'root'
    const skel = document.createElement('div')
    skel.id = 'skeleton'
    root.appendChild(skel)
    document.body.appendChild(root)
  }

  beforeEach(() => {
    // Clear the body using DOM methods (no innerHTML) to stay within the
    // project's XSS guardrails, then install the skeleton that the
    // watchdog expects to still be present at 10s.
    while (document.body.firstChild) {
      document.body.removeChild(document.body.firstChild)
    }
    mountSkeleton()
  })

  afterEach(() => {
    const existing = document.getElementById('fatal-error')
    if (existing) existing.remove()
    delete window.__wuphfBootDone
    window.location.hash = ''
  })

  it('first overlay wins — watchdog does not overwrite a specific error', () => {
    const { showFatal, fireWatchdog } = installWatchdog(window)
    showFatal('Uncaught error: specific', 'stack trace here')
    fireWatchdog()

    const overlays = document.querySelectorAll('#fatal-error')
    expect(overlays).toHaveLength(1)
    expect(overlays[0].querySelector('h2')?.textContent).toBe(
      'Uncaught error: specific',
    )
  })

  it('bootDone signal suppresses the 10s watchdog', () => {
    const { fireWatchdog } = installWatchdog(window)
    window.__wuphfBootDone?.()
    fireWatchdog()

    expect(document.getElementById('fatal-error')).toBeNull()
  })

  it('boot-timeout detail includes readyState and hash for debuggability', () => {
    const { fireWatchdog } = installWatchdog(window)
    window.location.hash = '#/wiki/companies/acme'
    fireWatchdog()

    const detail = document.querySelector('#fatal-error pre')?.textContent ?? ''
    expect(detail).toContain('readyState=')
    expect(detail).toContain('hash=#/wiki/companies/acme')
  })

  it('watchdog fires when skeleton is still present and bootDone is false', () => {
    const { fireWatchdog } = installWatchdog(window)
    fireWatchdog()

    const h = document.querySelector('#fatal-error h2')
    expect(h?.textContent).toBe('Boot timeout')
  })
})
