import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { renderHook, act } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import type { ReactNode } from 'react'
import React from 'react'
import { useKeyboardShortcuts } from './useKeyboardShortcuts'
import { useAppStore } from '../stores/app'

// `getChannels` is called when ⌘1..9 fires and the cache is cold. The
// cold-path is not the subject of these tests, so stub it out.
vi.mock('../api/client', async () => {
  const actual = await vi.importActual<typeof import('../api/client')>('../api/client')
  return {
    ...actual,
    getChannels: vi.fn().mockResolvedValue({ channels: [] }),
  }
})

function wrapper({ children }: { children: ReactNode }) {
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  return React.createElement(QueryClientProvider, { client: qc }, children)
}

function press(key: string, opts: Partial<KeyboardEventInit> & { targetTag?: string } = {}) {
  let target: EventTarget = window
  if (opts.targetTag) {
    const el = document.createElement(opts.targetTag)
    document.body.appendChild(el)
    target = el
  }
  const ev = new KeyboardEvent('keydown', {
    key,
    metaKey: opts.metaKey ?? false,
    ctrlKey: opts.ctrlKey ?? false,
    altKey: opts.altKey ?? false,
    shiftKey: opts.shiftKey ?? false,
    bubbles: true,
    cancelable: true,
  })
  Object.defineProperty(ev, 'target', { value: target, configurable: true })
  window.dispatchEvent(ev)
}

beforeEach(() => {
  useAppStore.setState({
    composerHelpOpen: false,
    searchOpen: false,
    activeAgentSlug: null,
    activeThreadId: null,
    onboardingComplete: true,
  })
})

afterEach(() => {
  while (document.body.firstChild) {
    document.body.removeChild(document.body.firstChild)
  }
})

describe('`?` opens the help modal', () => {
  it('toggles composerHelpOpen when not typing and onboarded', () => {
    renderHook(() => useKeyboardShortcuts(), { wrapper })
    act(() => press('?'))
    expect(useAppStore.getState().composerHelpOpen).toBe(true)
    act(() => press('?'))
    expect(useAppStore.getState().composerHelpOpen).toBe(false)
  })

  it('does not intercept `?` when focus is in an <input>', () => {
    renderHook(() => useKeyboardShortcuts(), { wrapper })
    act(() => press('?', { targetTag: 'input' }))
    expect(useAppStore.getState().composerHelpOpen).toBe(false)
  })

  it('does not intercept `?` when focus is in a <textarea>', () => {
    renderHook(() => useKeyboardShortcuts(), { wrapper })
    act(() => press('?', { targetTag: 'textarea' }))
    expect(useAppStore.getState().composerHelpOpen).toBe(false)
  })

  it('bails when onboarding is incomplete (avoids stale hidden state)', () => {
    useAppStore.setState({ onboardingComplete: false })
    renderHook(() => useKeyboardShortcuts(), { wrapper })
    act(() => press('?'))
    expect(useAppStore.getState().composerHelpOpen).toBe(false)
  })

  it('bails when a modifier key is held (so ⌘? / Ctrl? do not hijack)', () => {
    renderHook(() => useKeyboardShortcuts(), { wrapper })
    act(() => press('?', { metaKey: true }))
    expect(useAppStore.getState().composerHelpOpen).toBe(false)
    act(() => press('?', { ctrlKey: true }))
    expect(useAppStore.getState().composerHelpOpen).toBe(false)
    act(() => press('?', { altKey: true }))
    expect(useAppStore.getState().composerHelpOpen).toBe(false)
  })
})

describe('Escape priority', () => {
  it('closes composerHelpOpen before searchOpen', () => {
    useAppStore.setState({ composerHelpOpen: true, searchOpen: true })
    renderHook(() => useKeyboardShortcuts(), { wrapper })
    act(() => press('Escape'))
    expect(useAppStore.getState().composerHelpOpen).toBe(false)
    // searchOpen is untouched by this press — the handler returns after
    // the composerHelpOpen branch.
    expect(useAppStore.getState().searchOpen).toBe(true)
  })

  it('closes searchOpen once help is closed', () => {
    useAppStore.setState({ composerHelpOpen: false, searchOpen: true })
    renderHook(() => useKeyboardShortcuts(), { wrapper })
    act(() => press('Escape'))
    expect(useAppStore.getState().searchOpen).toBe(false)
  })

  it('closes activeAgentSlug once search is closed', () => {
    useAppStore.setState({ searchOpen: false, activeAgentSlug: 'ceo' })
    renderHook(() => useKeyboardShortcuts(), { wrapper })
    act(() => press('Escape'))
    expect(useAppStore.getState().activeAgentSlug).toBeNull()
  })
})

describe('⌘K command palette', () => {
  it('opens searchOpen on ⌘K', () => {
    renderHook(() => useKeyboardShortcuts(), { wrapper })
    act(() => press('k', { metaKey: true }))
    expect(useAppStore.getState().searchOpen).toBe(true)
  })

  it('opens searchOpen on Ctrl+K as well (Linux/Windows parity)', () => {
    renderHook(() => useKeyboardShortcuts(), { wrapper })
    act(() => press('k', { ctrlKey: true }))
    expect(useAppStore.getState().searchOpen).toBe(true)
  })
})
