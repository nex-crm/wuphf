PRAGMA foreign_keys = ON;

-- Branch 12 WebAuthn co-sign persistence.
--
-- The tables are broker-local control-plane state, not protocol wire. Challenge
-- rows bind random WebAuthn challenges to a canonical claim/scope preimage
-- hash; consumed-token rows make replay idempotent after successful verifier
-- execution. Role values intentionally duplicate @wuphf/protocol's
-- ApprovalRole enum so SQLite rejects drift at the storage boundary.
CREATE TABLE webauthn_registered_credentials (
  credential_id      TEXT NOT NULL
    CONSTRAINT webauthn_registered_credentials_id_base64url_check
    CHECK (length(credential_id) > 0 AND credential_id NOT GLOB '*[^A-Za-z0-9_-]*'),
  public_key_b64url  TEXT NOT NULL
    CONSTRAINT webauthn_registered_credentials_public_key_base64url_check
    CHECK (length(public_key_b64url) > 0 AND public_key_b64url NOT GLOB '*[^A-Za-z0-9_-]*'),
  sign_count         INTEGER NOT NULL
    CONSTRAINT webauthn_registered_credentials_sign_count_check
    CHECK (sign_count >= 0),
  role               TEXT NOT NULL
    CONSTRAINT webauthn_registered_credentials_role_check
    CHECK (role IN ('viewer', 'approver', 'host')),
  agent_id           TEXT NOT NULL,
  created_at_ms      INTEGER NOT NULL
    CONSTRAINT webauthn_registered_credentials_created_at_ms_check
    CHECK (created_at_ms >= 0),
  PRIMARY KEY (credential_id)
) STRICT, WITHOUT ROWID;

CREATE INDEX webauthn_registered_credentials_agent_role_idx
  ON webauthn_registered_credentials (agent_id, role);

CREATE TABLE webauthn_challenges (
  challenge_id         TEXT NOT NULL
    CONSTRAINT webauthn_challenges_challenge_id_base64url_check
    CHECK (length(challenge_id) > 0 AND challenge_id NOT GLOB '*[^A-Za-z0-9_-]*'),
  challenge_type       TEXT NOT NULL
    CONSTRAINT webauthn_challenges_type_check
    CHECK (challenge_type IN ('registration', 'cosign')),
  challenge_b64url     TEXT NOT NULL
    CONSTRAINT webauthn_challenges_challenge_base64url_check
    CHECK (length(challenge_b64url) > 0 AND challenge_b64url NOT GLOB '*[^A-Za-z0-9_-]*'),
  token_id             TEXT
    CONSTRAINT webauthn_challenges_token_id_check
    CHECK (token_id IS NULL OR (length(token_id) = 26 AND token_id NOT GLOB '*[^0-9A-HJKMNP-TV-Z]*')),
  role                 TEXT
    CONSTRAINT webauthn_challenges_role_check
    CHECK (role IS NULL OR role IN ('viewer', 'approver', 'host')),
  agent_id             TEXT NOT NULL,
  claim_json           TEXT,
  scope_json           TEXT,
  claim_scope_hash     TEXT
    CONSTRAINT webauthn_challenges_claim_scope_hash_check
    CHECK (claim_scope_hash IS NULL OR (length(claim_scope_hash) = 64 AND claim_scope_hash NOT GLOB '*[^0-9a-f]*')),
  approval_group_hash  TEXT
    CONSTRAINT webauthn_challenges_approval_group_hash_check
    CHECK (approval_group_hash IS NULL OR (length(approval_group_hash) = 64 AND approval_group_hash NOT GLOB '*[^0-9a-f]*')),
  not_before_ms        INTEGER
    CONSTRAINT webauthn_challenges_not_before_ms_check
    CHECK (not_before_ms IS NULL OR not_before_ms >= 0),
  expires_at_ms        INTEGER NOT NULL
    CONSTRAINT webauthn_challenges_expires_at_ms_check
    CHECK (expires_at_ms >= 0),
  consumed_at_ms       INTEGER
    CONSTRAINT webauthn_challenges_consumed_at_ms_check
    CHECK (consumed_at_ms IS NULL OR consumed_at_ms >= 0),
  created_at_ms        INTEGER NOT NULL
    CONSTRAINT webauthn_challenges_created_at_ms_check
    CHECK (created_at_ms >= 0),
  PRIMARY KEY (challenge_id),
  CONSTRAINT webauthn_challenges_registration_shape_check
    CHECK (
      (challenge_type = 'registration'
        AND token_id IS NULL
        AND role IS NOT NULL
        AND claim_json IS NULL
        AND scope_json IS NULL
        AND claim_scope_hash IS NULL
        AND approval_group_hash IS NULL
        AND not_before_ms IS NULL)
      OR
      (challenge_type = 'cosign'
        AND token_id IS NOT NULL
        AND role IS NOT NULL
        AND claim_json IS NOT NULL
        AND scope_json IS NOT NULL
        AND claim_scope_hash IS NOT NULL
        AND approval_group_hash IS NOT NULL
        AND not_before_ms IS NOT NULL)
    )
) STRICT, WITHOUT ROWID;

CREATE UNIQUE INDEX webauthn_challenges_token_id_idx
  ON webauthn_challenges (token_id)
  WHERE token_id IS NOT NULL;

CREATE INDEX webauthn_challenges_agent_idx
  ON webauthn_challenges (agent_id, challenge_type);

CREATE TABLE webauthn_consumed_tokens (
  token_id             TEXT NOT NULL
    CONSTRAINT webauthn_consumed_tokens_token_id_check
    CHECK (length(token_id) = 26 AND token_id NOT GLOB '*[^0-9A-HJKMNP-TV-Z]*'),
  challenge_id         TEXT NOT NULL
    CONSTRAINT webauthn_consumed_tokens_challenge_id_base64url_check
    CHECK (length(challenge_id) > 0 AND challenge_id NOT GLOB '*[^A-Za-z0-9_-]*'),
  outcome              TEXT NOT NULL
    CONSTRAINT webauthn_consumed_tokens_outcome_check
    CHECK (outcome IN ('approval_pending', 'approved')),
  response_json        TEXT NOT NULL,
  role                 TEXT NOT NULL
    CONSTRAINT webauthn_consumed_tokens_role_check
    CHECK (role IN ('viewer', 'approver', 'host')),
  approval_group_hash  TEXT NOT NULL
    CONSTRAINT webauthn_consumed_tokens_approval_group_hash_check
    CHECK (length(approval_group_hash) = 64 AND approval_group_hash NOT GLOB '*[^0-9a-f]*'),
  agent_id             TEXT NOT NULL,
  expires_at_ms        INTEGER NOT NULL
    CONSTRAINT webauthn_consumed_tokens_expires_at_ms_check
    CHECK (expires_at_ms >= 0),
  consumed_at_ms       INTEGER NOT NULL
    CONSTRAINT webauthn_consumed_tokens_consumed_at_ms_check
    CHECK (consumed_at_ms >= 0),
  PRIMARY KEY (token_id),
  FOREIGN KEY (challenge_id) REFERENCES webauthn_challenges (challenge_id)
) STRICT, WITHOUT ROWID;

CREATE UNIQUE INDEX webauthn_consumed_tokens_challenge_idx
  ON webauthn_consumed_tokens (challenge_id);

CREATE INDEX webauthn_consumed_tokens_group_idx
  ON webauthn_consumed_tokens (approval_group_hash, agent_id, expires_at_ms);

PRAGMA user_version = 4;
