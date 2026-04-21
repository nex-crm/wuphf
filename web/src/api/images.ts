/**
 * Wiki image API client — thin wrapper around `/wiki/images` multipart
 * upload + `/wiki/assets/*` serve path.
 *
 * The client avoids importing `post()` from client.ts because multipart
 * uploads need a raw `FormData` body and browser-set `Content-Type` boundary,
 * not the JSON envelope `post()` enforces.
 *
 * SSE events ride on the shared `/events` stream. Named listeners
 * `wiki:image_uploaded` and `wiki:image_alt_updated` mirror the pattern set
 * by entity events (see PR #182 for why named listeners are required).
 */

import { sseURL } from './client'

// ── Types ────────────────────────────────────────────────────────

export interface ImageUploadResult {
  asset_path: string
  thumb_path?: string
  width?: number
  height?: number
  sha256: string
  size_bytes: number
  format: 'png' | 'jpeg' | 'webp' | 'gif' | 'svg' | string
  commit_sha?: string
  alt_path?: string
}

export interface ImageUploadedEvent {
  path: string
  thumb_path?: string
  commit_sha: string
  author_slug: string
  timestamp: string
}

export interface ImageAltUpdatedEvent {
  alt_path: string
  commit_sha: string
  author_slug: string
  timestamp: string
}

export interface UploadOptions {
  authorSlug?: string
  alt?: string
  commitMessage?: string
  /**
   * Progress callback invoked with 0..1 as bytes upload. Fired from
   * XHR.upload.onprogress — undefined if the browser does not provide
   * upload progress (rare).
   */
  onProgress?: (fraction: number) => void
}

// ── Internal helpers ─────────────────────────────────────────────

/**
 * baseURL() in client.ts is private; replicate the decision here so image
 * uploads can share the same resolution. The only wrinkle is that
 * multipart uploads go straight to the broker in direct-connect mode
 * because the web proxy may add unwanted transformations.
 */
function resolveBaseURL(): string {
  // When running inside the web UI the proxy route /api is available
  // same-origin. Fall back to the broker only if /api is unreachable,
  // which the caller can detect via a 404 on the upload response.
  return '/api'
}

/**
 * resolveBearerToken reads the token the page fetched during initApi().
 * We re-fetch from /api-token rather than exposing the private state in
 * client.ts — both paths are loopback-only so the extra request is cheap.
 */
async function resolveBearerToken(): Promise<string | null> {
  try {
    const r = await fetch('/api-token')
    if (!r.ok) return null
    const data = (await r.json()) as { token?: string }
    return data.token ?? null
  } catch {
    return null
  }
}

// ── Upload ───────────────────────────────────────────────────────

/**
 * uploadImage performs a multipart POST to /wiki/images. Uses XHR so we
 * can surface upload progress; fetch() does not yet expose upload-side
 * streaming in any browser we ship to.
 */
export function uploadImage(file: File, options: UploadOptions = {}): Promise<ImageUploadResult> {
  return new Promise(async (resolve, reject) => {
    const token = await resolveBearerToken()
    const form = new FormData()
    form.append('file', file, file.name)
    if (options.authorSlug) form.append('author_slug', options.authorSlug)
    if (options.alt) form.append('alt', options.alt)
    if (options.commitMessage) form.append('commit_message', options.commitMessage)

    const xhr = new XMLHttpRequest()
    xhr.open('POST', resolveBaseURL() + '/wiki/images', true)
    if (token) xhr.setRequestHeader('Authorization', `Bearer ${token}`)

    xhr.upload.onprogress = (ev: ProgressEvent) => {
      if (ev.lengthComputable && options.onProgress) {
        options.onProgress(ev.loaded / ev.total)
      }
    }
    xhr.onload = () => {
      if (xhr.status >= 200 && xhr.status < 300) {
        try {
          const data = JSON.parse(xhr.responseText) as ImageUploadResult
          resolve(data)
        } catch (err) {
          reject(new Error('invalid upload response: ' + (err as Error).message))
        }
      } else {
        let message = `upload failed with status ${xhr.status}`
        try {
          const parsed = JSON.parse(xhr.responseText) as { error?: string }
          if (parsed?.error) message = parsed.error
        } catch {
          if (xhr.responseText) message = xhr.responseText
        }
        reject(new Error(message))
      }
    }
    xhr.onerror = () => reject(new Error('network error during upload'))
    xhr.send(form)
  })
}

/** Return the HTTP URL a browser should GET for a committed asset. */
export function assetURL(assetPath: string): string {
  // team/assets/... → /wiki/assets/... (no encoding on '/' separators)
  const tail = assetPath.replace(/^team\/assets\//, '')
  const encoded = tail.split('/').map(encodeURIComponent).join('/')
  return resolveBaseURL() + '/wiki/assets/' + encoded
}

/**
 * Return the URL for the sibling thumbnail, falling back to the full-size
 * URL when no thumbnail was generated (SVG, or source already small).
 */
export function thumbURL(result: Pick<ImageUploadResult, 'asset_path' | 'thumb_path'>): string {
  if (result.thumb_path) return assetURL(result.thumb_path)
  return assetURL(result.asset_path)
}

/**
 * Request server-side vision alt-text synthesis for an asset that was
 * uploaded without an explicit alt. Returns when the broker has queued
 * the job — the actual alt lands via SSE `wiki:image_alt_updated`.
 */
export async function requestVisionAlt(assetPath: string, actorSlug?: string): Promise<void> {
  const token = await resolveBearerToken()
  const res = await fetch(resolveBaseURL() + '/wiki/images/describe', {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
      ...(token ? { Authorization: `Bearer ${token}` } : {}),
    },
    body: JSON.stringify({ asset_path: assetPath, actor_slug: actorSlug }),
  })
  if (!res.ok) {
    const text = await res.text().catch(() => '')
    throw new Error(text || `vision request failed with status ${res.status}`)
  }
}

/** Fetch the current alt-text sidecar for an asset. Empty string = no alt. */
export async function fetchAlt(assetPath: string): Promise<string> {
  const token = await resolveBearerToken()
  const url = resolveBaseURL() + '/wiki/images/alt?asset_path=' + encodeURIComponent(assetPath)
  const res = await fetch(url, {
    headers: token ? { Authorization: `Bearer ${token}` } : {},
  })
  if (!res.ok) return ''
  const data = (await res.json()) as { alt?: string }
  return data.alt ?? ''
}

// ── SSE ──────────────────────────────────────────────────────────

/**
 * subscribeImageEvents opens an EventSource on the shared `/events` path
 * and fires the supplied handlers on the named events. Returns an
 * unsubscribe cleanup.
 *
 * The filter parameter narrows delivery to one asset path when present —
 * useful for the per-article refresh case where the page only cares about
 * one embedded image.
 */
export function subscribeImageEvents(
  onUpload: (ev: ImageUploadedEvent) => void,
  onAlt: (ev: ImageAltUpdatedEvent) => void,
  filter?: { assetPath?: string },
): () => void {
  let closed = false
  let source: EventSource | null = null

  const upHandler = (ev: MessageEvent) => {
    if (closed) return
    try {
      const data = JSON.parse(ev.data) as ImageUploadedEvent
      if (filter?.assetPath && data.path !== filter.assetPath) return
      onUpload(data)
    } catch {
      // ignore malformed events
    }
  }
  const altHandler = (ev: MessageEvent) => {
    if (closed) return
    try {
      const data = JSON.parse(ev.data) as ImageAltUpdatedEvent
      if (filter?.assetPath) {
        // Alt paths are sibling of asset paths — compare by stripping
        // .alt.md.
        const parent = data.alt_path.replace(/\.alt\.md$/, '')
        if (!filter.assetPath.startsWith(parent)) return
      }
      onAlt(data)
    } catch {
      // ignore
    }
  }

  try {
    const ES = (globalThis as { EventSource?: typeof EventSource }).EventSource
    if (!ES) return () => {}
    source = new ES(sseURL('/events'))
    source.addEventListener('wiki:image_uploaded', upHandler as EventListener)
    source.addEventListener('wiki:image_alt_updated', altHandler as EventListener)
    source.onerror = () => {
      // EventSource auto-reconnects — closing here would drop future
      // events after a transient network blip.
    }
  } catch {
    source = null
  }

  return () => {
    closed = true
    if (source) {
      source.removeEventListener('wiki:image_uploaded', upHandler as EventListener)
      source.removeEventListener('wiki:image_alt_updated', altHandler as EventListener)
      source.close()
      source = null
    }
  }
}
