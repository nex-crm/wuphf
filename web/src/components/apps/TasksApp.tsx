import { useQuery } from '@tanstack/react-query'
import { getOfficeTasks, type Task } from '../../api/client'
import { formatRelativeTime } from '../../lib/format'

const STATUS_ORDER = ['in_progress', 'open', 'review', 'pending', 'blocked', 'done'] as const

type StatusGroup = 'in_progress' | 'open' | 'review' | 'pending' | 'blocked' | 'done'

function normalizeStatus(raw: string): StatusGroup {
  const s = raw.toLowerCase().replace(/[\s-]+/g, '_')
  if (s === 'completed') return 'done'
  if (s === 'in_review') return 'review'
  if (STATUS_ORDER.includes(s as StatusGroup)) return s as StatusGroup
  return 'open'
}

function statusBadgeClass(status: StatusGroup): string {
  if (status === 'done') return 'badge badge-green'
  if (status === 'in_progress' || status === 'review') return 'badge badge-accent'
  if (status === 'blocked') return 'badge badge-yellow'
  return 'badge badge-accent'
}

function groupTasks(tasks: Task[]): Record<StatusGroup, Task[]> {
  const groups: Record<StatusGroup, Task[]> = {
    in_progress: [],
    open: [],
    review: [],
    pending: [],
    blocked: [],
    done: [],
  }
  for (const task of tasks) {
    const status = normalizeStatus(task.status)
    groups[status].push(task)
  }
  return groups
}

export function TasksApp() {
  const { data, isLoading, error } = useQuery({
    queryKey: ['office-tasks'],
    queryFn: () => getOfficeTasks({ includeDone: true }),
    refetchInterval: 10_000,
  })

  if (isLoading) {
    return (
      <div style={{ padding: '40px 20px', textAlign: 'center', color: 'var(--text-tertiary)', fontSize: 14 }}>
        Loading tasks...
      </div>
    )
  }

  if (error) {
    return (
      <div style={{ padding: '40px 20px', textAlign: 'center', color: 'var(--text-tertiary)', fontSize: 14 }}>
        Could not load tasks.
      </div>
    )
  }

  const tasks = data?.tasks ?? []

  if (tasks.length === 0) {
    return (
      <div style={{ padding: '40px 20px', textAlign: 'center', color: 'var(--text-tertiary)', fontSize: 14 }}>
        No tasks yet.
      </div>
    )
  }

  const grouped = groupTasks(tasks)

  return (
    <>
      <div style={{ padding: '16px 20px 0', borderBottom: '1px solid var(--border)' }}>
        <h3 style={{ fontSize: 16, fontWeight: 600 }}>Office tasks</h3>
        <div style={{ fontSize: 12, color: 'var(--text-tertiary)', marginTop: 4, marginBottom: 12 }}>
          All active lanes across the office.
        </div>
      </div>

      <div className="task-board">
        {STATUS_ORDER.map((status) => {
          const column = grouped[status]
          if (column.length === 0 && (status === 'pending' || status === 'blocked')) return null
          return (
            <div className="task-column" key={status}>
              <div className="task-column-header">
                <span>{status.replace(/_/g, ' ')}</span>
                <span className="task-column-count">{column.length}</span>
              </div>
              {column.map((task) => (
                <TaskCard key={task.id} task={task} />
              ))}
            </div>
          )
        })}
      </div>
    </>
  )
}

function TaskCard({ task }: { task: Task }) {
  const status = normalizeStatus(task.status)
  const timestamp = task.updated_at ?? task.created_at

  return (
    <div className="app-card" style={{ marginBottom: 8 }}>
      <div className="app-card-title">{task.title || 'Untitled'}</div>
      {task.description && (
        <div style={{ fontSize: 12, color: 'var(--text-secondary)', marginBottom: 8, lineHeight: 1.45 }}>
          {task.description.slice(0, 160)}
        </div>
      )}
      <div style={{ display: 'flex', alignItems: 'center', gap: 8, flexWrap: 'wrap' }}>
        <span className={statusBadgeClass(status)}>
          {status.replace(/_/g, ' ')}
        </span>
        {task.assigned_to && (
          <span className="app-card-meta">@{task.assigned_to}</span>
        )}
        {task.channel && (
          <span className="app-card-meta">#{task.channel}</span>
        )}
        {timestamp && (
          <span className="app-card-meta">{formatRelativeTime(timestamp)}</span>
        )}
      </div>
    </div>
  )
}
