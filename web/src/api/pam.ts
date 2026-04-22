/**
 * Pam the Archivist — client API.
 *
 * Backend lives in internal/team/pam.go + internal/team/broker_pam.go. Pam is
 * spawned in her own sub-process per action; this module just triggers jobs
 * and subscribes to her progress.
 */

import { get, post, sseURL } from './client'

export type PamActionId = 'enrich_article' | (string & {})

export interface PamActionDescriptor {
  id: PamActionId
  label: string
}

export interface PamActionStartedEvent {
  job_id: number
  action: PamActionId
  article_path: string
  request_by: string
  started_at: string
}

export interface PamActionDoneEvent {
  job_id: number
  action: PamActionId
  article_path: string
  commit_sha: string
  finished_at: string
}

export interface PamActionFailedEvent {
  job_id: number
  action: PamActionId
  article_path: string
  error: string
  failed_at: string
}

export type PamActionEvent =
  | ({ kind: 'started' } & PamActionStartedEvent)
  | ({ kind: 'done' } & PamActionDoneEvent)
  | ({ kind: 'failed' } & PamActionFailedEvent)

/**
 * Fetches Pam's action registry so the UI renders the desk menu in the same
 * order and with the same labels as the server defines. Keeps the menu
 * extensible — adding a new entry in pam_actions.go surfaces it in the UI
 * automatically.
 */
export function listPamActions() {
  return get<{ actions: PamActionDescriptor[] }>('/pam/actions')
}

/**
 * Triggers a Pam action on an article. Returns the job id so callers can
 * correlate subsequent SSE events from subscribePamEvents.
 */
export function triggerPamAction(action: PamActionId, articlePath: string) {
  return post<{ job_id: number; queued_at: string }>('/pam/action', {
    action,
    path: articlePath,
  })
}

/**
 * Subscribes to Pam's progress events on /events. Mirrors subscribeEditLog
 * in api/wiki.ts. Returns an unsubscribe function.
 */
export function subscribePamEvents(
  handler: (evt: PamActionEvent) => void,
): () => void {
  let closed = false
  let source: EventSource | null = null
  let onStarted: ((ev: MessageEvent) => void) | null = null
  let onDone: ((ev: MessageEvent) => void) | null = null
  let onFailed: ((ev: MessageEvent) => void) | null = null

  try {
    const ES = (globalThis as { EventSource?: typeof EventSource }).EventSource
    if (!ES) return () => { closed = true }
    source = new ES(sseURL('/events'))
    onStarted = (ev: MessageEvent) => {
      if (closed) return
      try {
        const data = JSON.parse(ev.data) as PamActionStartedEvent
        handler({ kind: 'started', ...data })
      } catch {
        // ignore malformed events
      }
    }
    onDone = (ev: MessageEvent) => {
      if (closed) return
      try {
        const data = JSON.parse(ev.data) as PamActionDoneEvent
        handler({ kind: 'done', ...data })
      } catch {
        // ignore malformed events
      }
    }
    onFailed = (ev: MessageEvent) => {
      if (closed) return
      try {
        const data = JSON.parse(ev.data) as PamActionFailedEvent
        handler({ kind: 'failed', ...data })
      } catch {
        // ignore malformed events
      }
    }
    source.addEventListener('pam:action_started', onStarted as EventListener)
    source.addEventListener('pam:action_done', onDone as EventListener)
    source.addEventListener('pam:action_failed', onFailed as EventListener)
  } catch {
    source = null
  }

  return () => {
    closed = true
    if (source) {
      if (onStarted) source.removeEventListener('pam:action_started', onStarted as EventListener)
      if (onDone) source.removeEventListener('pam:action_done', onDone as EventListener)
      if (onFailed) source.removeEventListener('pam:action_failed', onFailed as EventListener)
      source.close()
    }
  }
}
