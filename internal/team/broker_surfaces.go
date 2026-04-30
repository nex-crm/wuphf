package team

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"
)

const maxSurfaceRequestBodyBytes = 128 * 1024

type SurfaceEvent struct {
	Type      string `json:"type"`
	SurfaceID string `json:"surface_id"`
	WidgetID  string `json:"widget_id,omitempty"`
	Channel   string `json:"channel"`
	Actor     string `json:"actor,omitempty"`
	Title     string `json:"title,omitempty"`
	CreatedAt string `json:"created_at"`
}

var (
	surfaceSubscribersMu       sync.Mutex
	surfaceSubscribersByBroker = map[*Broker]map[int]chan SurfaceEvent{}
)

func (b *Broker) SubscribeSurfaceEvents(buffer int) (<-chan SurfaceEvent, func()) {
	if buffer <= 0 {
		buffer = 64
	}
	surfaceSubscribersMu.Lock()
	defer surfaceSubscribersMu.Unlock()
	subs := surfaceSubscribersByBroker[b]
	if subs == nil {
		subs = map[int]chan SurfaceEvent{}
		surfaceSubscribersByBroker[b] = subs
	}
	b.mu.Lock()
	id := b.nextSubscriberID
	b.nextSubscriberID++
	b.mu.Unlock()
	ch := make(chan SurfaceEvent, buffer)
	subs[id] = ch
	return ch, func() {
		surfaceSubscribersMu.Lock()
		defer surfaceSubscribersMu.Unlock()
		if subs := surfaceSubscribersByBroker[b]; subs != nil {
			if existing, ok := subs[id]; ok {
				delete(subs, id)
				close(existing)
			}
		}
	}
}

func (b *Broker) PublishSurfaceEvent(evt SurfaceEvent) {
	surfaceSubscribersMu.Lock()
	defer surfaceSubscribersMu.Unlock()
	for key, ch := range surfaceSubscribersByBroker[b] {
		select {
		case ch <- evt:
		default:
			log.Printf("surfaces: dropping event for subscriber %d (buffer full)", key)
		}
	}
}

func (b *Broker) handleSurfaces(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		b.handleSurfaceList(w, r)
	case http.MethodPost:
		b.handleSurfaceCreate(w, r)
	default:
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
	}
}

func (b *Broker) handleSurfaceSubpath(w http.ResponseWriter, r *http.Request) {
	trimmed := strings.Trim(strings.TrimPrefix(r.URL.Path, "/surfaces/"), "/")
	if trimmed == "" {
		b.handleSurfaces(w, r)
		return
	}
	parts := strings.Split(trimmed, "/")
	surfaceID := parts[0]
	switch {
	case len(parts) == 1:
		b.handleSurfaceResource(w, r, surfaceID)
	case len(parts) == 2 && parts[1] == "widgets":
		b.handleSurfaceWidgets(w, r, surfaceID)
	case len(parts) == 3 && parts[1] == "widgets":
		b.handleSurfaceWidgetResource(w, r, surfaceID, parts[2])
	case len(parts) == 4 && parts[1] == "widgets" && parts[3] == "render-check":
		b.handleSurfaceWidgetRenderCheck(w, r, surfaceID, parts[2])
	case len(parts) == 2 && parts[1] == "history":
		b.handleSurfaceHistory(w, r, surfaceID)
	default:
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
	}
}

func (b *Broker) handleSurfaceList(w http.ResponseWriter, r *http.Request) {
	viewer := surfaceViewerFromRequest(r)
	rows, err := b.surfaceStore().ListSurfaces()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	out := make([]SurfaceManifest, 0, len(rows))
	b.mu.Lock()
	for _, row := range rows {
		if b.canAccessChannelLocked(viewer, row.Channel) {
			out = append(out, row)
		}
	}
	b.mu.Unlock()
	writeJSON(w, http.StatusOK, map[string]any{"surfaces": out})
}

func (b *Broker) handleSurfaceCreate(w http.ResponseWriter, r *http.Request) {
	var body struct {
		SurfaceManifest `json:",inline"`
		MySlug          string `json:"my_slug,omitempty"`
		Actor           string `json:"actor,omitempty"`
		EventID         string `json:"event_id,omitempty"`
	}
	if err := decodeSurfaceJSON(w, r, &body); err != nil {
		return
	}
	actor := firstNonEmpty(body.Actor, body.CreatedBy, body.MySlug, r.Header.Get(agentRateLimitHeader), "human")
	body.CreatedBy = actor
	body.Channel = normalizeChannelSlug(body.Channel)
	if body.Channel == "" {
		body.Channel = "general"
	}
	if !b.canAccessSurfaceChannel(actor, body.Channel) {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "channel access denied"})
		return
	}
	if !b.channelExists(body.Channel) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "channel not found"})
		return
	}
	surface, err := b.surfaceStore().CreateSurface(body.SurfaceManifest)
	if err != nil {
		writeSurfaceError(w, err)
		return
	}
	b.afterSurfaceMutation("surface:created", "surface_created", surface, "", actor, "Created Studio surface", body.EventID)
	writeJSON(w, http.StatusOK, map[string]any{"surface": surface})
}

func (b *Broker) handleSurfaceResource(w http.ResponseWriter, r *http.Request, surfaceID string) {
	switch r.Method {
	case http.MethodGet:
		detail, ok := b.readSurfaceForRequest(w, r, surfaceID)
		if !ok {
			return
		}
		writeJSON(w, http.StatusOK, detail)
	case http.MethodPatch:
		var body struct {
			SurfaceManifest `json:",inline"`
			MySlug          string `json:"my_slug,omitempty"`
			Actor           string `json:"actor,omitempty"`
			EventID         string `json:"event_id,omitempty"`
		}
		if err := decodeSurfaceJSON(w, r, &body); err != nil {
			return
		}
		current, ok := b.readSurfaceManifestForRequest(w, r, surfaceID)
		if !ok {
			return
		}
		actor := firstNonEmpty(body.Actor, body.MySlug, r.Header.Get(agentRateLimitHeader), "human")
		body.ID = surfaceID
		updated, err := b.surfaceStore().UpdateSurface(body.SurfaceManifest, actor)
		if err != nil {
			writeSurfaceError(w, err)
			return
		}
		b.afterSurfaceMutation("surface:updated", "surface_updated", updated, "", actor, "Updated Studio surface", body.EventID)
		_ = current
		writeJSON(w, http.StatusOK, map[string]any{"surface": updated})
	case http.MethodDelete:
		current, ok := b.readSurfaceManifestForRequest(w, r, surfaceID)
		if !ok {
			return
		}
		actor := firstNonEmpty(r.URL.Query().Get("my_slug"), r.URL.Query().Get("actor"), r.Header.Get(agentRateLimitHeader), "human")
		if err := b.surfaceStore().DeleteSurface(surfaceID, actor); err != nil {
			writeSurfaceError(w, err)
			return
		}
		b.afterSurfaceMutation("surface:deleted", "surface_deleted", current, "", actor, "Deleted Studio surface", "")
		writeJSON(w, http.StatusOK, map[string]any{"deleted": true})
	default:
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
	}
}

func (b *Broker) handleSurfaceWidgets(w http.ResponseWriter, r *http.Request, surfaceID string) {
	switch r.Method {
	case http.MethodGet:
		if _, ok := b.readSurfaceManifestForRequest(w, r, surfaceID); !ok {
			return
		}
		widgets, err := b.surfaceStore().ListWidgetRecords(surfaceID)
		if err != nil {
			writeSurfaceError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"widgets": widgets})
	case http.MethodPost:
		var body struct {
			Widget  SurfaceWidgetFile `json:"widget"`
			MySlug  string            `json:"my_slug,omitempty"`
			Actor   string            `json:"actor,omitempty"`
			EventID string            `json:"event_id,omitempty"`
		}
		if err := decodeSurfaceJSON(w, r, &body); err != nil {
			return
		}
		current, ok := b.readSurfaceManifestForRequest(w, r, surfaceID)
		if !ok {
			return
		}
		actor := firstNonEmpty(body.Actor, body.MySlug, body.Widget.UpdatedBy, body.Widget.CreatedBy, r.Header.Get(agentRateLimitHeader), "human")
		eventType := "surface:widget_updated"
		if strings.TrimSpace(body.Widget.ID) != "" {
			if _, err := b.surfaceStore().ReadWidget(surfaceID, strings.TrimSpace(body.Widget.ID)); errors.Is(err, errWidgetNotFound) {
				eventType = "surface:widget_created"
			}
		}
		record, err := b.surfaceStore().UpsertWidget(surfaceID, body.Widget, actor)
		if err != nil {
			writeSurfaceError(w, err)
			return
		}
		b.afterSurfaceMutation(eventType, "widget_upserted", current, record.Widget.ID, actor, "Updated Studio widget", body.EventID)
		writeJSON(w, http.StatusOK, record)
	default:
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
	}
}

func (b *Broker) handleSurfaceWidgetResource(w http.ResponseWriter, r *http.Request, surfaceID, widgetID string) {
	switch r.Method {
	case http.MethodGet:
		if _, ok := b.readSurfaceManifestForRequest(w, r, surfaceID); !ok {
			return
		}
		record, err := b.surfaceStore().ReadWidget(surfaceID, widgetID)
		if err != nil {
			writeSurfaceError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, record)
	case http.MethodPatch:
		var body WidgetPatchRequest
		if err := decodeSurfaceJSON(w, r, &body); err != nil {
			return
		}
		current, ok := b.readSurfaceManifestForRequest(w, r, surfaceID)
		if !ok {
			return
		}
		if body.Actor == "" {
			body.Actor = firstNonEmpty(r.Header.Get(agentRateLimitHeader), "human")
		}
		record, err := b.surfaceStore().PatchWidget(surfaceID, widgetID, body)
		if err != nil {
			writeSurfaceError(w, err)
			return
		}
		b.afterSurfaceMutation("surface:widget_updated", "widget_patched", current, record.Widget.ID, body.Actor, "Patched Studio widget", "")
		writeJSON(w, http.StatusOK, record)
	default:
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
	}
}

func (b *Broker) handleSurfaceWidgetRenderCheck(w http.ResponseWriter, r *http.Request, surfaceID, widgetID string) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	current, ok := b.readSurfaceManifestForRequest(w, r, surfaceID)
	if !ok {
		return
	}
	var body struct {
		Widget *SurfaceWidgetFile `json:"widget,omitempty"`
		MySlug string             `json:"my_slug,omitempty"`
		Actor  string             `json:"actor,omitempty"`
	}
	if r.Body != nil {
		if err := decodeSurfaceJSON(w, r, &body); err != nil {
			return
		}
	}
	if body.Widget != nil {
		body.Widget.ID = firstNonEmpty(body.Widget.ID, widgetID)
		if saved, err := b.surfaceStore().ReadWidget(surfaceID, widgetID); err == nil {
			body.Widget.Title = firstNonEmpty(body.Widget.Title, saved.Widget.Title)
			body.Widget.Kind = firstNonEmpty(body.Widget.Kind, saved.Widget.Kind)
			body.Widget.SchemaVersion = firstNonEmpty(body.Widget.SchemaVersion, saved.Widget.SchemaVersion)
		}
	}
	result, err := b.surfaceStore().RenderCheck(surfaceID, widgetID, body.Widget)
	if err != nil {
		writeSurfaceError(w, err)
		return
	}
	actor := firstNonEmpty(body.Actor, body.MySlug, r.Header.Get(agentRateLimitHeader), "human")
	b.PublishSurfaceEvent(SurfaceEvent{
		Type:      "surface:render_checked",
		SurfaceID: surfaceID,
		WidgetID:  widgetID,
		Channel:   current.Channel,
		Actor:     actor,
		Title:     current.Title,
		CreatedAt: time.Now().UTC().Format(time.RFC3339),
	})
	writeJSON(w, http.StatusOK, result)
}

func (b *Broker) handleSurfaceHistory(w http.ResponseWriter, r *http.Request, surfaceID string) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	if _, ok := b.readSurfaceManifestForRequest(w, r, surfaceID); !ok {
		return
	}
	history, err := b.surfaceStore().ListHistory(surfaceID)
	if err != nil {
		writeSurfaceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"history": history})
}

func (b *Broker) readSurfaceForRequest(w http.ResponseWriter, r *http.Request, surfaceID string) (SurfaceDetail, bool) {
	detail, err := b.surfaceStore().ReadSurface(surfaceID)
	if err != nil {
		writeSurfaceError(w, err)
		return SurfaceDetail{}, false
	}
	if !b.canAccessSurfaceChannel(surfaceViewerFromRequest(r), detail.Surface.Channel) {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "channel access denied"})
		return SurfaceDetail{}, false
	}
	return detail, true
}

func (b *Broker) readSurfaceManifestForRequest(w http.ResponseWriter, r *http.Request, surfaceID string) (SurfaceManifest, bool) {
	manifest, err := b.surfaceStore().ReadSurfaceManifest(surfaceID)
	if err != nil {
		writeSurfaceError(w, err)
		return SurfaceManifest{}, false
	}
	if !b.canAccessSurfaceChannel(surfaceViewerFromRequest(r), manifest.Channel) {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "channel access denied"})
		return SurfaceManifest{}, false
	}
	return manifest, true
}

func (b *Broker) canAccessSurfaceChannel(actor, channel string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.canAccessChannelLocked(actor, channel)
}

func (b *Broker) channelExists(channel string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.findChannelLocked(channel) != nil
}

func (b *Broker) afterSurfaceMutation(eventType, actionKind string, surface SurfaceManifest, widgetID, actor, title, eventID string) {
	var summary string
	if widgetID != "" {
		summary = fmt.Sprintf("%s %s in %s", title, widgetID, surface.Title)
	} else {
		summary = fmt.Sprintf("%s %s", title, surface.Title)
	}
	relatedID := surface.ID
	if widgetID != "" {
		relatedID = surface.ID + "/" + widgetID
	}
	if err := b.RecordAction(actionKind, "studio", surface.Channel, actor, truncateSummary(summary, 140), relatedID, nil, ""); err != nil {
		log.Printf("surfaces: record action failed: %v", err)
	}
	if eventID == "" {
		eventID = fmt.Sprintf("studio:%s:%s:%s:%d", actionKind, surface.ID, widgetID, time.Now().UnixNano())
	}
	content := fmt.Sprintf("%s.\n\n[Open Studio](#/apps/studio)", summary)
	if _, _, err := b.PostAutomationMessage("studio", surface.Channel, "Studio", content, eventID, "studio", "Studio", nil, ""); err != nil {
		log.Printf("surfaces: post automation message failed: %v", err)
	}
	b.PublishSurfaceEvent(SurfaceEvent{
		Type:      eventType,
		SurfaceID: surface.ID,
		WidgetID:  widgetID,
		Channel:   surface.Channel,
		Actor:     actor,
		Title:     surface.Title,
		CreatedAt: time.Now().UTC().Format(time.RFC3339),
	})
}

func decodeSurfaceJSON(w http.ResponseWriter, r *http.Request, dest any) error {
	r.Body = http.MaxBytesReader(w, r.Body, maxSurfaceRequestBodyBytes)
	if err := json.NewDecoder(r.Body).Decode(dest); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid json"})
		return err
	}
	return nil
}

func writeSurfaceError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, errSurfaceNotFound), errors.Is(err, errWidgetNotFound):
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
	case strings.Contains(err.Error(), "access denied"):
		writeJSON(w, http.StatusForbidden, map[string]string{"error": err.Error()})
	case strings.Contains(err.Error(), "invalid"),
		strings.Contains(err.Error(), "requires"),
		strings.Contains(err.Error(), "exceeds"),
		strings.Contains(err.Error(), "not found"),
		strings.Contains(err.Error(), "ambiguous"),
		strings.Contains(err.Error(), "render check failed"),
		strings.Contains(err.Error(), "outside source"):
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
	default:
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}
}

func surfaceViewerFromRequest(r *http.Request) string {
	q := r.URL.Query()
	return firstNonEmpty(
		q.Get("viewer_slug"),
		q.Get("my_slug"),
		q.Get("actor"),
		r.Header.Get(agentRateLimitHeader),
		"human",
	)
}
