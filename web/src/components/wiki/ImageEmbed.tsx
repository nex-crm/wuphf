import { useCallback, useEffect, useRef, useState } from "react";

export interface ImageEmbedProps {
  /** Absolute URL the agent embedded in markdown. */
  src: string;
  alt?: string;
  width?: number;
  height?: number;
  /**
   * When true, render inside an article body — applies the editorial
   * figure styling and enables the lightbox. Inline previews (e.g. in
   * compose forms) should pass false to get a plain <img>.
   */
  editorial?: boolean;
}

export function ImageEmbed({
  src,
  alt = "",
  width,
  height,
  editorial = true,
}: ImageEmbedProps) {
  const [open, setOpen] = useState(false);
  const closeButtonRef = useRef<HTMLButtonElement | null>(null);

  useEffect(() => {
    if (!open) return;
    const handler = (ev: KeyboardEvent) => {
      if (ev.key === "Escape") setOpen(false);
    };
    window.addEventListener("keydown", handler);
    closeButtonRef.current?.focus();
    return () => window.removeEventListener("keydown", handler);
  }, [open]);

  const onImgClick = useCallback(
    (ev: React.MouseEvent) => {
      if (!editorial) return;
      ev.preventDefault();
      setOpen(true);
    },
    [editorial],
  );

  if (!editorial) {
    return (
      <img
        src={src}
        alt={alt}
        width={width}
        height={height}
        loading="lazy"
        decoding="async"
        referrerPolicy="no-referrer"
        className="image-embed__inline"
      />
    );
  }

  return (
    <>
      <figure className="image-embed">
        <button
          type="button"
          className="image-embed__trigger"
          aria-label={alt ? `View full-size: ${alt}` : "View full-size image"}
          onClick={() => setOpen(true)}
        >
          <img
            src={src}
            alt={alt}
            width={width}
            height={height}
            loading="lazy"
            decoding="async"
            referrerPolicy="no-referrer"
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
          aria-label={alt || "Image viewer"}
          onClick={() => setOpen(false)}
        >
          <button
            ref={closeButtonRef}
            type="button"
            className="image-embed__close"
            aria-label="Close image viewer"
            onClick={(e) => {
              e.stopPropagation();
              setOpen(false);
            }}
          >
            ×
          </button>
          <img
            src={src}
            alt={alt}
            referrerPolicy="no-referrer"
            className="image-embed__full"
            onClick={(e) => e.stopPropagation()}
          />
        </div>
      )}
    </>
  );
}

export default ImageEmbed;
