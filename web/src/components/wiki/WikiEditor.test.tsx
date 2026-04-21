import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { act, fireEvent, render, screen, waitFor } from '@testing-library/react'
import WikiEditor from './WikiEditor'
import * as api from '../../api/wiki'

const PATH = 'team/people/nazz.md'
const DRAFT_KEY = `wuphf:draft:${PATH}`
const SERVER_TS = '2026-04-20T10:00:00.000Z'

function setLocalStorageDraft(
  content: string,
  summary: string,
  savedAt: string,
) {
  window.localStorage.setItem(
    DRAFT_KEY,
    JSON.stringify({ content, summary, saved_at: savedAt }),
  )
}

describe('<WikiEditor>', () => {
  beforeEach(() => {
    vi.restoreAllMocks()
    window.localStorage.clear()
  })

  afterEach(() => {
    vi.useRealTimers()
  })

  it('pre-fills the textarea with the article content and the expected SHA is sent on save', async () => {
    const spy = vi
      .spyOn(api, 'writeHumanArticle')
      .mockResolvedValue({
        path: PATH,
        commit_sha: 'abc1234',
        bytes_written: 42,
      })

    const onSaved = vi.fn()
    render(
      <WikiEditor
        path={PATH}
        initialContent="# Nazz\n\nOriginal."
        expectedSha="deadbee"
        serverLastEditedTs={SERVER_TS}
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
      path: PATH,
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
        path={PATH}
        initialContent="# Nazz\n\nMine."
        expectedSha="oldsha1"
        serverLastEditedTs={SERVER_TS}
        onSaved={() => {}}
        onCancel={() => {}}
      />,
    )
    fireEvent.click(screen.getByTestId('wk-editor-save'))

    const banner = await screen.findByText(/Someone else edited this article/)
    expect(banner).toBeInTheDocument()
    expect(
      screen.getByRole('button', { name: /Reload latest & re-apply/ }),
    ).toBeInTheDocument()
  })

  it('blocks save when the textarea is emptied', async () => {
    const spy = vi.spyOn(api, 'writeHumanArticle')
    render(
      <WikiEditor
        path={PATH}
        initialContent="# Nazz\n"
        expectedSha="abc"
        serverLastEditedTs={SERVER_TS}
        onSaved={() => {}}
        onCancel={() => {}}
      />,
    )
    fireEvent.change(screen.getByTestId('wk-editor-textarea'), {
      target: { value: '   ' },
    })
    fireEvent.click(screen.getByTestId('wk-editor-save'))
    expect(spy).not.toHaveBeenCalled()
    expect(await screen.findByRole('alert')).toHaveTextContent(/cannot be empty/i)
  })

  it('cancels via the Cancel button', () => {
    const onCancel = vi.fn()
    render(
      <WikiEditor
        path={PATH}
        initialContent="x"
        expectedSha="abc"
        serverLastEditedTs={SERVER_TS}
        onSaved={() => {}}
        onCancel={onCancel}
      />,
    )
    fireEvent.click(screen.getByRole('button', { name: 'Cancel' }))
    expect(onCancel).toHaveBeenCalled()
  })

  // ── Draft autosave ─────────────────────────────────────────────────

  it('writes a debounced draft to localStorage after edits', async () => {
    vi.useFakeTimers()
    render(
      <WikiEditor
        path={PATH}
        initialContent="Original."
        expectedSha="abc"
        serverLastEditedTs={SERVER_TS}
        onSaved={() => {}}
        onCancel={() => {}}
      />,
    )
    fireEvent.change(screen.getByTestId('wk-editor-textarea'), {
      target: { value: 'Edited locally.' },
    })
    // Before the debounce fires, nothing is persisted.
    expect(window.localStorage.getItem(DRAFT_KEY)).toBeNull()
    // Advance past the debounce window.
    await act(async () => {
      vi.advanceTimersByTime(800)
    })
    const raw = window.localStorage.getItem(DRAFT_KEY)
    expect(raw).not.toBeNull()
    const parsed = JSON.parse(raw as string)
    expect(parsed.content).toBe('Edited locally.')
  })

  it('shows the draft banner when localStorage has a newer draft than the server', () => {
    const tenMinAfterServer = new Date(
      Date.parse(SERVER_TS) + 10 * 60 * 1000,
    ).toISOString()
    setLocalStorageDraft('# Nazz\n\nDraft text.', 'wip', tenMinAfterServer)

    render(
      <WikiEditor
        path={PATH}
        initialContent="# Nazz\n\nOriginal."
        expectedSha="abc"
        serverLastEditedTs={SERVER_TS}
        onSaved={() => {}}
        onCancel={() => {}}
      />,
    )
    expect(screen.getByTestId('wk-editor-draft-banner')).toBeInTheDocument()
  })

  it('restore copies the draft into the textarea and hides the banner', () => {
    const tenMinAfter = new Date(
      Date.parse(SERVER_TS) + 10 * 60 * 1000,
    ).toISOString()
    setLocalStorageDraft('# Nazz\n\nDraft text.', 'wip summary', tenMinAfter)

    render(
      <WikiEditor
        path={PATH}
        initialContent="# Nazz\n\nOriginal."
        expectedSha="abc"
        serverLastEditedTs={SERVER_TS}
        onSaved={() => {}}
        onCancel={() => {}}
      />,
    )
    fireEvent.click(screen.getByTestId('wk-editor-draft-restore'))
    const textarea = screen.getByTestId('wk-editor-textarea') as HTMLTextAreaElement
    expect(textarea.value).toBe('# Nazz\n\nDraft text.')
    const commit = screen.getByTestId('wk-editor-commit') as HTMLInputElement
    expect(commit.value).toBe('wip summary')
    expect(screen.queryByTestId('wk-editor-draft-banner')).toBeNull()
  })

  it('discard clears the draft from localStorage and hides the banner', () => {
    const tenMinAfter = new Date(
      Date.parse(SERVER_TS) + 10 * 60 * 1000,
    ).toISOString()
    setLocalStorageDraft('draft body', '', tenMinAfter)

    render(
      <WikiEditor
        path={PATH}
        initialContent="original body"
        expectedSha="abc"
        serverLastEditedTs={SERVER_TS}
        onSaved={() => {}}
        onCancel={() => {}}
      />,
    )
    fireEvent.click(screen.getByTestId('wk-editor-draft-discard'))
    expect(window.localStorage.getItem(DRAFT_KEY)).toBeNull()
    expect(screen.queryByTestId('wk-editor-draft-banner')).toBeNull()
  })

  it('hides the banner when the server is newer than the stored draft', () => {
    // Draft saved before the server timestamp — the server won; draft stale.
    const oldDraft = new Date(
      Date.parse(SERVER_TS) - 10 * 60 * 1000,
    ).toISOString()
    setLocalStorageDraft('stale draft', '', oldDraft)

    render(
      <WikiEditor
        path={PATH}
        initialContent="server wins"
        expectedSha="abc"
        serverLastEditedTs={SERVER_TS}
        onSaved={() => {}}
        onCancel={() => {}}
      />,
    )
    expect(screen.queryByTestId('wk-editor-draft-banner')).toBeNull()
    // Stale draft should also be cleared.
    expect(window.localStorage.getItem(DRAFT_KEY)).toBeNull()
  })

  it('successful save clears the draft from localStorage', async () => {
    const tenMinAfter = new Date(
      Date.parse(SERVER_TS) + 10 * 60 * 1000,
    ).toISOString()
    setLocalStorageDraft('edited body', 'msg', tenMinAfter)
    vi.spyOn(api, 'writeHumanArticle').mockResolvedValue({
      path: PATH,
      commit_sha: 'abc1234',
      bytes_written: 5,
    })

    render(
      <WikiEditor
        path={PATH}
        initialContent="original body"
        expectedSha="abc"
        serverLastEditedTs={SERVER_TS}
        onSaved={() => {}}
        onCancel={() => {}}
      />,
    )
    // Restore so content is non-empty + recognized as a real draft.
    fireEvent.click(screen.getByTestId('wk-editor-draft-restore'))
    fireEvent.click(screen.getByTestId('wk-editor-save'))
    await waitFor(() =>
      expect(window.localStorage.getItem(DRAFT_KEY)).toBeNull(),
    )
  })

  it('409 conflict keeps the draft in localStorage', async () => {
    const tenMinAfter = new Date(
      Date.parse(SERVER_TS) + 10 * 60 * 1000,
    ).toISOString()
    setLocalStorageDraft('my body', 'my summary', tenMinAfter)
    vi.spyOn(api, 'writeHumanArticle').mockResolvedValue({
      conflict: true,
      error: 'wiki: article changed since it was opened',
      current_sha: 'newsha9',
      current_content: 'other body',
    })

    render(
      <WikiEditor
        path={PATH}
        initialContent="original body"
        expectedSha="abc"
        serverLastEditedTs={SERVER_TS}
        onSaved={() => {}}
        onCancel={() => {}}
      />,
    )
    fireEvent.click(screen.getByTestId('wk-editor-draft-restore'))
    fireEvent.click(screen.getByTestId('wk-editor-save'))
    await screen.findByText(/Someone else edited this article/)
    // The draft must survive so the user's work isn't lost.
    const raw = window.localStorage.getItem(DRAFT_KEY)
    expect(raw).not.toBeNull()
    expect(JSON.parse(raw as string).content).toBe('my body')
  })

  // ── Preview pane ───────────────────────────────────────────────────

  it('preview toggle renders markdown with wikilinks via the shared pipeline', () => {
    render(
      <WikiEditor
        path={PATH}
        initialContent="See [[people/sarah-chen|Sarah]] for context."
        expectedSha="abc"
        serverLastEditedTs={SERVER_TS}
        catalog={[
          {
            path: 'people/sarah-chen',
            title: 'Sarah',
            author_slug: 'ceo',
            last_edited_ts: SERVER_TS,
            group: 'people',
          },
        ]}
        onSaved={() => {}}
        onCancel={() => {}}
      />,
    )
    // Preview is off initially.
    expect(screen.queryByTestId('wk-editor-preview')).toBeNull()
    fireEvent.click(screen.getByTestId('wk-editor-preview-toggle'))
    const preview = screen.getByTestId('wk-editor-preview')
    expect(preview).toBeInTheDocument()
    // The wikilink in the preview resolves (not broken) and carries the
    // shared pipeline's data-wikilink attribute.
    const link = preview.querySelector('a[data-wikilink="true"]')
    expect(link).not.toBeNull()
    expect(link?.getAttribute('data-broken')).toBe('false')
    expect(link?.textContent).toBe('Sarah')
  })

  it('preview renders image markdown through ImageEmbed', () => {
    render(
      <WikiEditor
        path={PATH}
        initialContent={'![logo](https://cdn.example.com/logo.png)'}
        expectedSha="abc"
        serverLastEditedTs={SERVER_TS}
        onSaved={() => {}}
        onCancel={() => {}}
      />,
    )
    fireEvent.click(screen.getByTestId('wk-editor-preview-toggle'))
    const preview = screen.getByTestId('wk-editor-preview')
    // ImageEmbed wraps the <img> in a <figure class="image-embed">.
    expect(preview.querySelector('figure.image-embed')).not.toBeNull()
    const img = preview.querySelector('img') as HTMLImageElement | null
    expect(img?.getAttribute('src')).toBe('https://cdn.example.com/logo.png')
    expect(img?.getAttribute('referrerpolicy')).toBe('no-referrer')
  })
})
