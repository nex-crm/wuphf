package team

// scanner_patterns.go is the exhaustive secret-shape catalog used by the
// scanner's redaction pass. Each entry carries a stable Name so the human-
// facing skip log can say *why* a file was quarantined (`pattern: aws-akia`
// beats an anonymous regex hit every time).
//
// The catalog is grouped by source — AI vendors, cloud providers, payment
// processors, SSH/crypto material, JWT, and database URIs. Adding a new
// shape is a one-liner: append a SecretPattern and add a positive + near-
// miss case to scanner_patterns_test.go.
//
// Conservative-by-default rule of thumb: false positives cost one
// [REDACTED] marker in prose. False negatives leak credentials into a
// git-backed wiki forever. When in doubt, match.

import "regexp"

// SecretPattern pairs a stable identifier with its detection regex. The
// Name is surfaced in the skip-log so humans can trace a redaction back
// to the specific rule that fired.
//
// A Poison pattern triggers the file-level skip on the first hit,
// regardless of the per-file match cap. Use it for shapes where even a
// single occurrence is sufficient evidence of a secret-bearing file
// (PEM private key armor, GCP service-account JSON, etc.) — the
// surrounding body is almost certainly dangerous to ingest.
type SecretPattern struct {
	Name    string
	Pattern *regexp.Regexp
	Poison  bool
}

// secretPatterns is the full catalog. Order is not meaningful — every
// pattern is evaluated on every pass. Keep patterns anchored tightly
// enough that they do not eat surrounding prose.
//
// Sources referenced during construction:
//   - OpenAI, Anthropic, Google, HuggingFace token format docs
//   - AWS IAM key-id reference (AKIA / ASIA / AGPA / AIPA prefixes)
//   - Stripe API key format (sk_live, pk_live, rk_live, sk_test, pk_test)
//   - Slack API token format (xoxb, xoxp, xoxa, xoxr, xoxs)
//   - OpenSSH and PEM-encoded private key armor
//   - RFC 7519 JWT compact serialization (three base64url segments)
//   - libpq / MongoDB connection string syntax with embedded credentials
//   - gitleaks rules.toml (pattern shapes adopted, binary NOT required)
var secretPatterns = []SecretPattern{
	// --- AI vendors ---

	// OpenAI legacy + project-scoped keys. `sk-` is shared across many
	// vendors, so the length floor keeps this from eating short test
	// tokens like `sk-test`. Project keys carry a `sk-proj-` prefix but
	// match the same parent pattern; catalogued separately for clarity.
	{Name: "openai", Pattern: regexp.MustCompile(`sk-(?:proj-)?[A-Za-z0-9_-]{20,}`)},

	// Anthropic public-API keys. `sk-ant-` prefix is unique, followed by
	// a long opaque body. Keep the length floor generous — Anthropic has
	// rotated the body format before.
	{Name: "anthropic", Pattern: regexp.MustCompile(`sk-ant-[A-Za-z0-9_-]{20,}`)},

	// HuggingFace user-access tokens. `hf_` prefix + 30+ chars. Distinct
	// from HF write tokens which share the format.
	{Name: "huggingface", Pattern: regexp.MustCompile(`hf_[A-Za-z0-9]{30,}`)},

	// Google Cloud / Gemini API keys. `AIza` prefix + exactly 35 chars.
	// The length anchor keeps this from matching AWS AKIA-ish prose.
	{Name: "google-api", Pattern: regexp.MustCompile(`AIza[0-9A-Za-z_-]{35}`)},

	// --- Cloud providers ---

	// AWS access-key IDs — AKIA (long-lived user), ASIA (STS temporary),
	// AGPA (group), AIPA (instance profile), ANPA (managed policy), AROA
	// (role), AIDA (user), ABIA (bucket). All 20 chars total.
	{Name: "aws-access-key", Pattern: regexp.MustCompile(`\b(?:AKIA|ASIA|AGPA|AIPA|ANPA|AROA|AIDA|ABIA)[0-9A-Z]{16}\b`)},

	// AWS secret keys have no fixed prefix, only a 40-char base64-ish
	// body that follows an `aws_secret_access_key` assignment. Catching
	// the assignment form keeps false positives low.
	{Name: "aws-secret", Pattern: regexp.MustCompile(`(?i)aws_secret_access_key\s*[:=]\s*["']?[A-Za-z0-9/+=]{40}["']?`), Poison: true},

	// Google service-account JSON private keys ship as a multi-line
	// string with escaped newlines. Match the `"type": "service_account"`
	// marker — finding it inline is a near-certain leak.
	{Name: "gcp-service-account", Pattern: regexp.MustCompile(`"type"\s*:\s*"service_account"`), Poison: true},

	// Azure storage connection strings carry `AccountKey=<base64>;` and
	// are frequently pasted whole into docs.
	{Name: "azure-storage", Pattern: regexp.MustCompile(`AccountKey=[A-Za-z0-9+/]{86}==`)},

	// --- Source control + CI ---

	// GitHub personal-access tokens (classic, 40 chars), fine-grained
	// PATs (`github_pat_`), OAuth tokens (`gho_`), user-to-server
	// (`ghu_`), server-to-server (`ghs_`), refresh tokens (`ghr_`).
	{Name: "github-classic", Pattern: regexp.MustCompile(`ghp_[A-Za-z0-9]{36}`)},
	{Name: "github-fine-grained", Pattern: regexp.MustCompile(`github_pat_[A-Za-z0-9_]{82}`)},
	{Name: "github-oauth", Pattern: regexp.MustCompile(`gho_[A-Za-z0-9]{36}`)},
	{Name: "github-user-server", Pattern: regexp.MustCompile(`ghu_[A-Za-z0-9]{36}`)},
	{Name: "github-server-server", Pattern: regexp.MustCompile(`ghs_[A-Za-z0-9]{36}`)},
	{Name: "github-refresh", Pattern: regexp.MustCompile(`ghr_[A-Za-z0-9]{36}`)},

	// GitLab personal-access tokens carry a `glpat-` prefix + 20 chars.
	{Name: "gitlab-pat", Pattern: regexp.MustCompile(`glpat-[A-Za-z0-9_-]{20}`)},

	// --- Payment processors ---

	// Stripe secret (live + test) keys. `rk_` is the restricted variant;
	// it carries the same risk. `pk_live_` is publishable but disclosing
	// it still signals account linkage so we redact it too.
	{Name: "stripe-secret-live", Pattern: regexp.MustCompile(`sk_live_[A-Za-z0-9]{24,}`)},
	{Name: "stripe-restricted-live", Pattern: regexp.MustCompile(`rk_live_[A-Za-z0-9]{24,}`)},
	{Name: "stripe-publishable-live", Pattern: regexp.MustCompile(`pk_live_[A-Za-z0-9]{24,}`)},
	{Name: "stripe-secret-test", Pattern: regexp.MustCompile(`sk_test_[A-Za-z0-9]{24,}`)},
	{Name: "stripe-publishable-test", Pattern: regexp.MustCompile(`pk_test_[A-Za-z0-9]{24,}`)},

	// Square access tokens carry an `EAAA` prefix + 60+ chars of base64.
	{Name: "square-access", Pattern: regexp.MustCompile(`EAAA[A-Za-z0-9_-]{60,}`)},

	// --- Messaging / comms ---

	// Slack bot (xoxb), user (xoxp), app-level (xoxa), refresh (xoxr),
	// workspace (xoxs) and legacy (xoxe) tokens. The suffix varies in
	// length; 10+ chars is the conservative floor.
	{Name: "slack-token", Pattern: regexp.MustCompile(`xox[baprse]-[A-Za-z0-9-]{10,}`)},

	// Slack incoming-webhook URLs are effectively bearer tokens.
	{Name: "slack-webhook", Pattern: regexp.MustCompile(`https://hooks\.slack\.com/services/T[A-Za-z0-9]+/B[A-Za-z0-9]+/[A-Za-z0-9]+`)},

	// Discord bot tokens are three dot-separated base64 segments.
	{Name: "discord-bot", Pattern: regexp.MustCompile(`[MN][A-Za-z0-9_-]{23}\.[A-Za-z0-9_-]{6}\.[A-Za-z0-9_-]{27,}`)},

	// Twilio account SID (`AC` + 32 hex) plus its auth-token sibling.
	{Name: "twilio-sid", Pattern: regexp.MustCompile(`\bAC[0-9a-fA-F]{32}\b`)},

	// SendGrid API keys.
	{Name: "sendgrid", Pattern: regexp.MustCompile(`SG\.[A-Za-z0-9_-]{22}\.[A-Za-z0-9_-]{43}`)},

	// --- SSH and crypto material ---

	// Any PEM-armored private-key block. OpenSSH, RSA, DSA, EC, PGP,
	// and encrypted variants all share the `-----BEGIN ... PRIVATE KEY`
	// line. Presence of even one is a quarantine trigger.
	{Name: "pem-private-key", Pattern: regexp.MustCompile(`-----BEGIN (?:OPENSSH|RSA|DSA|EC|ENCRYPTED|PGP)? ?PRIVATE KEY-----`), Poison: true},

	// Putty .ppk header.
	{Name: "ppk-private-key", Pattern: regexp.MustCompile(`PuTTY-User-Key-File-[23]:`), Poison: true},

	// --- Auth / session ---

	// JWT compact serialization: three base64url segments separated by
	// dots, starting with an `eyJ` (a base64-encoded `{"`). Requires a
	// body segment of meaningful length to avoid eating short prose.
	{Name: "jwt", Pattern: regexp.MustCompile(`eyJ[A-Za-z0-9_-]{10,}\.eyJ[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}`)},

	// Generic Bearer tokens in an HTTP Authorization header.
	{Name: "bearer", Pattern: regexp.MustCompile(`Bearer [A-Za-z0-9_.=\-]{10,}`)},

	// Any `*_API_KEY=...` or `*_SECRET=...` env-style assignment. Broad
	// and noisy, but the file-level threshold keeps it from clobbering
	// legitimate docs about env-var naming.
	{Name: "env-api-key", Pattern: regexp.MustCompile(`[A-Z][A-Z0-9_]*_API_KEY\s*=\s*\S+`)},
	{Name: "env-secret", Pattern: regexp.MustCompile(`[A-Z][A-Z0-9_]*_SECRET\s*=\s*\S+`)},
	{Name: "env-token", Pattern: regexp.MustCompile(`[A-Z][A-Z0-9_]*_TOKEN\s*=\s*\S+`)},

	// --- Database URIs with embedded credentials ---

	// Postgres / PostgreSQL URI with user:pass@host. The password group
	// is the sensitive bit; match the whole URI for context.
	{Name: "postgres-uri", Pattern: regexp.MustCompile(`postgres(?:ql)?://[^\s:/@]+:[^\s@]+@[^\s/]+`)},

	// MySQL connection URI.
	{Name: "mysql-uri", Pattern: regexp.MustCompile(`mysql://[^\s:/@]+:[^\s@]+@[^\s/]+`)},

	// MongoDB URI — both `mongodb://` and SRV `mongodb+srv://`.
	{Name: "mongodb-uri", Pattern: regexp.MustCompile(`mongodb(?:\+srv)?://[^\s:/@]+:[^\s@]+@[^\s/]+`)},

	// Redis URI (both plain and TLS).
	{Name: "redis-uri", Pattern: regexp.MustCompile(`rediss?://[^\s:/@]*:[^\s@]+@[^\s/]+`)},

	// Generic `password=...` and `passwd=...` assignments inside URLs
	// or config blobs. Narrow enough to require quotes or `&`/`;`.
	{Name: "inline-password", Pattern: regexp.MustCompile(`(?i)["']?(?:password|passwd|pwd)["']?\s*[:=]\s*["'][^"'\s]{6,}["']`)},
}
