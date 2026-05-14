PRAGMA foreign_keys = ON;

-- Rationale:
-- - Composite PK on (agent_id, runner_kind) makes `getEntry(agentId, kind)` an
--   O(1) point lookup and makes `put`'s replace-all semantics a simple
--   `DELETE FROM ... WHERE agent_id = ?` + bulk insert in one transaction.
-- - `STRICT` rejects type mismatches (e.g. an integer in a TEXT column).
-- - `WITHOUT ROWID` is appropriate because the PK is composite text and the
--   table will never have more rows than (agent count × RunnerKind count).
-- - No FK on `agent_id` — the broker does not enforce a global agent registry.
--   A stored route for a deleted agent is dead weight, not a correctness bug.
-- - No timestamps. Per broker rule 8, `Date.now()` is forbidden for ordering;
--   the table has no semantic need for timestamps and adding them invites
--   drift with the renderer's own time.
CREATE TABLE agent_provider_routing (
  agent_id          TEXT NOT NULL,
  runner_kind       TEXT NOT NULL,
  credential_scope  TEXT NOT NULL,
  provider_kind     TEXT NOT NULL,
  PRIMARY KEY (agent_id, runner_kind)
) STRICT, WITHOUT ROWID;

PRAGMA user_version = 3;
