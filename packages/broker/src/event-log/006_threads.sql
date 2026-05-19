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

PRAGMA user_version = 6;
