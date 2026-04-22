import { describe, expect, it, vi } from 'vitest'
import { render } from '@testing-library/react'
import { ChannelList } from './ChannelList'
import { MOD_KEY } from '../ui/Kbd'

// Fake channel factory — the store subscriptions are satisfied by the
// default zustand values set elsewhere; we only need to supply the
// channels query result.
const mkChannels = (n: number) =>
  Array.from({ length: n }, (_, i) => ({
    slug: `c${i + 1}`,
    name: `Channel ${i + 1}`,
  }))

vi.mock('../../hooks/useChannels', () => ({
  useChannels: vi.fn(),
}))

vi.mock('../../hooks/useOverflow', () => ({
  useOverflow: () => ({ current: null }),
}))

// ChannelWizard reaches into the app + a trigger portal we don't care
// about in these tests. Stub the wizard hook to return a no-op shape.
vi.mock('../channels/ChannelWizard', () => ({
  ChannelWizard: () => null,
  useChannelWizard: () => ({ open: false, show: () => {}, hide: () => {} }),
}))

import { useChannels } from '../../hooks/useChannels'
const useChannelsMock = vi.mocked(useChannels)

function setChannels(count: number) {
  useChannelsMock.mockReturnValue({
    data: mkChannels(count),
    isLoading: false,
    isError: false,
    error: null,
  } as unknown as ReturnType<typeof useChannels>)
}

describe('<ChannelList> keyboard shortcut badges', () => {
  it('renders a ⌘N badge on exactly the first 9 channels when there are more than 9', () => {
    setChannels(12)
    const { container } = render(<ChannelList />)
    const badges = container.querySelectorAll('.sidebar-shortcut')
    expect(badges.length).toBe(9)
  })

  it('renders a badge on every channel when there are ≤ 9', () => {
    setChannels(5)
    const { container } = render(<ChannelList />)
    const badges = container.querySelectorAll('.sidebar-shortcut')
    expect(badges.length).toBe(5)
  })

  it('renders no badge when there are 0 channels', () => {
    setChannels(0)
    const { container } = render(<ChannelList />)
    expect(container.querySelectorAll('.sidebar-shortcut').length).toBe(0)
  })

  it('uses MOD_KEY + (index + 1) as the badge text', () => {
    setChannels(3)
    const { container } = render(<ChannelList />)
    const badges = Array.from(container.querySelectorAll('.sidebar-shortcut .kbd'))
    expect(badges.map((b) => b.textContent)).toEqual([
      `${MOD_KEY}1`,
      `${MOD_KEY}2`,
      `${MOD_KEY}3`,
    ])
  })

  it('includes the shortcut in the title attribute for the first 9 and not beyond', () => {
    setChannels(11)
    const { container } = render(<ChannelList />)
    const buttons = Array.from(container.querySelectorAll('button.sidebar-item'))
      .filter((b) => b.querySelector('.sidebar-item-label')) as HTMLElement[]
    expect(buttons[0].title).toContain(`${MOD_KEY}1`)
    expect(buttons[8].title).toContain(`${MOD_KEY}9`)
    // The 10th and 11th channels show only the plain name/slug.
    expect(buttons[9].title).not.toContain(MOD_KEY)
    expect(buttons[10].title).not.toContain(MOD_KEY)
  })

  it('marks the shortcut span aria-hidden so screen readers skip the decorative glyph', () => {
    setChannels(3)
    const { container } = render(<ChannelList />)
    const spans = container.querySelectorAll('.sidebar-shortcut')
    spans.forEach((s) => expect(s.getAttribute('aria-hidden')).toBe('true'))
  })
})
