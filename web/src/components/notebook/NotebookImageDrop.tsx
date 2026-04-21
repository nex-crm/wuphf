import { useCallback, useState } from 'react'
import ImageUploader from '../wiki/ImageUploader'
import type { ImageUploadResult } from '../../api/images'

// NotebookImageDrop wraps ImageUploader for the v1.3 notebook surface.
// There is no inline markdown editor in the notebook UI yet (that's a
// future phase), so this component copies the generated markdown snippet
// to the clipboard on success and shows a toast so the user can paste it
// into their editor of choice — terminal, IDE, whatever.

interface NotebookImageDropProps {
  agentSlug: string
}

interface ToastState {
  message: string
  kind: 'ok' | 'err'
}

export default function NotebookImageDrop({ agentSlug }: NotebookImageDropProps) {
  const [toast, setToast] = useState<ToastState | null>(null)

  const onUploaded = useCallback(async (markdown: string, result: ImageUploadResult) => {
    try {
      await navigator.clipboard.writeText(markdown)
      setToast({
        message: `Copied markdown for ${result.asset_path.split('/').pop()} to clipboard`,
        kind: 'ok',
      })
    } catch {
      setToast({
        message: `Uploaded ${result.asset_path} — paste \`${markdown}\` into your entry`,
        kind: 'ok',
      })
    }
    setTimeout(() => setToast(null), 4000)
  }, [])

  const onError = useCallback((msg: string) => {
    setToast({ message: msg, kind: 'err' })
    setTimeout(() => setToast(null), 6000)
  }, [])

  return (
    <div className="nb-image-drop">
      <div className="nb-image-drop__label">Attach a screenshot or diagram</div>
      <ImageUploader
        authorSlug={agentSlug}
        onUploaded={onUploaded}
        onError={onError}
        compact
      />
      {toast && (
        <div
          className={`nb-image-drop__toast nb-image-drop__toast--${toast.kind}`}
          role="status"
          aria-live="polite"
        >
          {toast.message}
        </div>
      )}
    </div>
  )
}
