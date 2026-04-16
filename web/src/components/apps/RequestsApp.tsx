import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { getRequests, answerRequest, type AgentRequest } from '../../api/client'
import { useAppStore } from '../../stores/app'
import { formatRelativeTime } from '../../lib/format'

export function RequestsApp() {
  const currentChannel = useAppStore((s) => s.currentChannel)
  const queryClient = useQueryClient()

  const { data, isLoading, error } = useQuery({
    queryKey: ['requests', currentChannel],
    queryFn: () => getRequests(currentChannel),
    refetchInterval: 5_000,
  })

  if (isLoading) {
    return (
      <div style={{ padding: '40px 20px', textAlign: 'center', color: 'var(--text-tertiary)', fontSize: 14 }}>
        Loading requests...
      </div>
    )
  }

  if (error) {
    return (
      <div style={{ padding: '40px 20px', textAlign: 'center', color: 'var(--text-tertiary)', fontSize: 14 }}>
        Failed to load requests.
      </div>
    )
  }

  const allRequests = dedupeRequests(data)
  const pending = allRequests.filter((r) => !r.status || r.status === 'open' || r.status === 'pending')
  const answered = allRequests.filter((r) => r.status && r.status !== 'open' && r.status !== 'pending')

  if (allRequests.length === 0) {
    return (
      <div style={{ padding: '40px 20px', textAlign: 'center', color: 'var(--text-tertiary)', fontSize: 14 }}>
        No requests right now. Your agents are working independently.
      </div>
    )
  }

  return (
    <>
      {pending.length > 0 && (
        <>
          <div style={{ fontSize: 13, fontWeight: 600, color: 'var(--text-secondary)', padding: '8px 0 4px' }}>
            Pending ({pending.length})
          </div>
          {pending.map((req) => (
            <RequestItem
              key={req.id}
              request={req}
              isPending
              onAnswer={(choiceId) => {
                answerRequest(req.id, choiceId).then(() => {
                  queryClient.invalidateQueries({ queryKey: ['requests'] })
                })
              }}
            />
          ))}
        </>
      )}

      {answered.length > 0 && (
        <>
          <div style={{ fontSize: 13, fontWeight: 600, color: 'var(--text-secondary)', padding: '12px 0 4px' }}>
            Answered ({answered.length})
          </div>
          {answered.map((req) => (
            <RequestItem key={req.id} request={req} isPending={false} />
          ))}
        </>
      )}
    </>
  )
}

function dedupeRequests(data: { requests: AgentRequest[] } | undefined): AgentRequest[] {
  const raw = data?.requests ?? []
  const seen = new Set<string>()
  return raw.filter((r) => {
    if (!r.id || seen.has(r.id)) return false
    seen.add(r.id)
    return true
  })
}

interface RequestItemProps {
  request: AgentRequest
  isPending: boolean
  onAnswer?: (choiceId: string) => void
}

function RequestItem({ request, isPending, onAnswer }: RequestItemProps) {
  const choices = request.choices ?? []

  return (
    <div className="app-card" style={{ marginBottom: 8 }}>
      <div style={{ display: 'flex', alignItems: 'center', gap: 8, marginBottom: 4 }}>
        <span style={{ fontWeight: 600, fontSize: 13 }}>{request.from || 'Unknown'}</span>
        {request.status && (
          <span className="badge badge-accent" style={{ fontSize: 10 }}>
            {request.status.toUpperCase()}
          </span>
        )}
      </div>

      <div style={{ fontSize: 14, marginBottom: 8 }}>{request.question || ''}</div>

      {request.timestamp && (
        <div className="app-card-meta" style={{ marginBottom: 6 }}>
          {formatRelativeTime(request.timestamp)}
        </div>
      )}

      {isPending && choices.length > 0 && (
        <div style={{ display: 'flex', gap: 8, flexWrap: 'wrap' }}>
          {choices.map((choice) => (
            <button
              key={choice.id}
              className="btn btn-ghost btn-sm"
              onClick={() => onAnswer?.(choice.id)}
            >
              {choice.label}
            </button>
          ))}
        </div>
      )}

      {!isPending && (
        <div style={{ fontSize: 12, color: 'var(--green)', fontWeight: 500 }}>Answered</div>
      )}
    </div>
  )
}
