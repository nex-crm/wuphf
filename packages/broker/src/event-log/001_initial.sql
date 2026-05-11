PRAGMA foreign_keys = ON;

-- `lsn INTEGER PRIMARY KEY` (no AUTOINCREMENT) — append-only inserts get
-- monotonically increasing rowids without the `sqlite_sequence` table
-- write that AUTOINCREMENT requires. We never delete events, so the
-- "never reuse after delete" guarantee AUTOINCREMENT provides isn't
-- load-bearing here. (perf triangulation T4.)
CREATE TABLE event_log (
  lsn        INTEGER PRIMARY KEY,
  ts_ms      INTEGER NOT NULL,
  type       TEXT NOT NULL,
  payload    BLOB NOT NULL
) STRICT;

CREATE TABLE receipts_projection (
  receipt_id      TEXT PRIMARY KEY,
  thread_id       TEXT,
  schema_version  INTEGER NOT NULL,
  lsn             INTEGER NOT NULL UNIQUE,
  payload         BLOB NOT NULL,
  FOREIGN KEY (lsn) REFERENCES event_log(lsn) ON DELETE RESTRICT
) STRICT;

CREATE INDEX receipts_projection_thread_lsn
  ON receipts_projection(thread_id, lsn)
  WHERE thread_id IS NOT NULL;

PRAGMA user_version = 1;
