package team

// broker_slack_spawn.go spawns WUPHF-runtime office agents that carry their
// OWN Slack identity — the INVERSE of broker_slack_agents.go's foreign
// registry. A foreign agent runs outside WUPHF and its Slack posts are
// ALLOWED inbound; a spawned agent runs on WUPHF's own runtime, posts to
// Slack with its own bot token (so it appears as a real Slack user with its
// own name and avatar), and its Slack posts must NEVER re-ingress as new
// inbound — that is the echo guard in slack_transport.go's routeInbound.
//
// Creating a Slack app cannot be automated end-to-end, so the flow is guided
// and two-phase:
//
//	POST /slack/agents/spawn          { slug, name, role? }
//	  → a ready-to-paste Slack app manifest + a numbered human guide +
//	    a persisted pending-spawn record (survives restarts)
//	GET  /slack/agents/spawn          → { spawns: [ … ] }
//	POST /slack/agents/spawn/complete { slug }
//	  → reads the bot token from env var WUPHF_SLACK_AGENT_<SLUG>_TOKEN
//	    (NEVER from the request body), auth.tests it to discover the bot's
//	    Slack user id, and creates the member as a REAL office agent on the
//	    install-default runtime carrying its Slack identity in
//	    Provider.Slack (UserID + BotTokenEnv).
//
// Only env-var NAMES are persisted; raw tokens live exclusively in process
// env (same posture as channelSurface.BotTokenEnv in broker_slack_connect.go).

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/slack-go/slack"

	"github.com/nex-crm/wuphf/internal/provider"
)

// slackSpawnRecord is the persisted pending spawn: an agent the human was
// guided to create in Slack but whose token has not been completed yet.
// Stored in broker state (slug → record) so the flow survives restarts.
type slackSpawnRecord struct {
	Slug      string `json:"slug"`
	Name      string `json:"name"`
	Role      string `json:"role,omitempty"`
	TokenEnv  string `json:"token_env"`
	CreatedAt string `json:"created_at"`
}

// errSlackSpawnTokenMissing tags "the env var is not set yet" so the handler
// can answer 409 (come back after step 5 of the guide) instead of 500.
var errSlackSpawnTokenMissing = errors.New("spawned agent bot token env var not set")

// errSlackSpawnNotFound tags "no pending spawn for this slug" for a 404.
var errSlackSpawnNotFound = errors.New("no pending spawn for slug")

// slackSpawnAuthTestFunc is the auth.test seam for the spawn-complete flow:
// given a bot token it returns the bot's own Slack user id + display name.
// The Broker field slackSpawnAuthTest overrides it in tests; nil means the
// real Slack Web API (realSlackSpawnAuthTest).
type slackSpawnAuthTestFunc func(ctx context.Context, token string) (userID, botName string, err error)

// realSlackSpawnAuthTest runs auth.test against the real Slack Web API with
// the candidate bot token.
func realSlackSpawnAuthTest(ctx context.Context, token string) (string, string, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	resp, err := slack.New(token).AuthTestContext(ctx)
	if err != nil {
		return "", "", err
	}
	return resp.UserID, strings.TrimSpace(resp.User), nil
}

type slackSpawnRequest struct {
	Slug string `json:"slug,omitempty"`
	Name string `json:"name,omitempty"`
	Role string `json:"role,omitempty"`
}

type slackSpawnCompleteRequest struct {
	Slug string `json:"slug,omitempty"`
}

// slackAppManifest is the minimal Slack app manifest a spawned agent needs.
// Spawned agents only POST as themselves via chat.postMessage (inbound still
// flows through the main wuphf bot's Socket Mode connection), so there is no
// socket mode, no event subscriptions, and only two scopes.
type slackAppManifest struct {
	DisplayInformation slackManifestDisplay  `json:"display_information"`
	Features           slackManifestFeatures `json:"features"`
	OauthConfig        slackManifestOauth    `json:"oauth_config"`
}

type slackManifestDisplay struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

type slackManifestFeatures struct {
	BotUser slackManifestBotUser `json:"bot_user"`
}

type slackManifestBotUser struct {
	DisplayName  string `json:"display_name"`
	AlwaysOnline bool   `json:"always_online"`
}

type slackManifestOauth struct {
	Scopes slackManifestScopes `json:"scopes"`
}

type slackManifestScopes struct {
	Bot []string `json:"bot"`
}

// slackSpawnManifest builds the ready-to-paste app manifest for one spawned
// agent. Display name = the agent's office name so the Slack identity reads
// as the agent, not as "wuphf-2".
func slackSpawnManifest(name string) slackAppManifest {
	return slackAppManifest{
		DisplayInformation: slackManifestDisplay{
			Name:        name,
			Description: fmt.Sprintf("%s — a WUPHF office agent posting as itself.", name),
		},
		Features: slackManifestFeatures{
			BotUser: slackManifestBotUser{DisplayName: name, AlwaysOnline: true},
		},
		OauthConfig: slackManifestOauth{
			Scopes: slackManifestScopes{Bot: []string{"chat:write", "users:read"}},
		},
	}
}

// slackSpawnTokenEnv derives the env var NAME the spawned agent's bot token
// must live in: WUPHF_SLACK_AGENT_<SLUG>_TOKEN with non-alphanumerics mapped
// to underscores. Only the name transits responses and state.
func slackSpawnTokenEnv(slug string) string {
	mapped := strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z':
			return r - ('a' - 'A')
		case (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9'):
			return r
		default:
			return '_'
		}
	}, slug)
	return "WUPHF_SLACK_AGENT_" + mapped + "_TOKEN"
}

// slackSpawnGuide is the numbered human guide returned with the manifest.
func slackSpawnGuide(name, tokenEnv, slug string) []string {
	return []string{
		`1. Open https://api.slack.com/apps?new_app=1, choose "From a manifest", and pick the Slack workspace your office is bridged to.`,
		"2. Paste the manifest JSON from this response and create the app.",
		`3. Under "OAuth & Permissions", click "Install to Workspace", approve, and copy the Bot User OAuth Token (starts with xoxb-).`,
		fmt.Sprintf("4. In the bridged Slack channel, invite the new bot: /invite @%s.", name),
		fmt.Sprintf("5. Set the token in the WUPHF broker's environment as %s (never paste the raw token into a request body) and restart WUPHF so the env var is visible.", tokenEnv),
		fmt.Sprintf(`6. Finish with: POST /slack/agents/spawn/complete {"slug":%q} — WUPHF verifies the token, discovers the bot's Slack user id, and creates the office agent.`, slug),
	}
}

// handleSlackAgentsSpawn serves the guided spawn flow: POST records a pending
// spawn and returns the manifest + guide; GET lists pending spawns.
func (b *Broker) handleSlackAgentsSpawn(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, http.StatusOK, map[string]any{"spawns": b.pendingSlackSpawns()})
	case http.MethodPost:
		var body slackSpawnRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "invalid json", http.StatusBadRequest)
			return
		}
		// Pick the non-empty slug source BEFORE normalizing —
		// normalizeChannelSlug("") falls back to "general" (same gotcha as
		// handleSlackAgents).
		raw := strings.TrimSpace(body.Slug)
		if raw == "" {
			raw = strings.TrimSpace(body.Name)
		}
		slug := normalizeChannelSlug(raw)
		if raw == "" || slug == "" || slug == "general" {
			http.Error(w, "could not derive a usable slug; pass slug explicitly", http.StatusBadRequest)
			return
		}
		name := strings.TrimSpace(body.Name)
		if name == "" {
			name = humanizeSlug(slug)
		}
		rec, err := b.SpawnSlackAgent(slug, name, strings.TrimSpace(body.Role))
		if err != nil {
			http.Error(w, err.Error(), http.StatusConflict)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"slug":      rec.Slug,
			"name":      rec.Name,
			"role":      rec.Role,
			"token_env": rec.TokenEnv,
			"manifest":  slackSpawnManifest(rec.Name),
			"guide":     slackSpawnGuide(rec.Name, rec.TokenEnv, rec.Slug),
		})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleSlackAgentsSpawnComplete finishes a pending spawn: resolves the bot
// token from the recorded env var, auth.tests it, and creates the member.
func (b *Broker) handleSlackAgentsSpawnComplete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var body slackSpawnCompleteRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(body.Slug) == "" {
		http.Error(w, "slug required", http.StatusBadRequest)
		return
	}
	slug := normalizeChannelSlug(body.Slug)
	userID, created, err := b.CompleteSlackAgentSpawn(r.Context(), slug)
	switch {
	case errors.Is(err, errSlackSpawnNotFound):
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	case errors.Is(err, errSlackSpawnTokenMissing):
		http.Error(w, err.Error(), http.StatusConflict)
		return
	case err != nil:
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"slug": slug, "user_id": userID, "created": created,
	})
}

// SpawnSlackAgent records a pending spawn for slug. Idempotent for the same
// slug (re-spawning re-issues the guide and keeps the original CreatedAt);
// a slug that already names an office member — native, foreign, or a
// previously-completed spawn — is rejected.
func (b *Broker) SpawnSlackAgent(slug, name, role string) (slackSpawnRecord, error) {
	if slug == "" || slug == "general" {
		return slackSpawnRecord{}, fmt.Errorf("slug %q is reserved", slug)
	}
	// Serialize with member mutations so the existence check and the record
	// write cannot race a concurrent registration of the same slug.
	b.officeMemberMutationMu.Lock()
	defer b.officeMemberMutationMu.Unlock()
	if b.memberExists(slug) {
		return slackSpawnRecord{}, fmt.Errorf("slug %q already names an office member", slug)
	}
	rec := slackSpawnRecord{
		Slug:      slug,
		Name:      strings.TrimSpace(name),
		Role:      strings.TrimSpace(role),
		TokenEnv:  slackSpawnTokenEnv(slug),
		CreatedAt: time.Now().UTC().Format(time.RFC3339),
	}
	if rec.Name == "" {
		rec.Name = humanizeSlug(slug)
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.slackSpawns == nil {
		b.slackSpawns = make(map[string]slackSpawnRecord)
	}
	if old, ok := b.slackSpawns[slug]; ok {
		rec.CreatedAt = old.CreatedAt
	}
	b.slackSpawns[slug] = rec
	if err := b.saveLocked(); err != nil {
		return slackSpawnRecord{}, fmt.Errorf("persist pending spawn %q: %w", slug, err)
	}
	return rec, nil
}

// CompleteSlackAgentSpawn finishes the pending spawn for slug: it resolves
// the bot token from the recorded env var (never from a request body), runs
// auth.test to discover the bot's Slack user id, and creates the office
// member as a REAL agent on the install-default runtime (Provider.Kind == ""),
// carrying its Slack identity in Provider.Slack. Idempotent: re-completing a
// spawn whose member already exists with the same identity reports
// created=false.
func (b *Broker) CompleteSlackAgentSpawn(ctx context.Context, slug string) (userID string, created bool, err error) {
	if slug == "" || slug == "general" {
		return "", false, fmt.Errorf("slug %q is reserved", slug)
	}
	b.mu.Lock()
	rec, ok := b.slackSpawns[slug]
	b.mu.Unlock()
	if !ok {
		return "", false, fmt.Errorf("%w: %q (POST /slack/agents/spawn first)", errSlackSpawnNotFound, slug)
	}
	token := strings.TrimSpace(os.Getenv(rec.TokenEnv))
	if token == "" {
		return "", false, fmt.Errorf("%w: set %s to the bot's xoxb- token in the broker's environment, then retry", errSlackSpawnTokenMissing, rec.TokenEnv)
	}
	authTest := b.slackSpawnAuthTest
	if authTest == nil {
		authTest = realSlackSpawnAuthTest
	}
	userID, _, err = authTest(ctx, token)
	if err != nil {
		return "", false, fmt.Errorf("slack auth.test with %s: %w", rec.TokenEnv, err)
	}
	if !isSlackUserID(userID) {
		return "", false, fmt.Errorf("slack auth.test returned an invalid bot user id %q", userID)
	}

	// Serialize with every other member mutation so the conflict check and
	// the create step are atomic against concurrent registrations (same
	// outer lock RegisterSlackAgent takes).
	b.officeMemberMutationMu.Lock()
	defer b.officeMemberMutationMu.Unlock()
	alreadyCompleted, conflictErr := b.slackSpawnConflict(slug, userID)
	if conflictErr != nil {
		return "", false, conflictErr
	}
	if alreadyCompleted {
		// Member already exists with exactly this identity (e.g. a retried
		// complete after a crash between create and record cleanup).
		if err := b.clearSlackSpawnRecord(slug); err != nil {
			return "", false, err
		}
		return userID, false, nil
	}
	if err := b.createSpawnedSlackMember(rec, userID); err != nil {
		return "", false, err
	}
	if err := b.clearSlackSpawnRecord(slug); err != nil {
		return "", false, err
	}
	return userID, true, nil
}

// slackSpawnConflict checks the spawn-flavored 1:1 invariants under the
// broker lock: the slug must be free (or already completed with exactly this
// Slack identity → alreadyCompleted=true), and the Slack user id must not be
// bound to any other member — foreign or spawned.
func (b *Broker) slackSpawnConflict(slug, userID string) (alreadyCompleted bool, err error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for i := range b.members {
		m := &b.members[i]
		boundID := ""
		if m.Provider.Slack != nil {
			boundID = m.Provider.Slack.UserID
		}
		if m.Slug == slug {
			if boundID == userID && m.Provider.Kind != provider.KindSlack {
				return true, nil // idempotent re-complete
			}
			if boundID != "" {
				return false, fmt.Errorf("slug %q already bound to slack user %s", slug, boundID)
			}
			return false, fmt.Errorf("slug %q already names an office member", slug)
		}
		if boundID == userID {
			return false, fmt.Errorf("slack user %s already registered as %q", userID, m.Slug)
		}
	}
	return false, nil
}

// createSpawnedSlackMember persists the spawned agent as a real office
// member: install-default runtime (Kind == ""), Slack identity on the
// binding, seeded into every non-DM channel so its replies are not 403'd
// (same policy and rationale as createOfficeMember).
func (b *Broker) createSpawnedSlackMember(rec slackSpawnRecord, userID string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	member := officeMember{
		Slug:      rec.Slug,
		Name:      rec.Name,
		Role:      rec.Role,
		CreatedBy: "slack-spawn",
		CreatedAt: now,
		Provider: provider.ProviderBinding{
			// Kind stays empty → install-wide default runtime: a spawned
			// agent is a REAL office agent, not a gateway/foreign kind.
			Slack: &provider.SlackProviderBinding{UserID: userID, BotTokenEnv: rec.TokenEnv},
		},
	}
	applyOfficeMemberDefaults(&member)

	b.mu.Lock()
	if b.findMemberLocked(member.Slug) != nil {
		b.mu.Unlock()
		return fmt.Errorf("member %q already exists", member.Slug)
	}
	// Append only — findMemberLocked's length-check lazily rebuilds the
	// member index (same shape as EnsureBridgedMember).
	b.members = append(b.members, member)
	updatedChannels := make([]string, 0, len(b.channels))
	for i := range b.channels {
		if b.channels[i].isDM() {
			continue
		}
		if !containsString(b.channels[i].Members, member.Slug) {
			b.channels[i].Members = append(b.channels[i].Members, member.Slug)
			b.channels[i].UpdatedAt = now
			updatedChannels = append(updatedChannels, b.channels[i].Slug)
		}
	}
	if err := b.saveLocked(); err != nil {
		b.mu.Unlock()
		return fmt.Errorf("persist spawned member %q: %w", member.Slug, err)
	}
	b.publishOfficeChangeLocked(officeChangeEvent{Kind: "member_created", Slug: member.Slug})
	for _, chSlug := range updatedChannels {
		b.publishOfficeChangeLocked(officeChangeEvent{Kind: "channel_updated", Slug: chSlug})
	}
	b.mu.Unlock()
	b.backfillAgentFilesForRoster()
	return nil
}

// clearSlackSpawnRecord deletes the pending record after a successful
// complete and persists the deletion.
func (b *Broker) clearSlackSpawnRecord(slug string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if _, ok := b.slackSpawns[slug]; !ok {
		return nil
	}
	delete(b.slackSpawns, slug)
	if err := b.saveLocked(); err != nil {
		return fmt.Errorf("persist spawn cleanup %q: %w", slug, err)
	}
	return nil
}

// pendingSlackSpawns lists the pending spawn records, slug-sorted for a
// deterministic GET response.
func (b *Broker) pendingSlackSpawns() []slackSpawnRecord {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]slackSpawnRecord, 0, len(b.slackSpawns))
	for _, rec := range b.slackSpawns {
		out = append(out, rec)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Slug < out[j].Slug })
	return out
}

// cloneSlackSpawnsLocked snapshots the pending-spawn registry for
// persistence. Caller holds b.mu. nil when empty so the state file omits it.
func (b *Broker) cloneSlackSpawnsLocked() map[string]slackSpawnRecord {
	if len(b.slackSpawns) == 0 {
		return nil
	}
	out := make(map[string]slackSpawnRecord, len(b.slackSpawns))
	for slug, rec := range b.slackSpawns {
		out[slug] = rec
	}
	return out
}

// IsSpawnedSlackAgentUserID reports whether userID is the Slack identity of a
// SPAWNED office agent — a member running on WUPHF's own runtime (Kind !=
// KindSlack) that posts to Slack as itself. This is the transport's ECHO
// GUARD lookup: such posts are office-originated and must never re-ingress
// as new inbound (the inverse of SlackAgentSlugByUserID, the foreign-agent
// ingress ALLOWLIST).
func (b *Broker) IsSpawnedSlackAgentUserID(userID string) bool {
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return false
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	for i := range b.members {
		m := &b.members[i]
		if m.Provider.Slack != nil && m.Provider.Kind != provider.KindSlack && m.Provider.Slack.UserID == userID {
			return true
		}
	}
	return false
}

// SpawnedSlackAgentTokenEnv returns the env-var NAME holding the bot token a
// spawned agent posts to Slack with, or "" when slug is not a spawned agent.
// The outbound posting hook (slack_spawned_agents.go) keys on this.
func (b *Broker) SpawnedSlackAgentTokenEnv(slug string) string {
	// Empty-check BEFORE normalizing: normalizeChannelSlug("") == "general".
	if strings.TrimSpace(slug) == "" {
		return ""
	}
	slug = normalizeChannelSlug(slug)
	b.mu.Lock()
	defer b.mu.Unlock()
	m := b.findMemberLocked(slug)
	if m == nil || m.Provider.Slack == nil || m.Provider.Kind == provider.KindSlack {
		return ""
	}
	return m.Provider.Slack.BotTokenEnv
}
