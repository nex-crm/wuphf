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
-- - Enum values are duplicated from packages/protocol/src/runner.ts,
--   packages/protocol/src/credential-handle.ts, and
--   packages/protocol/src/receipt.ts. If a future branch adds a value to
--   those enums, ship a new migration that recreates this table; do not edit
--   this file post-merge.
CREATE TABLE agent_provider_routing (
  agent_id          TEXT NOT NULL,
  runner_kind       TEXT NOT NULL
    CONSTRAINT agent_provider_routing_runner_kind_check
    CHECK (runner_kind IN ('claude-cli', 'codex-cli', 'openai-compat')),
  credential_scope  TEXT NOT NULL
    CONSTRAINT agent_provider_routing_credential_scope_check
    CHECK (
      credential_scope IN (
        'anthropic',
        'openai',
        'openai-compat',
        'ollama',
        'openclaw',
        'hermes-agent',
        'openclaw-http',
        'opencode',
        'opencodego',
        'github'
      )
    ),
  provider_kind     TEXT NOT NULL
    CONSTRAINT agent_provider_routing_provider_kind_check
    CHECK (
      provider_kind IN (
        'anthropic',
        'openai',
        'openai-compat',
        'ollama',
        'openclaw',
        'hermes-agent',
        'openclaw-http',
        'opencode',
        'opencodego'
      )
    ),
  PRIMARY KEY (agent_id, runner_kind)
) STRICT, WITHOUT ROWID;

PRAGMA user_version = 3;
