package team

// scanner_patterns_test.go asserts one positive hit + one near-miss per
// named pattern in the secretPatterns catalog. "Near-miss" strings are
// the kind of prose or IDs that a naive regex would eat if the pattern
// were looser — keeping them here as regression anchors.

import (
	"strings"
	"testing"
)

func TestSecretPatternsPositiveAndNearMiss(t *testing.T) {
	// Helper for building long GitHub-style 36-char bodies.
	rep := func(c string, n int) string { return strings.Repeat(c, n) }

	cases := []struct {
		pattern string
		hit     string
		miss    string
	}{
		// AI vendors
		{"openai", "api=sk-" + rep("A", 30), "note: sk-ip or sk-prefixed short"},
		{"anthropic", "key=sk-ant-" + rep("X", 40), "sk-ant (too short, no body)"},
		{"huggingface", "token=hf_" + rep("b", 34), "hf_short"},
		{"google-api", "key=AIza" + rep("C", 35), "AIza short"},
		// Cloud
		{"aws-access-key", "id=AKIAIOSFODNN7EXAMPLE end", "AKIA is a word sometimes"},
		{"aws-secret", `aws_secret_access_key = "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY"`, "aws_secret_access_key mentioned in prose"},
		{"gcp-service-account", `{"type": "service_account"}`, `"type": "user"`},
		{"azure-storage", "AccountKey=" + rep("a", 86) + "==", "AccountKey=short=="},
		// Source control + CI
		{"github-classic", "token=ghp_" + rep("a", 36), "ghp_short"},
		{"github-fine-grained", "token=github_pat_" + rep("z", 82), "github_pat_abc"},
		{"github-oauth", "gho_" + rep("a", 36), "gho_short"},
		{"github-user-server", "ghu_" + rep("a", 36), "ghu_short"},
		{"github-server-server", "ghs_" + rep("a", 36), "ghs_short"},
		{"github-refresh", "ghr_" + rep("a", 36), "ghr_short"},
		{"gitlab-pat", "token=glpat-" + rep("y", 20), "glpat-short"},
		// Payment
		{"stripe-secret-live", "key=sk_live_" + rep("a", 24), "sk_live_ short"},
		{"stripe-restricted-live", "key=rk_live_" + rep("a", 24), "rk_live_ short"},
		{"stripe-publishable-live", "key=pk_live_" + rep("a", 24), "pk_live_ short"},
		{"stripe-secret-test", "key=sk_test_" + rep("a", 24), "sk_test_ short"},
		{"stripe-publishable-test", "key=pk_test_" + rep("a", 24), "pk_test_ short"},
		{"square-access", "EAAA" + rep("z", 60), "EAAA short"},
		// Messaging
		{"slack-token", "xoxb-" + rep("1", 20), "xox-prefix (malformed)"},
		{"slack-webhook", "https://hooks.slack.com/services/T01234/B9876/abcdef", "https://hooks.slack.com/docs"},
		{"discord-bot", "M" + rep("a", 23) + ".abcdef." + rep("z", 30), "M.abcdef.zzz"},
		{"twilio-sid", "sid=AC" + rep("a", 32), "ACmentioned"},
		{"sendgrid", "SG." + rep("a", 22) + "." + rep("b", 43), "SG.short"},
		// SSH / crypto
		{"pem-private-key", "-----BEGIN OPENSSH PRIVATE KEY-----", "BEGIN PUBLIC KEY mentioned"},
		{"ppk-private-key", "PuTTY-User-Key-File-2:", "PuTTY-User-Key-File reference"},
		// Auth / session
		{"jwt", "token=eyJ" + rep("a", 20) + ".eyJ" + rep("b", 20) + "." + rep("c", 20), "eyJ.short"},
		{"bearer", "Authorization: Bearer abc.def-012=345", "bearer in lowercase prose"},
		{"env-api-key", "STRIPE_API_KEY=sk_live_whatever", "API_KEY by itself"},
		{"env-secret", "DB_SECRET=abcdef", "SECRET mentioned in prose"},
		{"env-token", "OAUTH_TOKEN=abc123", "TOKEN mentioned in prose"},
		// DB URIs
		{"postgres-uri", "DATABASE_URL=postgres://user:pass@host/db", "postgres://host/db (no creds)"},
		{"mysql-uri", "DATABASE_URL=mysql://u:p@host/db", "mysql://host/db"},
		{"mongodb-uri", "MONGO=mongodb+srv://u:p@cluster.mongodb.net/db", "mongodb://localhost"},
		{"redis-uri", "REDIS=redis://:pw@host:6379", "redis://localhost:6379"},
		{"inline-password", `cfg = {"password": "hunter2!!"}`, "password field: not set"},
	}

	for _, tc := range cases {
		t.Run(tc.pattern, func(t *testing.T) {
			// Find the pattern in the catalog.
			var found *SecretPattern
			for i := range secretPatterns {
				if secretPatterns[i].Name == tc.pattern {
					found = &secretPatterns[i]
					break
				}
			}
			if found == nil {
				t.Fatalf("pattern %q not in catalog", tc.pattern)
			}
			if !found.Pattern.MatchString(tc.hit) {
				t.Errorf("expected hit for %q in %q", tc.pattern, tc.hit)
			}
			if found.Pattern.MatchString(tc.miss) {
				t.Errorf("unexpected hit for %q in near-miss %q", tc.pattern, tc.miss)
			}
		})
	}
}

func TestRedactSecretsDetailedRecordsReasons(t *testing.T) {
	in := "Authorization: Bearer abcdef012345\n" +
		"OPENAI_API_KEY=sk-" + strings.Repeat("a", 30) + "\n" +
		"plain prose line"
	res := redactSecretsDetailed(in)
	if res.PatternHits == 0 {
		t.Fatalf("expected pattern hits, got 0. result=%+v", res)
	}
	if !strings.Contains(res.Content, "[REDACTED]") {
		t.Fatalf("expected [REDACTED] in scrubbed body, got %q", res.Content)
	}
	names := map[string]bool{}
	for _, r := range res.Reasons {
		if r.Kind == "pattern" {
			names[r.Name] = true
		}
	}
	if !names["bearer"] {
		t.Errorf("expected a 'bearer' reason, got %+v", res.Reasons)
	}
}

func TestFormatRedactionReasonsStableOutput(t *testing.T) {
	reasons := []RedactionReason{
		{Kind: "pattern", Name: "openai"},
		{Kind: "pattern", Name: "aws-access-key"},
		{Kind: "entropy", Bits: 5.43},
	}
	got := formatRedactionReasons(reasons)
	want := "pattern: openai, pattern: aws-access-key, entropy: ~5.43 bits"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
	if formatRedactionReasons(nil) != "unknown" {
		t.Fatalf("nil reasons should render as 'unknown'")
	}
}
