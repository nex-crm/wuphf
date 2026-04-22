/**
 * Pam's desk-menu action registry (frontend).
 *
 * Kept as a thin adapter over the backend registry (internal/team/pam_actions.go)
 * so server + client stay in lockstep: new actions appear in the UI as soon as
 * they are added to pam_actions.go and exposed via GET /pam/actions.
 *
 * The `handlers` map below is how the UI actually wires each action to a
 * client-side effect. For v1, every action triggers the same POST /pam/action
 * endpoint with its id — so a single default handler is enough. If an action
 * ever needs custom UX (e.g. a pre-confirmation dialog, or an inline prompt),
 * override it here.
 */

import { triggerPamAction, type PamActionDescriptor, type PamActionId } from '../api/pam'

export interface PamActionRunContext {
  articlePath: string
}

export type PamActionHandler = (ctx: PamActionRunContext) => Promise<{ job_id: number }>

// Default handler: POST /pam/action with {action, path}. Covers every action
// that runs entirely in Pam's sub-process (no client-side interaction beyond
// the click that opened the menu).
const defaultHandler =
  (actionId: PamActionId): PamActionHandler =>
  async (ctx) => {
    const res = await triggerPamAction(actionId, ctx.articlePath)
    return { job_id: res.job_id }
  }

// handlerOverrides lets a specific action opt out of the default flow. Empty
// for v1 — listed here so future actions have a one-line edit to plug in.
const handlerOverrides: Partial<Record<PamActionId, PamActionHandler>> = {}

export function resolvePamHandler(id: PamActionId): PamActionHandler {
  return handlerOverrides[id] ?? defaultHandler(id)
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
    run: resolvePamHandler(d.id),
  }))
}
