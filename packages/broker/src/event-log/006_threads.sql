PRAGMA foreign_keys = ON;

-- §15.B thread foundation: folded current-state projection rebuilt from
-- event_log rows of type thread.created / thread.spec_edited /
-- thread.status_changed. The event log remains authoritative; this table is
-- disposable and can be regenerated from LSN 0.
CREATE TABLE threads (
  thread_id             TEXT PRIMARY KEY,
  title                 TEXT NOT NULL,
  status                TEXT NOT NULL,
  head_lsn              INTEGER NOT NULL,
  created_by            TEXT NOT NULL,
  created_at_ms         INTEGER NOT NULL,
  updated_at_ms         INTEGER NOT NULL,
  closed_at_ms          INTEGER,
  spec_revision_id      TEXT,
  spec_base_revision_id TEXT,
  spec_content          TEXT,
  spec_content_hash     TEXT,
  spec_authored_by      TEXT,
  spec_authored_at_ms   INTEGER,
  external_refs         TEXT NOT NULL,
  FOREIGN KEY (head_lsn) REFERENCES event_log(lsn) ON DELETE RESTRICT,
  CHECK (status IN ('open', 'in_progress', 'needs_review', 'merged', 'closed')),
  CHECK (head_lsn > 0),
  CHECK (created_at_ms >= 0),
  CHECK (updated_at_ms >= 0),
  CHECK (closed_at_ms IS NULL OR closed_at_ms >= 0)
) STRICT, WITHOUT ROWID;

CREATE INDEX threads_status
  ON threads(status);

-- Globally accepted spec revision ids. This is projection state rebuilt from
-- thread.spec_edited events; the UNIQUE primary key preserves the audit-log
-- invariant without folding unrelated thread histories on every command.
CREATE TABLE thread_spec_revisions (
  revision_id TEXT PRIMARY KEY,
  thread_id   TEXT NOT NULL,
  lsn         INTEGER NOT NULL,
  FOREIGN KEY (thread_id) REFERENCES threads(thread_id) ON DELETE RESTRICT,
  FOREIGN KEY (lsn) REFERENCES event_log(lsn) ON DELETE RESTRICT,
  CHECK (lsn > 0)
) STRICT, WITHOUT ROWID;

CREATE INDEX thread_spec_revisions_thread_lsn
  ON thread_spec_revisions(thread_id, lsn);

-- Receipt/thread index projection. Updated in the same SQLite write
-- transaction that appends receipt.put so thread reads never scan the receipt
-- store and never synthesize an unbounded Thread.task_ids array.
CREATE TABLE thread_receipts (
  thread_id  TEXT NOT NULL,
  receipt_id TEXT NOT NULL,
  task_id    TEXT NOT NULL,
  lsn        INTEGER NOT NULL,
  PRIMARY KEY (thread_id, receipt_id),
  FOREIGN KEY (thread_id) REFERENCES threads(thread_id) ON DELETE RESTRICT,
  FOREIGN KEY (lsn) REFERENCES event_log(lsn) ON DELETE RESTRICT,
  CHECK (lsn > 0)
) STRICT, WITHOUT ROWID;

CREATE INDEX thread_receipts_thread_lsn
  ON thread_receipts(thread_id, lsn);

PRAGMA user_version = 6;
