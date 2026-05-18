PRAGMA foreign_keys = ON;

CREATE INDEX webauthn_challenges_expires_at_ms_idx
  ON webauthn_challenges (expires_at_ms);

CREATE INDEX webauthn_consumed_tokens_expires_at_ms_idx
  ON webauthn_consumed_tokens (expires_at_ms);

PRAGMA user_version = 5;
