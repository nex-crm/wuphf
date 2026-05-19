PRAGMA foreign_keys = ON;

-- Approval commands share the cost-ledger command_idempotency table. Approval
-- route idempotency is additionally bound to the target resource and canonical
-- request body so key reuse cannot replay a response for a different approval.
ALTER TABLE command_idempotency
  ADD COLUMN request_fingerprint TEXT;

-- §15.B D2: pending approvals are explicit backend events, projected into a
-- disposable folded-state table. "Pending" is a status filter over this table,
-- not a derivation from receipt.approvals[].
CREATE TABLE pending_approvals (
  approval_id      TEXT PRIMARY KEY,
  status           TEXT NOT NULL,
  head_lsn         INTEGER NOT NULL,
  claim            TEXT NOT NULL,
  scope            TEXT NOT NULL,
  risk_class       TEXT NOT NULL,
  thread_id        TEXT,
  task_id          TEXT,
  receipt_id       TEXT,
  requested_by     TEXT NOT NULL,
  requested_at_ms  INTEGER NOT NULL,
  decided_by       TEXT,
  decided_at_ms    INTEGER,
  decision         TEXT,
  token            TEXT,
  FOREIGN KEY (head_lsn) REFERENCES event_log(lsn) ON DELETE RESTRICT,
  CHECK (status IN ('pending', 'approved', 'rejected', 'abstained')),
  CHECK (
    (status = 'pending' AND decided_by IS NULL AND decided_at_ms IS NULL AND decision IS NULL)
    OR
    (status != 'pending' AND decided_by IS NOT NULL AND decided_at_ms IS NOT NULL AND decision IS NOT NULL)
  ),
  CHECK (
    (decision IS NULL AND status = 'pending')
    OR (decision = 'approve' AND status = 'approved')
    OR (decision = 'reject' AND status = 'rejected')
    OR (decision = 'abstain' AND status = 'abstained')
  )
) STRICT, WITHOUT ROWID;

CREATE INDEX pending_approvals_status
  ON pending_approvals(status);

-- TODO(PR3): add a thread FK once the thread_state table lands. In this PR,
-- thread/task/receipt ids are opaque optional references.
CREATE INDEX pending_approvals_thread
  ON pending_approvals(thread_id)
  WHERE thread_id IS NOT NULL;

CREATE INDEX pending_approvals_task
  ON pending_approvals(task_id)
  WHERE task_id IS NOT NULL;

PRAGMA user_version = 6;
