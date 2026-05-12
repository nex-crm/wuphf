PRAGMA foreign_keys = ON;

-- §15.A architecture-proof slice: projection tables + reactor-cursor + command
-- idempotency. Every row references an event_log LSN as the canonical source
-- of truth. The §15.A sum invariant
--   sum(cost_events) == sum(cost_by_agent) == sum(cost_by_task)
-- is decidable because amounts are stored as INTEGER micro-USD throughout —
-- no float drift, no rounding ambiguity. See packages/protocol/src/cost.ts for
-- the wire-shape brand bounds.

-- cost_by_agent: per-(agent, calendar-day-UTC) cumulative spend.
-- Day key is a UTC date string (YYYY-MM-DD) derived from the cost_event's
-- `occurredAt`. Calendar-day reset (NOT rolling 24h) is the locked design
-- decision — simpler to explain, matches every billing surface humans already
-- recognize. `last_lsn` lets the projection answer "is this aggregate up to
-- date relative to event_log?" without a full scan.
CREATE TABLE cost_by_agent (
  agent_slug         TEXT NOT NULL,
  day_utc            TEXT NOT NULL,
  total_micro_usd    INTEGER NOT NULL,
  last_lsn           INTEGER NOT NULL,
  PRIMARY KEY (agent_slug, day_utc),
  FOREIGN KEY (last_lsn) REFERENCES event_log(lsn) ON DELETE RESTRICT,
  CHECK (total_micro_usd >= 0)
) STRICT, WITHOUT ROWID;

-- cost_by_task: per-task cumulative spend across the task's lifetime.
-- Optional task_id on cost_event means not every event hits this projection.
CREATE TABLE cost_by_task (
  task_id            TEXT PRIMARY KEY,
  total_micro_usd    INTEGER NOT NULL,
  last_lsn           INTEGER NOT NULL,
  FOREIGN KEY (last_lsn) REFERENCES event_log(lsn) ON DELETE RESTRICT,
  CHECK (total_micro_usd >= 0)
) STRICT, WITHOUT ROWID;

-- cost_budgets: current state of every budget, as projected from
-- `budget_set` events. `set_at_lsn` is the LSN of the most recent
-- non-tombstone budget_set; the threshold-crossings projection keys by it so
-- raising or resetting a budget mints a new LSN that re-arms thresholds
-- automatically. `tombstoned` is 1 when the most recent budget_set carried
-- `limitMicroUsd === 0` — the row is retained for replay, but the reactor
-- treats it as unreachable.
--
-- `thresholds_bps` is canonical JSON of an ascending-deduplicated integer
-- array (≤ 8 entries, each in (0, 10000]); enforcement lives in the protocol
-- validators, so this column is a passthrough.
CREATE TABLE cost_budgets (
  budget_id          TEXT PRIMARY KEY,
  scope              TEXT NOT NULL,
  subject_id         TEXT,
  limit_micro_usd    INTEGER NOT NULL,
  thresholds_bps     TEXT NOT NULL,
  set_at_lsn         INTEGER NOT NULL,
  tombstoned         INTEGER NOT NULL DEFAULT 0,
  FOREIGN KEY (set_at_lsn) REFERENCES event_log(lsn) ON DELETE RESTRICT,
  CHECK (scope IN ('global', 'agent', 'task')),
  CHECK (limit_micro_usd >= 0),
  CHECK (tombstoned IN (0, 1))
) STRICT, WITHOUT ROWID;

CREATE INDEX cost_budgets_scope_subject
  ON cost_budgets(scope, subject_id);

-- cost_threshold_crossings: one row per (budget, budget_set epoch, threshold)
-- that has fired. The composite PK is what gives §15.A's "re-arming on budget
-- bump" — a new budget_set LSN keys an independent row even for the same
-- budget_id + threshold_bps pair. Without budget_set_lsn in the PK, a
-- raised-and-then-tripped budget would silently fail to re-emit a crossing.
CREATE TABLE cost_threshold_crossings (
  budget_id          TEXT NOT NULL,
  budget_set_lsn     INTEGER NOT NULL,
  threshold_bps      INTEGER NOT NULL,
  crossed_at_lsn     INTEGER NOT NULL,
  observed_micro_usd INTEGER NOT NULL,
  limit_micro_usd    INTEGER NOT NULL,
  PRIMARY KEY (budget_id, budget_set_lsn, threshold_bps),
  FOREIGN KEY (budget_set_lsn) REFERENCES event_log(lsn) ON DELETE RESTRICT,
  FOREIGN KEY (crossed_at_lsn) REFERENCES event_log(lsn) ON DELETE RESTRICT,
  CHECK (threshold_bps > 0 AND threshold_bps <= 10000),
  CHECK (observed_micro_usd >= 0),
  CHECK (limit_micro_usd >= 0)
) STRICT, WITHOUT ROWID;

-- command_idempotency: stores response payloads for safely-retryable POST
-- commands keyed by `cmd_<command>_<26-ULID>` (see §15.A:1095). On duplicate
-- POST with the same key the broker returns the cached response_payload byte
-- for byte. `command` is the canonical command name (e.g. "cost.event") so
-- the same ULID re-used across different commands does not collide.
CREATE TABLE command_idempotency (
  idempotency_key    TEXT PRIMARY KEY,
  command            TEXT NOT NULL,
  status_code        INTEGER NOT NULL,
  response_payload   BLOB NOT NULL,
  created_at_lsn     INTEGER,
  created_at_ms      INTEGER NOT NULL,
  CHECK (status_code >= 100 AND status_code < 600)
) STRICT;

CREATE INDEX command_idempotency_created
  ON command_idempotency(created_at_ms);

-- reactor_cursors: persistent per-reactor cursor. The threshold reactor
-- advances this in the same transaction that appends a
-- `cost.budget.threshold.crossed` event (or that decides no crossing fires),
-- so a crash mid-projection cannot lose a crossing on replay.
CREATE TABLE reactor_cursors (
  reactor_name       TEXT PRIMARY KEY,
  last_processed_lsn INTEGER NOT NULL,
  updated_at_ms      INTEGER NOT NULL,
  CHECK (last_processed_lsn >= 0)
) STRICT;

PRAGMA user_version = 2;
