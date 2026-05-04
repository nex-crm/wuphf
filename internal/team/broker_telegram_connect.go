package team

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/nex-crm/wuphf/internal/company"
	"github.com/nex-crm/wuphf/internal/config"
)

// Sentinel errors returned by createTelegramChannel so the HTTP handler can
// discriminate cases without fragile substring matching against free-text
// error messages. Any future refactor that touches the wording can't silently
// reroute "already exists" into "already bridges" or vice versa.
var (
	errChannelAlreadyBridges = errors.New("channel already bridges a different telegram chat")
	errChannelAlreadyExists  = errors.New("channel already exists")
)

// Telegram connect endpoints used by the web wizard. The TUI talks to the
// Telegram Bot API directly; the web does it through these handlers so the
// flow can be completed end-to-end inside the office UI.
//
//	POST /telegram/verify   { token }                       → { ok, bot_name }
//	POST /telegram/discover { token }                       → { groups: [...] }
//	POST /telegram/connect  { token, chat_id, title, type } → { channel_slug, group_title }

type telegramConnectRequest struct {
	Token  string `json:"token,omitempty"`
	ChatID int64  `json:"chat_id,omitempty"`
	Title  string `json:"title,omitempty"`
	Type   string `json:"type,omitempty"`
}

func (b *Broker) handleTelegramVerify(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var body telegramConnectRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	rawBodyToken := strings.TrimSpace(body.Token)
	token := resolveTelegramTokenFromBody(body.Token)
	if token == "" {
		http.Error(w, "telegram bot token required", http.StatusBadRequest)
		return
	}
	name, err := VerifyBot(token)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	// Persist on first successful verify so a verify-then-abandon flow leaves
	// the user with a usable token in config. Two correctness notes:
	//
	//   1. Only save when nothing is already *persisted*. We read
	//      cfg.TelegramBotToken directly rather than going through
	//      ResolveTelegramBotToken — the latter short-circuits on
	//      WUPHF_TELEGRAM_BOT_TOKEN, which would mean a developer who
	//      exported the env to test bot A would never persist bot B's token
	//      through the wizard. Persistence is config-state; env-as-override
	//      is a runtime concern. SaveTelegramBotToken still stores a single
	//      global field, so we don't overwrite — multi-bot support needs a
	//      per-channel token map and is out of scope for this PR.
	//
	//   2. Don't os.Setenv. The Telegram transport caches the token at
	//      construction time (telegram.go: BotToken is captured into the
	//      struct), so a mutated env var never reaches the live bridge —
	//      and process-global env mutation races with any concurrent reader.
	//      The save above is what actually matters for the next restart.
	//
	if rawBodyToken != "" {
		if cfg, _ := config.Load(); strings.TrimSpace(cfg.TelegramBotToken) == "" {
			config.SaveTelegramBotToken(rawBodyToken)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "bot_name": name})
}

func (b *Broker) handleTelegramDiscover(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var body telegramConnectRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	token := resolveTelegramTokenFromBody(body.Token)
	if token == "" {
		http.Error(w, "telegram bot token required", http.StatusBadRequest)
		return
	}
	groups, err := DiscoverGroups(token)
	if err != nil {
		// Surfacing the error (rather than treating it as "no groups") keeps
		// the wizard from pushing the user into the manual/DM fallback when
		// the real failure is transient (Telegram 5xx, DNS, rate limit). The
		// frontend renders the body inside the picker-step error banner.
		http.Error(w, fmt.Sprintf("could not discover groups: %v", err), http.StatusBadGateway)
		return
	}

	// Merge in groups the live transport has seen so the picker shows everything.
	seen := make(map[int64]bool, len(groups))
	for _, g := range groups {
		seen[g.ChatID] = true
	}
	for chatID, title := range b.SeenTelegramGroups() {
		if seen[chatID] {
			continue
		}
		groups = append(groups, TelegramGroup{ChatID: chatID, Title: title, Type: "group"})
	}

	out := make([]map[string]any, 0, len(groups))
	for _, g := range groups {
		out = append(out, map[string]any{
			"chat_id": g.ChatID,
			"title":   g.Title,
			"type":    g.Type,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"groups": out})
}

func (b *Broker) handleTelegramConnect(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var body telegramConnectRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	token := resolveTelegramTokenFromBody(body.Token)
	if token == "" {
		http.Error(w, "telegram bot token required", http.StatusBadRequest)
		return
	}

	title := strings.TrimSpace(body.Title)
	chatID := body.ChatID

	// chat_id == 0 is reserved for the "DM" pseudo-group (the TUI uses it the
	// same way). For anything else, verify the chat exists before we create
	// a channel pointed at it.
	if chatID != 0 {
		verified, err := VerifyChat(token, chatID)
		if err != nil {
			http.Error(w, fmt.Sprintf("could not verify chat: %v", err), http.StatusBadRequest)
			return
		}
		if title == "" {
			title = verified
		}
	}
	if title == "" {
		if chatID == 0 {
			title = "Telegram DM"
		} else {
			title = fmt.Sprintf("Telegram %d", chatID)
		}
	}

	slug := SlugifyTelegramTitle(title)
	chType := strings.TrimSpace(body.Type)
	if chType == "" {
		if chatID == 0 {
			chType = "private"
		} else {
			chType = "group"
		}
	}

	ch, err := b.createTelegramChannel(slug, title, chatID, chType)
	if err != nil {
		switch {
		// errChannelAlreadyExists with a matching remote id is idempotent —
		// the user just asked for the same chat twice. ch holds the existing
		// channel, so fall through to the 200 response.
		case errors.Is(err, errChannelAlreadyExists):
			// fall through
		// errChannelAlreadyBridges means the slug already maps to a different
		// chat (or to a non-telegram channel). 409 Conflict so the wizard
		// keeps the picker step open with the error visible — see
		// telegram-connect.spec.ts checkpoint [10].
		case errors.Is(err, errChannelAlreadyBridges):
			http.Error(w, err.Error(), http.StatusConflict)
			return
		default:
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}
	if ch == nil {
		// Defence-in-depth: the only path that returns a non-nil error AND a
		// nil channel today is the "already bridges" case handled above. If a
		// future refactor introduces another nil-channel error, this guard
		// stops a downstream nil-deref on ch.Slug.
		http.Error(w, "channel create returned nil channel", http.StatusInternalServerError)
		return
	}

	// Best-effort welcome message + manifest sync. Failures here are logged
	// but don't fail the request — the channel exists and the bridge will
	// catch up on the next poll.
	if chatID != 0 {
		if sendErr := SendTelegramMessage(token, chatID,
			"Connected to WUPHF Office. Messages here will be visible to the team."); sendErr != nil {
			log.Printf("[telegram] welcome send failed for chat %d: %v", chatID, sendErr)
		}
	}
	syncManifestForTelegramChannel(slug, title, chatID)

	writeJSON(w, http.StatusOK, map[string]any{
		"channel_slug": ch.Slug,
		"group_title":  title,
	})
}

// createTelegramChannel routes the Telegram-bridged channel through the
// canonical createChannelLocked helper so member validation, the
// reserved-slug guard, and the creator-self-add rules apply the same way they
// do for /channels POST. Members come from company.yaml — same set the TUI
// seeds in cmd/wuphf/channel_integration.go's connectTelegramGroup.
func (b *Broker) createTelegramChannel(slug, title string, chatID int64, chType string) (*teamChannel, error) {
	// Read the manifest under the same lock UpdateManifest uses, so the
	// member list we derive here is consistent with whatever
	// syncManifestForTelegramChannel writes a few lines later. A bare
	// LoadManifest would race with concurrent UpdateManifest writers and
	// could let the broker's in-memory channel diverge from the persisted
	// manifest's member list. Snapshot, then proceed under b.mu.
	manifest, err := company.SnapshotManifest()
	if err != nil {
		return nil, fmt.Errorf("load company manifest: %w", err)
	}
	// Mirror the TUI exactly: lead first, then every other manifest member.
	// "ceo" is prepended by createChannelLocked itself, so we don't add it here.
	members := []string{manifest.Lead}
	for _, m := range manifest.Members {
		if m.Slug != "" && m.Slug != manifest.Lead {
			members = append(members, m.Slug)
		}
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	// The manifest is desired state; the broker's in-memory member set is
	// current state. Manifest slugs that haven't been adopted yet (e.g.
	// "planner", "executor", "reviewer") would cause createChannelLocked to
	// return "unknown members". Skip them with a log entry so the Telegram
	// connect succeeds — they join the channel when they are adopted.
	adopted := make([]string, 0, len(members))
	for _, m := range members {
		if b.findMemberLocked(m) != nil {
			adopted = append(adopted, m)
		}
	}
	if len(adopted) < len(members) {
		var skipped []string
		adoptedSet := make(map[string]bool, len(adopted))
		for _, m := range adopted {
			adoptedSet[m] = true
		}
		for _, m := range members {
			if !adoptedSet[m] {
				skipped = append(skipped, m)
			}
		}
		log.Printf("[telegram] connect: skipping %d unadopted member(s): %s",
			len(skipped), strings.Join(skipped, ", "))
	}
	members = adopted

	if existing := b.findChannelLocked(slug); existing != nil {
		// SlugifyTelegramTitle is title-only, so two distinct Telegram chats
		// with the same display name collide on slug. If we returned the
		// existing channel here, the caller would think the new chat was
		// connected even though the channel's surface still points at the
		// previous chat — messages would route to the wrong conversation.
		newRemoteID := fmt.Sprintf("%d", chatID)
		// Surface == nil or Provider != "telegram" means a hand-created or
		// other-provider channel happens to share the slug. Treat that as a
		// conflict — silently reusing it would bridge a Telegram chat into a
		// non-Telegram channel.
		if existing.Surface == nil || existing.Surface.Provider != "telegram" {
			return nil, fmt.Errorf("%w: %q already exists as a non-telegram channel",
				errChannelAlreadyBridges, slug)
		}
		if existing.Surface.RemoteID != "" && existing.Surface.RemoteID != newRemoteID {
			return nil, fmt.Errorf(
				"%w: %q already bridges Telegram chat %s; rename the new chat or pick a different slug",
				errChannelAlreadyBridges, slug, existing.Surface.RemoteID,
			)
		}
		return existing, errChannelAlreadyExists
	}

	ch, cerr := b.createChannelLocked(channelCreateInput{
		Slug:        slug,
		Name:        title,
		Description: fmt.Sprintf("Telegram bridge for %s.", title),
		Members:     members,
		CreatedBy:   "you",
		Surface: &channelSurface{
			Provider:    "telegram",
			RemoteID:    fmt.Sprintf("%d", chatID),
			RemoteTitle: title,
			Mode:        chType,
			BotTokenEnv: "WUPHF_TELEGRAM_BOT_TOKEN",
		},
	})
	if cerr != nil {
		return nil, cerr
	}
	return ch, nil
}

// syncManifestForTelegramChannel adds the new channel to company.yaml so that
// a future broker restart picks it up before the broker-state.json is reread.
// This mirrors what the TUI does in connectTelegramGroup.
//
// Goes through company.UpdateManifest so the load → append → save sequence is
// atomic against concurrent web/web AND web/TUI updates — without that lock,
// two parallel connects could both LoadManifest, each append, and the slower
// SaveManifest would silently drop the faster one's row.
func syncManifestForTelegramChannel(slug, title string, chatID int64) {
	err := company.UpdateManifest(func(manifest *company.Manifest) error {
		for _, ch := range manifest.Channels {
			if ch.Slug == slug {
				return nil
			}
		}
		members := []string{manifest.Lead}
		for _, m := range manifest.Members {
			if m.Slug != "" && m.Slug != manifest.Lead {
				members = append(members, m.Slug)
			}
		}
		manifest.Channels = append(manifest.Channels, company.ChannelSpec{
			Slug:        slug,
			Name:        title,
			Description: fmt.Sprintf("Telegram bridge for %s.", title),
			Members:     members,
			Surface: &company.ChannelSurfaceSpec{
				Provider:    "telegram",
				RemoteID:    fmt.Sprintf("%d", chatID),
				RemoteTitle: title,
				BotTokenEnv: "WUPHF_TELEGRAM_BOT_TOKEN",
			},
		})
		return nil
	})
	if err != nil {
		// Don't fail the connect — the in-memory channel is already created
		// and persisted via b.saveLocked(). The manifest sync is just so that
		// a future restart re-reads from company.yaml. Log it loudly so a
		// disk-full / permission bug surfaces in the broker log instead of
		// silently rotting until someone restarts.
		log.Printf("[telegram] manifest sync failed for %s: %v", slug, err)
	}
}

func resolveTelegramTokenFromBody(token string) string {
	if t := strings.TrimSpace(token); t != "" {
		return t
	}
	if env := strings.TrimSpace(os.Getenv("WUPHF_TELEGRAM_BOT_TOKEN")); env != "" {
		return env
	}
	return strings.TrimSpace(config.ResolveTelegramBotToken())
}
