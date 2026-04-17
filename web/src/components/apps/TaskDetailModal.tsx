import { useEffect, useMemo, useState } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import {
  getOfficeMembers,
  reassignTask,
  updateTaskStatus,
  type OfficeMember,
  type Task,
  type TaskStatusAction,
} from '../../api/client'
import { formatRelativeTime } from '../../lib/format'
import { confirm } from '../ui/ConfirmDialog'

interface TaskDetailModalProps {
  task: Task
  onClose: () => void
}

const HUMAN_SLUG = 'human'

export function TaskDetailModal({ task, onClose }: TaskDetailModalProps) {
  const queryClient = useQueryClient()
  const { data: memberData } = useQuery({
    queryKey: ['office-members'],
    queryFn: getOfficeMembers,
    staleTime: 30_000,
  })

  const currentOwner = (task.owner ?? '').trim()
  const currentStatus = (task.status ?? '').trim().toLowerCase()
  const [selectedOwner, setSelectedOwner] = useState<string>(currentOwner)
  const [submitting, setSubmitting] = useState(false)
  const [statusBusy, setStatusBusy] = useState<TaskStatusAction | null>(null)
  const [errorMsg, setErrorMsg] = useState<string | null>(null)

  useEffect(() => {
    setSelectedOwner((task.owner ?? '').trim())
    setErrorMsg(null)
  }, [task.id, task.owner])

  useEffect(() => {
    function handleKey(e: KeyboardEvent) {
      if (e.key === 'Escape') onClose()
    }
    document.addEventListener('keydown', handleKey)
    return () => document.removeEventListener('keydown', handleKey)
  }, [onClose])

  const assignableMembers = useMemo<OfficeMember[]>(() => {
    const members = memberData?.members ?? []
    return members.filter((m) => {
      const slug = m.slug?.trim().toLowerCase()
      return slug && slug !== 'human' && slug !== 'you'
    })
  }, [memberData])

  async function runStatusAction(action: TaskStatusAction) {
    setStatusBusy(action)
    setErrorMsg(null)
    try {
      await updateTaskStatus(task.id, action, task.channel || 'general', HUMAN_SLUG)
      await queryClient.invalidateQueries({ queryKey: ['office-tasks'] })
      if (action === 'cancel' || action === 'complete') {
        onClose()
      }
    } catch (err) {
      const message = err instanceof Error ? err.message : `${action} failed`
      setErrorMsg(message)
    } finally {
      setStatusBusy(null)
    }
  }

  function handleStatusAction(action: TaskStatusAction) {
    if (action === 'cancel') {
      confirm({
        title: 'Mark task as won\'t do?',
        message: `"${task.title || task.id}" will move to the Won't Do column. Owners are notified.`,
        confirmLabel: "Won't do",
        danger: true,
        onConfirm: () => runStatusAction(action),
      })
      return
    }
    void runStatusAction(action)
  }

  async function handleReassign() {
    const next = selectedOwner.trim()
    if (!next || next === currentOwner) return
    setSubmitting(true)
    setErrorMsg(null)
    try {
      await reassignTask(task.id, next, task.channel || 'general', HUMAN_SLUG)
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: ['office-tasks'] }),
        queryClient.invalidateQueries({ queryKey: ['tasks'] }),
      ])
      onClose()
    } catch (err) {
      const message = err instanceof Error ? err.message : 'Reassign failed'
      setErrorMsg(message)
    } finally {
      setSubmitting(false)
    }
  }

  function handleOverlayClick(e: React.MouseEvent<HTMLDivElement>) {
    if (e.target === e.currentTarget) onClose()
  }

  const status = (task.status || '').replace(/_/g, ' ')
  const reviewState = (task.review_state || '').replace(/_/g, ' ')
  const description = task.description?.trim() || ''
  const details = task.details?.trim() || ''

  const metaRows: Array<[string, string | null | undefined]> = [
    ['Owner', task.owner ? `@${task.owner}` : '(unassigned)'],
    ['Channel', task.channel ? `#${task.channel}` : '—'],
    ['Status', status || '—'],
    ['Review state', reviewState || null],
    ['Task type', task.task_type || null],
    ['Execution mode', task.execution_mode || null],
    ['Pipeline', task.pipeline_id || null],
    ['Pipeline stage', task.pipeline_stage || null],
    ['Worktree branch', task.worktree_branch || null],
    ['Worktree path', task.worktree_path || null],
    ['Source signal', task.source_signal_id || null],
    ['Source decision', task.source_decision_id || null],
    ['Thread', task.thread_id || null],
    ['Created by', task.created_by ? `@${task.created_by}` : null],
    ['Created', task.created_at ? formatRelativeTime(task.created_at) : null],
    ['Updated', task.updated_at ? formatRelativeTime(task.updated_at) : null],
    ['Due', task.due_at ? formatRelativeTime(task.due_at) : null],
    ['Follow up', task.follow_up_at ? formatRelativeTime(task.follow_up_at) : null],
    ['Reminder', task.reminder_at ? formatRelativeTime(task.reminder_at) : null],
    ['Recheck', task.recheck_at ? formatRelativeTime(task.recheck_at) : null],
  ]

  const dependsOn = task.depends_on ?? []

  const ownerChanged = selectedOwner.trim() !== currentOwner && selectedOwner.trim() !== ''

  return (
    <div
      className="task-detail-overlay"
      onClick={handleOverlayClick}
      role="dialog"
      aria-modal="true"
      aria-label={`Task ${task.id}`}
    >
      <div className="task-detail-modal card">
        <header className="task-detail-header">
          <div>
            <div className="task-detail-id">#{task.id}</div>
            <h2 className="task-detail-title">{task.title || 'Untitled task'}</h2>
          </div>
          <button
            type="button"
            className="task-detail-close"
            onClick={onClose}
            aria-label="Close"
          >
            ×
          </button>
        </header>

        <section className="task-detail-section">
          <div className="task-detail-label">Status</div>
          <div className="task-detail-status">
            <span className={`task-detail-status-badge status-${currentStatus || 'open'}`}>
              {currentStatus ? currentStatus.replace(/_/g, ' ') : 'open'}
            </span>
            <div className="task-detail-status-actions">
              <StatusButton
                action="release"
                label="Release"
                busy={statusBusy}
                disabledFor={['open']}
                currentStatus={currentStatus}
                onClick={handleStatusAction}
              />
              <StatusButton
                action="review"
                label="Mark review"
                busy={statusBusy}
                disabledFor={['review']}
                currentStatus={currentStatus}
                onClick={handleStatusAction}
              />
              <StatusButton
                action="block"
                label="Block"
                busy={statusBusy}
                disabledFor={['blocked']}
                currentStatus={currentStatus}
                onClick={handleStatusAction}
              />
              <StatusButton
                action="complete"
                label="Mark done"
                busy={statusBusy}
                disabledFor={['done']}
                currentStatus={currentStatus}
                onClick={handleStatusAction}
              />
              <StatusButton
                action="cancel"
                label="Won't do"
                busy={statusBusy}
                disabledFor={['canceled', 'cancelled']}
                currentStatus={currentStatus}
                onClick={handleStatusAction}
                danger
              />
            </div>
          </div>
        </section>

        <section className="task-detail-section">
          <div className="task-detail-label">Ownership</div>
          <div className="task-detail-ownership">
            <div className="task-detail-owner-current">
              <span className="task-detail-owner-badge">
                {task.owner ? `@${task.owner}` : '(unassigned)'}
              </span>
              <span className="task-detail-hint">
                Reassigning posts to #{task.channel || 'general'} and DMs both owners.
                CEO is cc'd.
              </span>
            </div>
            <div className="task-detail-owner-controls">
              <select
                className="task-detail-select"
                value={selectedOwner}
                onChange={(e) => setSelectedOwner(e.target.value)}
                disabled={submitting}
              >
                <option value="">(pick an owner)</option>
                {assignableMembers.map((m) => (
                  <option key={m.slug} value={m.slug}>
                    {m.name ? `${m.name} — @${m.slug}` : `@${m.slug}`}
                  </option>
                ))}
              </select>
              <button
                type="button"
                className="btn btn-primary btn-sm"
                onClick={handleReassign}
                disabled={!ownerChanged || submitting}
              >
                {submitting ? 'Reassigning...' : 'Reassign'}
              </button>
            </div>
            {errorMsg && <div className="task-detail-error">{errorMsg}</div>}
          </div>
        </section>

        {(description || details) && (
          <section className="task-detail-section">
            {description && (
              <>
                <div className="task-detail-label">Description</div>
                <div className="task-detail-body">{description}</div>
              </>
            )}
            {details && (
              <>
                <div className="task-detail-label" style={{ marginTop: description ? 12 : 0 }}>
                  Details
                </div>
                <div className="task-detail-body">{details}</div>
              </>
            )}
          </section>
        )}

        {dependsOn.length > 0 && (
          <section className="task-detail-section">
            <div className="task-detail-label">Depends on</div>
            <ul className="task-detail-deps">
              {dependsOn.map((dep) => (
                <li key={dep}>#{dep}</li>
              ))}
            </ul>
          </section>
        )}

        <section className="task-detail-section">
          <div className="task-detail-label">Metadata</div>
          <dl className="task-detail-meta">
            {metaRows
              .filter(([, value]) => value != null && value !== '')
              .map(([key, value]) => (
                <div key={key} className="task-detail-meta-row">
                  <dt>{key}</dt>
                  <dd>{value}</dd>
                </div>
              ))}
          </dl>
        </section>
      </div>
    </div>
  )
}

interface StatusButtonProps {
  action: TaskStatusAction
  label: string
  busy: TaskStatusAction | null
  disabledFor: string[]
  currentStatus: string
  onClick: (action: TaskStatusAction) => void
  danger?: boolean
}

function StatusButton({
  action,
  label,
  busy,
  disabledFor,
  currentStatus,
  onClick,
  danger,
}: StatusButtonProps) {
  const isCurrent = disabledFor.includes(currentStatus)
  const isBusy = busy === action
  const anyBusy = busy !== null
  const className = 'btn btn-sm ' + (danger ? 'btn-ghost task-detail-status-btn-danger' : 'btn-ghost')
  return (
    <button
      type="button"
      className={className}
      onClick={() => onClick(action)}
      disabled={isCurrent || anyBusy}
      title={isCurrent ? 'Task is already in this state' : undefined}
    >
      {isBusy ? '...' : label}
    </button>
  )
}
