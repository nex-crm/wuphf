package team

// slack_spawned_agents.go is the OUTBOUND half of spawned Slack agents
// (broker_slack_spawn.go): when an office message authored by a spawned agent
// goes out to a bridged Slack channel, it is posted with the AGENT'S OWN bot
// token — so it appears in Slack as that agent, with its own name and avatar —
// instead of the main wuphf bot token.
//
// transport.Outbound is a shared kernel type with no sender field, so the
// slack transport carries the spawned sender from FormatOutbound to Send in
// the Outbound.Participant field, which the broker's channel dispatcher
// leaves unused for channel-bound adapters. The key is prefixed so a real
// Slack participant key (U…) can never be mistaken for a spawned-sender
// marker.

import (
	"log"
	"os"
	"strings"

	"github.com/slack-go/slack"

	"github.com/nex-crm/wuphf/internal/team/transport"
)

// spawnedParticipantKeyPrefix marks an Outbound.Participant.Key that carries
// a spawned office agent slug from FormatOutbound to Send.
const spawnedParticipantKeyPrefix = "spawned:"

// spawnedSenderParticipant returns the Participant FormatOutbound attaches
// when the office-internal sender is a spawned Slack agent, and the zero
// Participant for every other sender. Resolution keys on the broker roster
// (never on message text), so a hostile message body cannot select a token.
func (t *SlackTransport) spawnedSenderParticipant(from string) transport.Participant {
	if t.Broker == nil {
		return transport.Participant{}
	}
	slug := normalizeActorSlug(from)
	if slug == "" || t.Broker.SpawnedSlackAgentTokenEnv(slug) == "" {
		return transport.Participant{}
	}
	return transport.Participant{
		AdapterName: "slack",
		Key:         spawnedParticipantKeyPrefix + slug,
		DisplayName: strings.TrimSpace(from),
	}
}

// spawnedSlugFromParticipant extracts the spawned-sender slug carried by
// spawnedSenderParticipant, or "" for any other participant shape.
func spawnedSlugFromParticipant(p transport.Participant) string {
	if p.AdapterName != "slack" {
		return ""
	}
	slug, ok := strings.CutPrefix(p.Key, spawnedParticipantKeyPrefix)
	if !ok {
		return ""
	}
	return slug
}

// postClientFor resolves which Web API client Send posts with: the spawned
// agent's own client when the outbound carries a spawned-sender participant
// and its token env var is set, the main bot client otherwise.
func (t *SlackTransport) postClientFor(p transport.Participant) slackAPI {
	slug := spawnedSlugFromParticipant(p)
	if slug == "" {
		return t.api
	}
	if c := t.spawnedAgentClient(slug); c != nil {
		return c
	}
	return t.api
}

// spawnedAgentClient returns (and caches) a Web API client authed as the
// spawned agent's own bot, or nil when slug is not a spawned agent or its
// token env var is unset (Send then degrades to the main bot token, so the
// message still lands — just attributed to the wuphf bot). The cache holds
// the miss too: env vars are process-lifetime, so a re-probe per message
// would only repeat the same lookup and warning.
func (t *SlackTransport) spawnedAgentClient(slug string) slackAPI {
	if v, ok := t.spawnedClients.Load(slug); ok {
		c, _ := v.(slackAPI)
		return c
	}
	var client slackAPI
	if t.Broker != nil {
		if env := t.Broker.SpawnedSlackAgentTokenEnv(slug); env != "" {
			if token := strings.TrimSpace(os.Getenv(env)); token != "" {
				client = &slackClient{api: slack.New(token)}
			} else {
				log.Printf("[slack] spawned agent %q: env %s is unset — posting with the main bot token", slug, env)
			}
		}
	}
	t.spawnedClients.Store(slug, client)
	return client
}
