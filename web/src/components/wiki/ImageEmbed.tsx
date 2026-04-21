import { useCallback, useEffect, useRef, useState } from 'react'
import { assetURL, fetchAlt } from '../../api/images'

// ── Types ────────────────────────────────────────────────────────

export interface ImageEmbedProps {
  /**
   * Wiki-relative path like `team/assets/202604/abcdef123456-diagram.png`.
   * Components that receive this from markdown rendering should NOT prefix
   * `/wiki/assets/`; ImageEmbed handles URL resolution.
   */
  assetPath: string
  /**
   * Optional pre-resolved alt text. When absent, ImageEmbed fetches from
   * the sidecar endpoint. Supplying it explicitly skips the fetch and
   * avoids a flash of alt-less rendering.
   */
  alt?: string
  /** Optional sibling thumbnail; falls back to the full-size source when empty. */
  thumbPath?: string
  /** Intrinsic dimensions for CLS-free rendering. */
  width?: number
  height?: number
  /**
   * When true, render inside an article body — applies the editorial
   * figure styling and enables the lightbox. The notebook preview in a
   * compose form sets this to false because it's not interactive.
   */
  editorial?: boolean
}

// ── Component ────────────────────────────────────────────────────

export function ImageEmbed({
  assetPath,
  alt: providedAlt,
  thumbPath,
  width,
  height,
  editorial = true,
}: ImageEmbedProps) {
  const [alt, setAlt] = useState<string>(providedAlt ?? '')
  const [open, setOpen] = useState(false)
  const closeButtonRef = useRef<HTMLButtonElement | null>(null)

  // Lazily fetch alt-text when it wasn't supplied and the asset is rendered
  // in editorial mode (where a11y matters most). The fetch is a no-op if
  // the sidecar doesn't exist yet.
  useEffect(() => {
    if (providedAlt !== undefined) {
      setAlt(providedAlt)
      return
    }
    let cancelled = false
    fetchAlt(assetPath)
      .then((a) => {
        if (!cancelled) setAlt(a)
      })
      .catch(() => {
        if (!cancelled) setAlt('')
      })
    return () => {
      cancelled = true
    }
  }, [assetPath, providedAlt])

  // Lightbox keyboard — Esc closes.
  useEffect(() => {
    if (!open) return
    const handler = (ev: KeyboardEvent) => {
      if (ev.key === 'Escape') setOpen(false)
    }
    window.addEventListener('keydown', handler)
    // Move focus to the close button so screen readers announce the dialog
    // and keyboard users can dismiss immediately.
    closeButtonRef.current?.focus()
    return () => window.removeEventListener('keydown', handler)
  }, [open])

  const src = thumbPath ? assetURL(thumbPath) : assetURL(assetPath)
  const fullSrc = assetURL(assetPath)

  const onImgClick = useCallback(
    (ev: React.MouseEvent) => {
      if (!editorial) return
      // Let assistive-tech users open via Enter / Space too via the wrapping
      // button — the <img> click fires here only for pointer users.
      ev.preventDefault()
      setOpen(true)
    },
    [editorial],
  )

  if (!editorial) {
    return (
      <img
        src={src}
        alt={alt}
        width={width}
        height={height}
        loading="lazy"
        decoding="async"
        className="image-embed__inline"
      />
    )
  }

  return (
    <>
      <figure className="image-embed">
        <button
          type="button"
          className="image-embed__trigger"
          aria-label={alt ? `View full-size: ${alt}` : 'View full-size image'}
          onClick={() => setOpen(true)}
        >
          <img
            src={src}
            alt={alt}
            width={width}
            height={height}
            loading="lazy"
            decoding="async"
            onClick={onImgClick}
            className="image-embed__img"
          />
        </button>
        {alt && <figcaption className="image-embed__caption">{alt}</figcaption>}
      </figure>
      {open && (
        <div
          className="image-embed__lightbox"
          role="dialog"
          aria-modal="true"
          aria-label={alt || 'Image viewer'}
          onClick={() => setOpen(false)}
        >
          <button
            ref={closeButtonRef}
            type="button"
            className="image-embed__close"
            aria-label="Close image viewer"
            onClick={(e) => {
              e.stopPropagation()
              setOpen(false)
            }}
          >
            ×
          </button>
          <img
            src={fullSrc}
            alt={alt}
            className="image-embed__full"
            onClick={(e) => e.stopPropagation()}
          />
        </div>
      )}
    </>
  )
}

export default ImageEmbed
