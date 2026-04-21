import { useEffect, useRef, useState } from 'react'
import { writeHumanArticle, type WriteHumanConflict } from '../../api/wiki'

interface WikiEditorProps {
  /** Target article path, e.g. `team/people/nazz.md`. */
  path: string
  /** Markdown the editor starts with (article.content when present). */
  initialContent: string
  /** SHA the editor opened against; sent back as expected_sha on save. */
  expectedSha: string
  /** Called after a successful save so the parent can refetch. */
  onSaved: (newSha: string) => void
  /** Called when the user cancels. */
  onCancel: () => void
}

/**
 * Plain-markdown editor surface. Wikipedia's "Edit source" button maps
 * to this component: a textarea + commit-message input + save/cancel.
 *
 * Rich-text preview and autosave are deferred to v1.1; this ships the
 * minimum path that lets the founder fix a typo without shelling into
 * the .wuphf directory.
 */
export default function WikiEditor({
  path,
  initialContent,
  expectedSha,
  onSaved,
  onCancel,
}: WikiEditorProps) {
  const [content, setContent] = useState(initialContent)
  const [commitMessage, setCommitMessage] = useState('')
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [conflict, setConflict] = useState<WriteHumanConflict | null>(null)
  const textareaRef = useRef<HTMLTextAreaElement | null>(null)

  useEffect(() => {
    // Fresh content whenever the article or opened revision changes.
    setContent(initialContent)
    setCommitMessage('')
    setError(null)
    setConflict(null)
  }, [path, expectedSha, initialContent])

  async function handleSave() {
    if (saving) return
    setError(null)
    setConflict(null)
    if (!content.trim()) {
      setError('Article content cannot be empty.')
      return
    }
    setSaving(true)
    try {
      const result = await writeHumanArticle({
        path,
        content,
        commitMessage: commitMessage.trim() || `human: update ${path}`,
        expectedSha,
      })
      if ('conflict' in result) {
        setConflict(result)
        return
      }
      onSaved(result.commit_sha)
    } catch (err: unknown) {
      setError(err instanceof Error ? err.message : 'Save failed.')
    } finally {
      setSaving(false)
    }
  }

  function handleReloadConflict() {
    if (!conflict) return
    setContent(conflict.current_content)
    // Hand the new SHA back to the parent so it re-fetches and re-opens
    // the editor against the latest article rev.
    onSaved(conflict.current_sha)
  }

  return (
    <div className="wk-editor" data-testid="wk-editor">
      {conflict && (
        <div className="wk-editor-banner wk-editor-banner--conflict" role="alert">
          <strong>Someone else edited this article.</strong> Your save was
          rejected because the article changed since you opened it.
          <div className="wk-editor-banner-actions">
            <button type="button" onClick={handleReloadConflict}>
              Reload latest &amp; re-apply
            </button>
          </div>
        </div>
      )}
      {error && !conflict && (
        <div className="wk-editor-banner wk-editor-banner--error" role="alert">
          {error}
        </div>
      )}
      <label className="wk-editor-label" htmlFor="wk-editor-textarea">
        Article source ({path})
      </label>
      <textarea
        id="wk-editor-textarea"
        ref={textareaRef}
        className="wk-editor-textarea"
        data-testid="wk-editor-textarea"
        value={content}
        onChange={(e) => setContent(e.target.value)}
        spellCheck
        rows={28}
      />
      <label className="wk-editor-label" htmlFor="wk-editor-commit-msg">
        Edit summary
      </label>
      <input
        id="wk-editor-commit-msg"
        className="wk-editor-commit"
        data-testid="wk-editor-commit"
        type="text"
        placeholder="human: short description of the edit"
        value={commitMessage}
        onChange={(e) => setCommitMessage(e.target.value)}
      />
      <div className="wk-editor-actions">
        <button
          type="button"
          className="wk-editor-save"
          data-testid="wk-editor-save"
          onClick={handleSave}
          disabled={saving}
        >
          {saving ? 'Saving…' : 'Save changes'}
        </button>
        <button
          type="button"
          className="wk-editor-cancel"
          onClick={onCancel}
          disabled={saving}
        >
          Cancel
        </button>
      </div>
      <p className="wk-editor-help">
        Plain markdown. <code>[[slug]]</code> creates a wikilink. Saved as
        commit author <strong>Human &lt;human@wuphf.local&gt;</strong>.
      </p>
    </div>
  )
}
