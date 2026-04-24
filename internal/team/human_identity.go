package team

// human_identity.go implements the per-human git-identity registry used by
// the wiki editor (v1.5 hardening of PR #192).
//
// Problem
// =======
//
// In v1.4 every human wiki edit was attributed to the synthetic identity
//
//	human <human@wuphf.local>
//
// That worked for a one-person team but collapsed for two. With multiple
// humans editing the wiki (founder + cofounder, say) `git log` could not
// tell them apart.
//
// Design
// ======
//
// First time the broker observes a write path, it probes the user's shell
// git config for `user.name` + `user.email`. That identity is cached on
// disk at
//
//	~/.wuphf/humans/{sha256(email)[:16]}.json
//
// and served back for every subsequent `POST /wiki/write-human` from the
// same machine. Two different humans on two different machines produce
// two different cache files; the registry grows on read.
//
// Slug derivation
// ---------------
//
// The slug is the email's local-part, normalised to lowercase kebab-case:
//
//	sarah.chen@acme.com  → sarah-chen
//	FOUNDER+work@ex.io   → founder-work
//
// The slug is used as the git commit author name's URL-safe handle and
// as the key the web UI uses to look up the registered display name for
// a byline. It is NOT used as the commit email — we keep the user's real
// email on commits so `git log` matches their local identity.
//
// Fallback
// --------
//
// When git config is missing (fresh shell, CI sandbox) the probe falls
// back to the v1.4 synthetic identity (`human`/`human@wuphf.local`). That
// preserves existing behaviour and lets the handler stay unconditional.
//
// Multi-human support
// -------------------
//
// The registry is merge-on-read. Every commit that lands with an email
// we haven't cached gets written to a new `{sha}.json`, so once two
// people have each edited once, both are in the registry.

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/nex-crm/wuphf/internal/config"
)

// HumanIdentity is the cached git identity for a single human.
//
// It is written to disk as the JSON contents of
// ~/.wuphf/humans/{sha256(email)[:16]}.json and is the source of truth
// the broker uses when stamping a `/wiki/write-human` commit.
type HumanIdentity struct {
	Name      string    `json:"name"`
	Email     string    `json:"email"`
	Slug      string    `json:"slug"`
	CreatedAt time.Time `json:"created_at"`
}

// FallbackHumanIdentity is returned when no git config is set and no
// cached local identity exists. It matches the v1.4 behaviour so audit
// history keeps its meaning for single-user installs.
var FallbackHumanIdentity = HumanIdentity{
	Name:  HumanAuthor,
	Email: HumanAuthor + "@wuphf.local",
	Slug:  HumanAuthor,
}

// HumanIdentityRegistry manages the on-disk cache of git identities.
// Safe for concurrent use.
type HumanIdentityRegistry struct {
	dir string

	mu         sync.Mutex
	localCache *HumanIdentity
}

// NewHumanIdentityRegistry constructs a registry rooted at
// {RuntimeHomeDir}/.wuphf/humans. The directory is created lazily on
// first write; construction never touches the filesystem.
func NewHumanIdentityRegistry() *HumanIdentityRegistry {
	return &HumanIdentityRegistry{dir: humansDir()}
}

// NewHumanIdentityRegistryAt is the test hook — accepts an explicit dir
// so each test run has its own cache.
func NewHumanIdentityRegistryAt(dir string) *HumanIdentityRegistry {
	return &HumanIdentityRegistry{dir: dir}
}

// humansDir mirrors WikiRootDir's layout so dev runs (WUPHF_RUNTIME_HOME
// override) stay isolated from a user's prod ~/.wuphf.
func humansDir() string {
	home := strings.TrimSpace(config.RuntimeHomeDir())
	if home == "" {
		return filepath.Join(".wuphf", "humans")
	}
	return filepath.Join(home, ".wuphf", "humans")
}

// Dir returns the on-disk directory the registry reads/writes. Useful
// for tests and observability.
func (r *HumanIdentityRegistry) Dir() string { return r.dir }

// Local returns the local-machine identity — the one derived from the
// current user's `git config --global`. Cached after the first call so
// repeated writes don't fork a subprocess per commit.
//
// On any error (git missing, config unset, persist failure) Local falls
// back to FallbackHumanIdentity so callers never need to nil-check.
func (r *HumanIdentityRegistry) Local() HumanIdentity {
	r.mu.Lock()
	cached := r.localCache
	r.mu.Unlock()
	if cached != nil {
		return *cached
	}
	id := probeLocalGitIdentity()
	// Only persist real identities — the fallback would clobber a real
	// identity written by a later run.
	if id.Email != FallbackHumanIdentity.Email {
		if err := r.persist(id); err != nil {
			// Non-fatal: persistence failure just means we probe again
			// next run. Log-level concerns are up to the caller.
			_ = err
		}
	}
	r.mu.Lock()
	copy := id
	r.localCache = &copy
	r.mu.Unlock()
	return id
}

// Lookup returns the HumanIdentity cached for the given slug, if any.
// The slug is the URL-safe handle derived from the email local-part;
// see deriveSlug.
func (r *HumanIdentityRegistry) Lookup(slug string) (HumanIdentity, bool) {
	slug = strings.TrimSpace(slug)
	if slug == "" {
		return HumanIdentity{}, false
	}
	for _, id := range r.List() {
		if id.Slug == slug {
			return id, true
		}
	}
	return HumanIdentity{}, false
}

// List returns every identity currently cached on disk. Ordering is not
// guaranteed. Errors from the filesystem are swallowed — an unreadable
// cache entry simply does not appear in the list.
func (r *HumanIdentityRegistry) List() []HumanIdentity {
	entries, err := os.ReadDir(r.dir)
	if err != nil {
		return nil
	}
	out := make([]HumanIdentity, 0, len(entries))
	seen := make(map[string]struct{}, len(entries))
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		bytes, err := os.ReadFile(filepath.Join(r.dir, e.Name()))
		if err != nil {
			continue
		}
		var id HumanIdentity
		if err := json.Unmarshal(bytes, &id); err != nil {
			continue
		}
		if _, dup := seen[id.Slug]; dup {
			continue
		}
		seen[id.Slug] = struct{}{}
		out = append(out, id)
	}
	return out
}

// Observe records an identity seen on a landed commit. Idempotent —
// writes the cache file only if a matching entry is not already on
// disk. This is how the registry grows when a second human's commit
// arrives.
func (r *HumanIdentityRegistry) Observe(name, email string) (HumanIdentity, error) {
	id, err := buildIdentity(name, email)
	if err != nil {
		return HumanIdentity{}, err
	}
	if err := r.persist(id); err != nil {
		return HumanIdentity{}, err
	}
	return id, nil
}

// persist writes an identity to {dir}/{sha}.json idempotently. Existing
// entries are left untouched so CreatedAt is stable across runs.
func (r *HumanIdentityRegistry) persist(id HumanIdentity) error {
	if strings.TrimSpace(id.Email) == "" {
		return fmt.Errorf("human identity: email is required")
	}
	if err := os.MkdirAll(r.dir, 0o700); err != nil {
		return fmt.Errorf("human identity: mkdir %s: %w", r.dir, err)
	}
	path := filepath.Join(r.dir, identityFilename(id.Email))
	if _, err := os.Stat(path); err == nil {
		// Already cached — keep the original CreatedAt.
		return nil
	}
	bytes, err := json.MarshalIndent(id, "", "  ")
	if err != nil {
		return fmt.Errorf("human identity: marshal: %w", err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, bytes, 0o600); err != nil {
		return fmt.Errorf("human identity: write tmp: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("human identity: rename: %w", err)
	}
	return nil
}

// identityFilename hashes the (lowercased) email so each identity has a
// stable, filesystem-safe filename independent of its display form.
func identityFilename(email string) string {
	sum := sha256.Sum256([]byte(strings.ToLower(strings.TrimSpace(email))))
	return hex.EncodeToString(sum[:])[:16] + ".json"
}

// probeLocalGitIdentity runs `git config --global user.name` and
// `user.email` in the caller's shell environment (NOT the sandboxed
// repo env used by runGitLocked — we want the user's real config).
// Returns FallbackHumanIdentity when either probe fails or yields
// empty output.
func probeLocalGitIdentity() HumanIdentity {
	name := runGitConfig("user.name")
	email := runGitConfig("user.email")
	if name == "" || email == "" {
		return FallbackHumanIdentity
	}
	id, err := buildIdentity(name, email)
	if err != nil {
		return FallbackHumanIdentity
	}
	return id
}

func runGitConfig(key string) string {
	// Deliberately NOT setting GIT_CONFIG_GLOBAL=/dev/null here — we
	// WANT the user's real global config. Honour a short timeout so a
	// hung git doesn't stall broker startup.
	//
	// gitCleanEnv strips GIT_CONFIG_PARAMETERS and friends: when wuphf
	// runs inside a git hook, the outer git may inject `-c` overrides
	// via that env var which would silently override --global reads.
	cmd := exec.Command("git", "config", "--global", key)
	cmd.Env = gitCleanEnv()
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// buildIdentity validates inputs and constructs a HumanIdentity. Shared
// by the local probe and the Observe path so both take the same slug
// derivation rules.
func buildIdentity(name, email string) (HumanIdentity, error) {
	name = strings.TrimSpace(name)
	email = strings.TrimSpace(email)
	if name == "" {
		return HumanIdentity{}, fmt.Errorf("human identity: name is required")
	}
	if email == "" {
		return HumanIdentity{}, fmt.Errorf("human identity: email is required")
	}
	slug := deriveSlug(email)
	if slug == "" {
		return HumanIdentity{}, fmt.Errorf("human identity: could not derive slug from %q", email)
	}
	return HumanIdentity{
		Name:      name,
		Email:     email,
		Slug:      slug,
		CreatedAt: time.Now().UTC(),
	}, nil
}

var slugSanitizer = regexp.MustCompile(`[^a-z0-9]+`)

// deriveSlug turns an email into a url-safe kebab-case handle:
//
//	"Sarah.Chen@acme.com"       → "sarah-chen"
//	"founder+work@example.io"   → "founder-work"
//	"a_b.c@x.y"                 → "a-b-c"
//
// Collisions across domains are possible (`sarah@a.com` and `sarah@b.com`
// both slug to `sarah`), which is why the on-disk filename hashes the
// full email rather than the slug. Collision within the slug namespace
// is accepted for v1.5 — rare in practice at team scale.
func deriveSlug(email string) string {
	local := strings.ToLower(email)
	if at := strings.IndexByte(local, '@'); at >= 0 {
		local = local[:at]
	}
	local = slugSanitizer.ReplaceAllString(local, "-")
	local = strings.Trim(local, "-")
	return local
}
