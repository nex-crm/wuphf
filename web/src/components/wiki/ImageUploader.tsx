import { useCallback, useRef, useState } from 'react'
import { uploadImage, type ImageUploadResult } from '../../api/images'

// ── Types ────────────────────────────────────────────────────────

export interface ImageUploaderProps {
  /**
   * Author slug credited in the git commit. Defaults to 'human' so the web
   * UI can upload without a configured agent identity.
   */
  authorSlug?: string
  /**
   * Called after a successful upload with the markdown snippet the user
   * should insert at their cursor, plus the full result for advanced
   * callers (entity briefs, notebook editors that auto-reference the
   * asset, etc.).
   */
  onUploaded: (markdown: string, result: ImageUploadResult) => void
  /** Called on upload failure so the host can surface the error. */
  onError?: (message: string) => void
  /** Size ceiling displayed in the drop hint. Default matches backend. */
  maxMB?: number
  /** Compact mode renders inline with the composer instead of full-width. */
  compact?: boolean
}

// Local optimistic preview — we show it the instant a file is selected so
// the UI doesn't feel like it's frozen during multi-hundred-KB uploads.
interface PreviewState {
  name: string
  url: string
  progress: number
  status: 'uploading' | 'done' | 'error'
  error?: string
}

const DEFAULT_MAX_MB = 10
const ACCEPTED_MIME = 'image/png,image/jpeg,image/webp,image/gif,image/svg+xml'

export function ImageUploader({
  authorSlug,
  onUploaded,
  onError,
  maxMB = DEFAULT_MAX_MB,
  compact = false,
}: ImageUploaderProps) {
  const inputRef = useRef<HTMLInputElement | null>(null)
  const [preview, setPreview] = useState<PreviewState | null>(null)
  const [isDragActive, setDragActive] = useState(false)

  const handleFile = useCallback(
    async (file: File) => {
      if (file.size > maxMB * 1024 * 1024) {
        const msg = `File exceeds ${maxMB} MB size limit.`
        setPreview({ name: file.name, url: '', progress: 0, status: 'error', error: msg })
        onError?.(msg)
        return
      }
      const localURL = URL.createObjectURL(file)
      setPreview({ name: file.name, url: localURL, progress: 0, status: 'uploading' })
      try {
        const result = await uploadImage(file, {
          authorSlug,
          onProgress: (f) =>
            setPreview((prev) => (prev ? { ...prev, progress: f } : prev)),
        })
        setPreview((prev) => (prev ? { ...prev, progress: 1, status: 'done' } : prev))
        // Revoke the object URL after a tick so the preview img still paints
        // during the swap to the server URL.
        setTimeout(() => URL.revokeObjectURL(localURL), 500)
        const alt = result.alt_path ? '' : file.name.replace(/\.[^.]+$/, '')
        const markdown = `![${alt}](${result.asset_path})`
        onUploaded(markdown, result)
      } catch (err) {
        const msg = err instanceof Error ? err.message : 'upload failed'
        setPreview({ name: file.name, url: localURL, progress: 0, status: 'error', error: msg })
        onError?.(msg)
      }
    },
    [authorSlug, maxMB, onError, onUploaded],
  )

  const onSelect = useCallback(
    (ev: React.ChangeEvent<HTMLInputElement>) => {
      const file = ev.target.files?.[0]
      if (file) void handleFile(file)
      // Reset so selecting the same file twice still fires onChange.
      ev.target.value = ''
    },
    [handleFile],
  )

  const onDrop = useCallback(
    (ev: React.DragEvent<HTMLDivElement>) => {
      ev.preventDefault()
      setDragActive(false)
      const file = ev.dataTransfer.files?.[0]
      if (file) void handleFile(file)
    },
    [handleFile],
  )

  return (
    <div
      className={`image-uploader${compact ? ' image-uploader--compact' : ''}${
        isDragActive ? ' image-uploader--drag' : ''
      }`}
      onDragOver={(e) => {
        e.preventDefault()
        setDragActive(true)
      }}
      onDragLeave={() => setDragActive(false)}
      onDrop={onDrop}
    >
      <input
        ref={inputRef}
        type="file"
        accept={ACCEPTED_MIME}
        onChange={onSelect}
        data-testid="image-uploader-input"
        style={{ display: 'none' }}
      />
      {preview ? (
        <div className="image-uploader__preview" aria-live="polite">
          {preview.url && (
            <img
              src={preview.url}
              alt=""
              className="image-uploader__thumb"
              width={120}
              height={90}
              loading="eager"
            />
          )}
          <div className="image-uploader__meta">
            <span className="image-uploader__name">{preview.name}</span>
            {preview.status === 'uploading' && (
              <div
                role="progressbar"
                aria-valuemin={0}
                aria-valuemax={100}
                aria-valuenow={Math.round(preview.progress * 100)}
                className="image-uploader__bar"
              >
                <span
                  className="image-uploader__bar-fill"
                  style={{ width: `${Math.round(preview.progress * 100)}%` }}
                />
              </div>
            )}
            {preview.status === 'done' && <span className="image-uploader__ok">Uploaded</span>}
            {preview.status === 'error' && (
              <span className="image-uploader__err" role="alert">
                {preview.error}
              </span>
            )}
          </div>
          {preview.status !== 'uploading' && (
            <button
              type="button"
              className="image-uploader__reset"
              onClick={() => {
                if (preview.url) URL.revokeObjectURL(preview.url)
                setPreview(null)
              }}
            >
              Clear
            </button>
          )}
        </div>
      ) : (
        <button
          type="button"
          className="image-uploader__drop"
          onClick={() => inputRef.current?.click()}
        >
          <span className="image-uploader__drop-label">
            {isDragActive ? 'Drop image to upload' : 'Drop image or click to browse'}
          </span>
          <span className="image-uploader__drop-hint">
            PNG, JPEG, WebP, GIF, or SVG · up to {maxMB} MB
          </span>
        </button>
      )}
    </div>
  )
}

export default ImageUploader
