import { useQuery } from '@tanstack/react-query'
import { getScheduler, type SchedulerJob } from '../../api/client'
import { formatRelativeTime } from '../../lib/format'

function groupJobsByDate(jobs: SchedulerJob[]): Record<string, SchedulerJob[]> {
  const groups: Record<string, SchedulerJob[]> = {}
  for (const job of jobs) {
    let key = 'Upcoming'
    if (job.next_run) {
      const d = new Date(job.next_run)
      key = d.toLocaleDateString(undefined, { weekday: 'short', month: 'short', day: 'numeric' })
    }
    if (!groups[key]) groups[key] = []
    groups[key].push(job)
  }
  return groups
}

export function CalendarApp() {
  const { data, isLoading, error } = useQuery({
    queryKey: ['scheduler'],
    queryFn: () => getScheduler(),
    refetchInterval: 15_000,
  })

  const today = new Date()
  const dateStr = today.toLocaleDateString(undefined, {
    weekday: 'long',
    year: 'numeric',
    month: 'long',
    day: 'numeric',
  })

  if (isLoading) {
    return (
      <div style={{ padding: '40px 20px', textAlign: 'center', color: 'var(--text-tertiary)', fontSize: 14 }}>
        Loading schedule...
      </div>
    )
  }

  if (error) {
    return (
      <div style={{ padding: '40px 20px', textAlign: 'center', color: 'var(--text-tertiary)', fontSize: 14 }}>
        Could not load schedule.
      </div>
    )
  }

  const jobs = data?.jobs ?? []
  const groups = groupJobsByDate(jobs)
  const groupKeys = Object.keys(groups)

  return (
    <>
      <div style={{ padding: '0 0 12px', borderBottom: '1px solid var(--border)', marginBottom: 12 }}>
        <h3 style={{ fontSize: 16, fontWeight: 600, marginBottom: 4 }}>Schedule</h3>
        <p style={{ fontSize: 13, color: 'var(--text-secondary)' }}>{dateStr}</p>
      </div>

      {jobs.length === 0 ? (
        <div style={{ padding: '40px 20px', textAlign: 'center', color: 'var(--text-tertiary)', fontSize: 14 }}>
          No scheduled jobs.
        </div>
      ) : (
        groupKeys.map((groupKey) => (
          <div key={groupKey}>
            <div style={{
              fontSize: 11,
              fontWeight: 600,
              textTransform: 'uppercase',
              letterSpacing: '0.05em',
              color: 'var(--text-tertiary)',
              padding: '8px 0 6px',
            }}>
              {groupKey}
            </div>
            {groups[groupKey].map((job) => (
              <JobCard key={job.id} job={job} />
            ))}
          </div>
        ))
      )}
    </>
  )
}

function JobCard({ job }: { job: SchedulerJob }) {
  const metaParts: string[] = []
  if (job.cron) metaParts.push(job.cron)
  if (job.last_run) metaParts.push('Last: ' + new Date(job.last_run).toLocaleString())
  if (job.next_run) metaParts.push('Next: ' + new Date(job.next_run).toLocaleString())

  return (
    <div className="app-card" style={{ marginBottom: 8 }}>
      <div style={{ display: 'flex', alignItems: 'center', gap: 8, marginBottom: 4 }}>
        <span style={{ fontSize: 14 }}>{'\u23F0'}</span>
        <span className="app-card-title" style={{ marginBottom: 0 }}>
          {job.name || 'Job'}
        </span>
        {job.status && (
          <span className={job.status === 'active' ? 'badge badge-green' : 'badge badge-accent'}>
            {job.status.toUpperCase()}
          </span>
        )}
      </div>
      {job.next_run && (
        <div className="app-card-meta" style={{ marginBottom: 2 }}>
          {formatRelativeTime(job.next_run)}
        </div>
      )}
      {metaParts.length > 0 && (
        <div className="app-card-meta">{metaParts.join(' \u2022 ')}</div>
      )}
    </div>
  )
}
