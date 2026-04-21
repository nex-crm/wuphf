import { describe, expect, it, vi, beforeEach } from 'vitest'
import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import WikiEditor from './WikiEditor'
import * as api from '../../api/wiki'

describe('<WikiEditor>', () => {
  beforeEach(() => {
    vi.restoreAllMocks()
  })

  it('pre-fills the textarea with the article content and the expected SHA is sent on save', async () => {
    const spy = vi
      .spyOn(api, 'writeHumanArticle')
      .mockResolvedValue({ path: 'team/people/nazz.md', commit_sha: 'abc1234', bytes_written: 42 })

    const onSaved = vi.fn()
    render(
      <WikiEditor
        path="team/people/nazz.md"
        initialContent="# Nazz\n\nOriginal."
        expectedSha="deadbee"
        onSaved={onSaved}
        onCancel={() => {}}
      />,
    )

    const textarea = screen.getByTestId('wk-editor-textarea') as HTMLTextAreaElement
    expect(textarea.value).toContain('Original.')

    fireEvent.change(textarea, { target: { value: '# Nazz\n\nEdited.' } })
    fireEvent.change(screen.getByTestId('wk-editor-commit'), {
      target: { value: 'fix wording' },
    })
    fireEvent.click(screen.getByTestId('wk-editor-save'))

    await waitFor(() => expect(spy).toHaveBeenCalled())
    expect(spy).toHaveBeenCalledWith({
      path: 'team/people/nazz.md',
      content: '# Nazz\n\nEdited.',
      commitMessage: 'fix wording',
      expectedSha: 'deadbee',
    })
    await waitFor(() => expect(onSaved).toHaveBeenCalledWith('abc1234'))
  })

  it('shows the conflict banner when the server returns 409', async () => {
    vi.spyOn(api, 'writeHumanArticle').mockResolvedValue({
      conflict: true,
      error: 'wiki: article changed since it was opened',
      current_sha: 'newsha9',
      current_content: '# Nazz\n\nFresh text from someone else.',
    })

    render(
      <WikiEditor
        path="team/people/nazz.md"
        initialContent="# Nazz\n\nMine."
        expectedSha="oldsha1"
        onSaved={() => {}}
        onCancel={() => {}}
      />,
    )
    fireEvent.click(screen.getByTestId('wk-editor-save'))

    const banner = await screen.findByRole('alert')
    expect(banner.textContent).toMatch(/Someone else edited this article/)
    expect(
      screen.getByRole('button', { name: /Reload latest & re-apply/ }),
    ).toBeInTheDocument()
  })

  it('blocks save when the textarea is emptied', async () => {
    const spy = vi.spyOn(api, 'writeHumanArticle')
    render(
      <WikiEditor
        path="team/people/nazz.md"
        initialContent="# Nazz\n"
        expectedSha="abc"
        onSaved={() => {}}
        onCancel={() => {}}
      />,
    )
    fireEvent.change(screen.getByTestId('wk-editor-textarea'), { target: { value: '   ' } })
    fireEvent.click(screen.getByTestId('wk-editor-save'))
    expect(spy).not.toHaveBeenCalled()
    expect(await screen.findByRole('alert')).toHaveTextContent(/cannot be empty/i)
  })

  it('cancels via the Cancel button', () => {
    const onCancel = vi.fn()
    render(
      <WikiEditor
        path="team/people/nazz.md"
        initialContent="x"
        expectedSha="abc"
        onSaved={() => {}}
        onCancel={onCancel}
      />,
    )
    fireEvent.click(screen.getByRole('button', { name: 'Cancel' }))
    expect(onCancel).toHaveBeenCalled()
  })
})
