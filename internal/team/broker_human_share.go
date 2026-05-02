package team

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
)

const (
	humanInviteTTL      = 24 * time.Hour
	humanSessionCookie  = "wuphf_human_session"
	humanShareEventFrom = "system"
)

type humanInviteResponse struct {
	ID         string `json:"id"`
	CreatedAt  string `json:"created_at"`
	ExpiresAt  string `json:"expires_at"`
	AcceptedAt string `json:"accepted_at,omitempty"`
	AcceptedBy string `json:"accepted_by,omitempty"`
	RevokedAt  string `json:"revoked_at,omitempty"`
}

type humanSessionResponse struct {
	ID          string `json:"id"`
	InviteID    string `json:"invite_id"`
	HumanSlug   string `json:"human_slug"`
	DisplayName string `json:"display_name"`
	Device      string `json:"device,omitempty"`
	RemoteAddr  string `json:"remote_addr,omitempty"`
	CreatedAt   string `json:"created_at"`
	ExpiresAt   string `json:"expires_at"`
	RevokedAt   string `json:"revoked_at,omitempty"`
	LastSeenAt  string `json:"last_seen_at,omitempty"`
}

type shareError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Action  string `json:"action"`
	DocURL  string `json:"doc_url,omitempty"`
}

func (b *Broker) handleHumanInvites(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		b.mu.Lock()
		invites := make([]humanInviteResponse, 0, len(b.humanInvites))
		for _, invite := range b.humanInvites {
			invites = append(invites, humanInviteToResponse(invite))
		}
		b.mu.Unlock()
		writeJSON(w, http.StatusOK, map[string]any{"invites": invites})
	case http.MethodPost:
		token, invite, err := b.createHumanInvite()
		if err != nil {
			writeShareError(w, http.StatusInternalServerError, "invite_create_failed", "Could not create invite.", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"invite": humanInviteToResponse(invite),
			"token":  token,
		})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (b *Broker) handleHumanInviteAccept(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		Token       string `json:"token"`
		DisplayName string `json:"display_name"`
		Device      string `json:"device"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeShareError(w, http.StatusBadRequest, "invalid_json", "Invalid invite request.", "Reload the invite link and try again.")
		return
	}
	sessionToken, session, err := b.acceptHumanInvite(body.Token, body.DisplayName, body.Device, r.RemoteAddr)
	if err != nil {
		code := "invite_invalid"
		status := http.StatusBadRequest
		if errors.Is(err, errHumanInviteExpiredOrUsed) {
			code = "invite_expired_or_used"
			status = http.StatusGone
		}
		writeShareError(w, status, code, "This invite is no longer valid.", "Ask the host founder for a new invite.")
		return
	}
	http.SetCookie(w, humanSessionCookieForToken(sessionToken, sessionExpiresAt(session)))
	writeJSON(w, http.StatusOK, map[string]any{"session": humanSessionToResponse(session)})
}

func (b *Broker) handleHumanSessions(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		b.mu.Lock()
		sessions := make([]humanSessionResponse, 0, len(b.humanSessions))
		for _, session := range b.humanSessions {
			sessions = append(sessions, humanSessionToResponse(session))
		}
		b.mu.Unlock()
		writeJSON(w, http.StatusOK, map[string]any{"sessions": sessions})
	case http.MethodDelete:
		var body struct {
			ID string `json:"id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeShareError(w, http.StatusBadRequest, "invalid_json", "Invalid revoke request.", "Retry from Settings.")
			return
		}
		if err := b.revokeHumanSession(body.ID); err != nil {
			writeShareError(w, http.StatusNotFound, "session_not_found", "Session not found.", "Refresh the Sharing settings.")
			return
		}
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (b *Broker) handleHumanMe(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	session, ok := b.humanSessionFromRequest(r)
	if ok {
		writeJSON(w, http.StatusOK, map[string]any{"human": humanSessionToResponse(session)})
		return
	}
	if b.requestHasBrokerAuth(r) {
		writeJSON(w, http.StatusOK, map[string]any{
			"human": map[string]string{
				"slug":         "human",
				"display_name": "Host founder",
				"role":         "host",
			},
		})
		return
	}
	writeShareError(w, http.StatusUnauthorized, "session_required", "Your session expired.", "Ask the host founder for a new invite.")
}

var errHumanInviteExpiredOrUsed = errors.New("invite expired or used")

func (b *Broker) createHumanInvite() (string, humanInvite, error) {
	token, err := randomToken("wphf")
	if err != nil {
		return "", humanInvite{}, err
	}
	now := time.Now().UTC()
	invite := humanInvite{
		ID:        "invite-" + randomID(8),
		TokenHash: hashShareToken(token),
		CreatedAt: now.Format(time.RFC3339),
		ExpiresAt: now.Add(humanInviteTTL).Format(time.RFC3339),
	}
	b.mu.Lock()
	b.humanInvites = append(b.humanInvites, invite)
	err = b.saveLocked()
	b.mu.Unlock()
	if err != nil {
		return "", humanInvite{}, err
	}
	return token, invite, nil
}

func (b *Broker) acceptHumanInvite(token, displayName, device, remoteAddr string) (string, humanSession, error) {
	token = strings.TrimSpace(token)
	if token == "" {
		return "", humanSession{}, errHumanInviteExpiredOrUsed
	}
	now := time.Now().UTC()
	tokenHash := hashShareToken(token)
	sessionToken, err := randomToken("wphfs")
	if err != nil {
		return "", humanSession{}, err
	}
	displayName = strings.TrimSpace(displayName)
	if displayName == "" {
		displayName = "Co-founder"
	}
	slug := normalizeHumanSessionSlug(displayName)
	if slug == "" {
		slug = "co-founder"
	}

	b.mu.Lock()
	defer b.mu.Unlock()
	for i := range b.humanInvites {
		invite := &b.humanInvites[i]
		if invite.TokenHash != tokenHash {
			continue
		}
		if invite.RevokedAt != "" || invite.AcceptedAt != "" || !now.Before(parseBrokerTimestamp(invite.ExpiresAt)) {
			return "", humanSession{}, errHumanInviteExpiredOrUsed
		}
		invite.AcceptedAt = now.Format(time.RFC3339)
		invite.AcceptedBy = slug
		session := humanSession{
			ID:          "session-" + randomID(8),
			TokenHash:   hashShareToken(sessionToken),
			InviteID:    invite.ID,
			HumanSlug:   slug,
			DisplayName: displayName,
			Device:      strings.TrimSpace(device),
			RemoteAddr:  strings.TrimSpace(remoteAddr),
			CreatedAt:   now.Format(time.RFC3339),
			ExpiresAt:   now.Add(7 * 24 * time.Hour).Format(time.RFC3339),
			LastSeenAt:  now.Format(time.RFC3339),
		}
		b.humanSessions = append(b.humanSessions, session)
		b.counter++
		b.appendMessageLocked(channelMessage{
			ID:        fmt.Sprintf("msg-%d", b.counter),
			From:      humanShareEventFrom,
			Channel:   "general",
			Kind:      "system",
			Content:   fmt.Sprintf("%s joined the office.", displayName),
			Timestamp: now.Format(time.RFC3339),
		})
		if err := b.saveLocked(); err != nil {
			return "", humanSession{}, err
		}
		return sessionToken, session, nil
	}
	return "", humanSession{}, errHumanInviteExpiredOrUsed
}

func (b *Broker) revokeHumanSession(id string) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return errors.New("session id required")
	}
	now := time.Now().UTC()
	b.mu.Lock()
	defer b.mu.Unlock()
	for i := range b.humanSessions {
		if b.humanSessions[i].ID != id {
			continue
		}
		if b.humanSessions[i].RevokedAt == "" {
			b.humanSessions[i].RevokedAt = now.Format(time.RFC3339)
		}
		return b.saveLocked()
	}
	return errors.New("session not found")
}

func (b *Broker) humanSessionFromRequest(r *http.Request) (humanSession, bool) {
	cookie, err := r.Cookie(humanSessionCookie)
	if err != nil {
		return humanSession{}, false
	}
	tokenHash := hashShareToken(cookie.Value)
	now := time.Now().UTC()
	b.mu.Lock()
	defer b.mu.Unlock()
	for i := range b.humanSessions {
		session := &b.humanSessions[i]
		if session.TokenHash != tokenHash || session.RevokedAt != "" {
			continue
		}
		expiresAt := sessionExpiresAt(*session)
		if !expiresAt.IsZero() && !now.Before(expiresAt) {
			continue
		}
		session.LastSeenAt = now.Format(time.RFC3339)
		return *session, true
	}
	return humanSession{}, false
}

func humanSessionCookieForToken(token string, expires time.Time) *http.Cookie {
	return &http.Cookie{
		Name:     humanSessionCookie,
		Value:    token,
		Path:     "/",
		Expires:  expires,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	}
}

func sessionExpiresAt(s humanSession) time.Time {
	if expires := parseBrokerTimestamp(s.ExpiresAt); !expires.IsZero() {
		return expires
	}
	created := parseBrokerTimestamp(s.CreatedAt)
	if created.IsZero() {
		created = time.Now().UTC()
	}
	return created.Add(7 * 24 * time.Hour)
}

func humanInviteToResponse(invite humanInvite) humanInviteResponse {
	return humanInviteResponse{
		ID:         invite.ID,
		CreatedAt:  invite.CreatedAt,
		ExpiresAt:  invite.ExpiresAt,
		AcceptedAt: invite.AcceptedAt,
		AcceptedBy: invite.AcceptedBy,
		RevokedAt:  invite.RevokedAt,
	}
}

func humanSessionToResponse(session humanSession) humanSessionResponse {
	return humanSessionResponse{
		ID:          session.ID,
		InviteID:    session.InviteID,
		HumanSlug:   session.HumanSlug,
		DisplayName: session.DisplayName,
		Device:      session.Device,
		RemoteAddr:  session.RemoteAddr,
		CreatedAt:   session.CreatedAt,
		ExpiresAt:   sessionExpiresAt(session).Format(time.RFC3339),
		RevokedAt:   session.RevokedAt,
		LastSeenAt:  session.LastSeenAt,
	}
}

func writeShareError(w http.ResponseWriter, status int, code, message, action string) {
	writeJSON(w, status, map[string]shareError{"error": {
		Code:    code,
		Message: message,
		Action:  action,
		DocURL:  "https://github.com/nex-crm/wuphf#share-with-a-co-founder",
	}})
}

func randomToken(prefix string) (string, error) {
	buf := make([]byte, 24)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return prefix + "_" + hex.EncodeToString(buf), nil
}

func randomID(n int) string {
	if n <= 0 {
		n = 8
	}
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(buf)
}

func hashShareToken(token string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(token)))
	return hex.EncodeToString(sum[:])
}

func normalizeHumanSessionSlug(name string) string {
	name = strings.ToLower(strings.TrimSpace(name))
	var b strings.Builder
	lastDash := false
	for _, r := range name {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			lastDash = false
			continue
		}
		if !lastDash {
			b.WriteByte('-')
			lastDash = true
		}
	}
	return strings.Trim(b.String(), "-")
}
