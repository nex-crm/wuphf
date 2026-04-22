/**
 * Pam's desk-menu action registry (frontend).
 *
 * Kept as a thin adapter over the backend registry (internal/team/pam_actions.go)
 * so server + client stay in lockstep: new actions appear in the UI as soon as
 * they are added to pam_actions.go and exposed via GET /pam/actions.
 *
 * v1: every action triggers the same POST /pam/action endpoint with its id,
 * so one default handler is enough. If a future action ever needs custom UX
 * (pre-confirmation dialog, inline prompt, etc.), reintroduce a per-id
 * override map here — see PR #217 review.
 */

import { triggerPamAction, type PamActionDescriptor, type PamActionId } from '../api/pam'

export interface PamActionRunContext {
  articlePath: string
}

export type PamActionHandler = (ctx: PamActionRunContext) => Promise<{ job_id: number }>

// Default handler: POST /pam/action with {action, path}. Covers every action
// that runs entirely in Pam's sub-process (no client-side interaction beyond
// the click that opened the menu).
function defaultHandler(actionId: PamActionId): PamActionHandler {
  return async (ctx) => {
    const res = await triggerPamAction(actionId, ctx.articlePath)
    return { job_id: res.job_id }
  }
}

/**
 * PamMenuEntry couples what the backend returned (id + label) with the
 * resolved client-side handler. This is what Pam's desk menu actually
 * renders + invokes.
 */
export interface PamMenuEntry extends PamActionDescriptor {
  run: PamActionHandler
}

export function buildPamMenu(descriptors: PamActionDescriptor[]): PamMenuEntry[] {
  return descriptors.map((d) => ({
    ...d,
    run: defaultHandler(d.id),
  }))
}
