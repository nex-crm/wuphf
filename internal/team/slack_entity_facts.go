package team

// slack_entity_facts.go builds the office's entity wiki from the Slack
// surface: every human and AI agent the office can observe gets a wiki
// article under team/people/, continuously accreted from Slack profile info
// (users:read), bridged-channel membership, the office roster, and the
// foreign-agent registry. Reuses the B2 substrate end-to-end — observations
// land as append-only facts (content-hash dedup makes the pass idempotent)
// and articles regenerate deterministically ONLY when a new fact landed, so
// the recurring sync never churns the wiki repo.
//
// Agents deliberately share the `people` kind with humans: B1 task-mention
// extraction already records @agent mentions as people/<slug>, so a separate
// kind would split the same entity across two articles. What an entity IS
// (human teammate, office agent, foreign bridged agent) is stated in its
// facts instead.

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/slack-go/slack"
)

// slackEntityFactSyncInterval is the recurring cadence. Profiles and rosters
// change on human timescales; dedup makes extra passes free but each pass
// spends a users.list-shaped API call per bridged channel.
const slackEntityFactSyncInterval = 15 * time.Minute

// slackEntityFactSyncWarmup delays the first pass past boot so the transport
// has connected and warmed its user map.
const slackEntityFactSyncWarmup = 90 * time.Second

// slackEntityRecordedBy attributes the facts this pass records.
const slackEntityRecordedBy = "system"

// entityFactSinks snapshots the B2 wiring under the broker lock. Any nil
// return means the wiki backend is not active and the pass should skip.
func (b *Broker) entityFactSinks() (*FactLog, *EntityGraph, *WikiWorker) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.factLog, b.entityGraph, b.wikiWorker
}

// runEntityFactSync drives the recurring pass until ctx is cancelled.
func (t *SlackTransport) runEntityFactSync(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(slackEntityFactSyncWarmup):
	}
	t.syncEntityFactsOnce(ctx)
	ticker := time.NewTicker(slackEntityFactSyncInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
		t.syncEntityFactsOnce(ctx)
	}
}

// syncEntityFactsOnce records identity facts for every observable human and
// agent, then regenerates the wiki article of each entity that gained a new
// fact this pass.
func (t *SlackTransport) syncEntityFactsOnce(ctx context.Context) {
	if t.Broker == nil {
		return
	}
	factLog, graph, worker := t.Broker.entityFactSinks()
	// Fail closed on any missing sink: RegenerateEntityArticle dereferences the
	// graph, so a nil graph (wiki backend half-initialized) would panic the
	// sync goroutine. All three must be present before we touch the wiki.
	if factLog == nil || graph == nil || worker == nil {
		return
	}

	start := time.Now().UTC()
	touched := map[string]bool{}
	record := func(slug string, text string) {
		if slug == "" || text == "" {
			return
		}
		fact, err := factLog.Append(ctx, EntityKindPeople, slug, text, "", slackEntityRecordedBy)
		if err != nil {
			log.Printf("[slack] entity fact append failed for %s: %v", slug, err)
			return
		}
		// Append returns the ORIGINAL fact on dedup (older CreatedAt), so a
		// timestamp at-or-after `start` identifies a genuinely new fact.
		if !fact.CreatedAt.Before(start) {
			touched[slug] = true
		}
	}

	t.recordSlackUserFacts(ctx, record)
	t.recordOfficeMemberFacts(record)

	for slug := range touched {
		if err := RegenerateEntityArticle(ctx, worker, factLog, graph, EntityKindPeople, slug); err != nil {
			log.Printf("[slack] entity article regen failed for %s: %v", slug, err)
		}
	}
}

// recordSlackUserFacts walks the membership of every bridged channel,
// resolves each member's profile, and records identity + presence facts.
// The office's own bot user is skipped — the office is the observer here.
func (t *SlackTransport) recordSlackUserFacts(ctx context.Context, record func(slug, text string)) {
	if t.api == nil {
		return
	}
	t.mapsMu.RLock()
	channels := make(map[string]string, len(t.ChannelMap)) // channelID → office slug
	for channelID, slug := range t.ChannelMap {
		channels[channelID] = slug
	}
	t.mapsMu.RUnlock()

	for channelID, channelSlug := range channels {
		members, _, err := t.api.GetUsersInConversationContext(ctx, &slack.GetUsersInConversationParameters{
			ChannelID: channelID,
			Limit:     200,
		})
		if err != nil {
			log.Printf("[slack] entity sync: membership lookup failed for %s: %v", channelID, err)
			continue
		}
		for _, userID := range members {
			if userID == "" || userID == t.botUserID {
				continue
			}
			// resolveUser is the authority on human-vs-bot (it consults
			// users.info and caches the result); UserMap is only the profile
			// detail cache and can be momentarily stale on a cache miss. Trust
			// the returned `human`, not info.human, for the classification.
			name, human := t.resolveUser(ctx, userID)
			t.mapsMu.RLock()
			info := t.UserMap[userID]
			t.mapsMu.RUnlock()
			slug := slackEntitySlug(name, userID)

			// Registered foreign agents are described by the roster pass
			// (richer identity); here they only gain the presence fact.
			isForeignAgent := t.foreignAgentSlug(userID) != ""
			if !isForeignAgent {
				if human {
					record(slug, fmt.Sprintf("Human teammate on Slack — display name %q (user id %s).", name, userID))
					if info.realName != "" && info.realName != name {
						record(slug, fmt.Sprintf("Full name: %s.", info.realName))
					}
					if info.title != "" {
						record(slug, fmt.Sprintf("Title (from Slack profile): %s.", info.title))
					}
					if info.tz != "" {
						record(slug, fmt.Sprintf("Timezone: %s.", info.tz))
					}
				} else {
					record(slug, fmt.Sprintf("Slack bot %q (user id %s) — present in the workspace but not registered as an office agent.", name, userID))
				}
			} else {
				slug = t.foreignAgentSlug(userID)
			}
			record(slug, fmt.Sprintf("Member of the Slack channel bridged to office channel %q.", channelSlug))
		}
	}
}

// recordOfficeMemberFacts records identity facts for every office roster
// member: built-in/office agents and bridged foreign agents alike.
func (t *SlackTransport) recordOfficeMemberFacts(record func(slug, text string)) {
	for _, m := range t.Broker.OfficeMembers() {
		slug := strings.TrimSpace(m.Slug)
		if slug == "" {
			continue
		}
		role := strings.TrimSpace(m.Role)
		if role == "" {
			role = "agent"
		}
		switch {
		case m.Provider.Slack != nil && m.Provider.Slack.BotTokenEnv != "":
			// Spawned agent (broker_slack_spawn.go): a real office agent that
			// carries its OWN Slack identity and posts as itself.
			record(slug, fmt.Sprintf("AI agent %q — a WUPHF office agent with its own Slack identity (Slack user id %s); it posts in the bridged channel as itself. Role: %s.", m.Name, m.Provider.Slack.UserID, role))
		case m.Provider.Slack != nil:
			record(slug, fmt.Sprintf("AI agent %q — a foreign Slack agent bridged into the office (Slack user id %s). It runs its own runtime outside WUPHF and is reached by @-mentioning it in the bridged channel.", m.Name, m.Provider.Slack.UserID))
		default:
			record(slug, fmt.Sprintf("AI agent %q — an office agent with the role: %s.", m.Name, role))
			if kind := strings.TrimSpace(m.Provider.Kind); kind != "" {
				record(slug, fmt.Sprintf("Runs on the %s runtime.", kind))
			}
		}
	}
}

// slackEntitySlug derives a stable, valid entity slug for a Slack user: the
// kebab-cased display name, falling back to the lowercased user id when the
// name is empty or normalizes away entirely.
func slackEntitySlug(name, userID string) string {
	if s := slugify(name); s != "" {
		return s
	}
	return strings.ToLower(strings.TrimSpace(userID))
}
