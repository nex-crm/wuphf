package team

import (
	"context"
	"net/http"
	"strings"
	"time"
)

type requestActorKind string

const (
	requestActorKindBroker requestActorKind = "broker"
	requestActorKindHuman  requestActorKind = "human"
)

type requestActor struct {
	Kind        requestActorKind
	Slug        string
	DisplayName string
	SessionID   string
}

type requestActorContextKey struct{}

func (b *Broker) requestActorFromRequest(r *http.Request) (requestActor, bool) {
	if b.requestHasBrokerAuth(r) {
		return requestActor{Kind: requestActorKindBroker}, true
	}
	if session, ok := b.humanSessionFromRequest(r); ok {
		return requestActor{
			Kind:        requestActorKindHuman,
			Slug:        strings.TrimSpace(session.HumanSlug),
			DisplayName: strings.TrimSpace(session.DisplayName),
			SessionID:   strings.TrimSpace(session.ID),
		}, true
	}
	return requestActor{}, false
}

func requestWithActor(r *http.Request, actor requestActor) *http.Request {
	return r.WithContext(context.WithValue(r.Context(), requestActorContextKey{}, actor))
}

func requestActorFromContext(ctx context.Context) (requestActor, bool) {
	actor, ok := ctx.Value(requestActorContextKey{}).(requestActor)
	return actor, ok
}

func humanMessageSender(slug string) string {
	slug = normalizeHumanSessionSlug(slug)
	if slug == "" {
		slug = "co-founder"
	}
	return "human:" + slug
}

func isHumanMessageSender(sender string) bool {
	sender = normalizeActorSlug(sender)
	return sender == "" || sender == "you" || sender == "human" || strings.HasPrefix(sender, "human:")
}

func humanIdentityFromActor(actor requestActor) HumanIdentity {
	slug := normalizeHumanSessionSlug(actor.Slug)
	if slug == "" {
		slug = "co-founder"
	}
	name := strings.TrimSpace(actor.DisplayName)
	if name == "" {
		name = slug
	}
	return HumanIdentity{
		Name:      name,
		Email:     slug + "@wuphf.local",
		Slug:      slug,
		CreatedAt: time.Now().UTC(),
	}
}
