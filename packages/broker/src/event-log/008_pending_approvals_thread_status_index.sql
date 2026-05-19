PRAGMA foreign_keys = ON;

-- PR 3 pinned approvals are a read-time query over pending_approvals scoped by
-- thread_id and status. Keep the hot path bounded without adding projection
-- state.
CREATE INDEX pending_approvals_thread_status
  ON pending_approvals(thread_id, status)
  WHERE thread_id IS NOT NULL;

PRAGMA user_version = 8;
