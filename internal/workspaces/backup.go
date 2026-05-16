package workspaces

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// TrashEntry is the orchestrator-level shape for a shredded workspace held
// under ~/.wuphf-spaces/.backups/. The directory name encodes both the
// original workspace name and the shred-time unix timestamp, of the form
// "<name>-<unix-timestamp>". TrashID is that directory name verbatim and
// is what the Restore call takes as input.
//
// The on-disk layout inside a backup directory is categorized:
//
//	<id>/
//	  manifest.json   — original_runtime_home, ports, shredded_at
//	  wiki/           — mirror of <wuphfHome>/wiki/ (includes wiki/team/skills/)
//	  skills/         — duplicate of <wuphfHome>/wiki/team/skills/ for quick access
//	  chats/          — mirror of <wuphfHome>/sessions/
//	  context/        — remaining state, preserves relative paths within wuphfHome
//
// "Trash" is preserved as the user-facing label for historical reasons;
// the underlying storage is the categorized backup tree above.
type TrashEntry struct {
	// Name is the original workspace name parsed from the backup dir.
	Name string `json:"name"`
	// TrashID is the backup directory name ("<name>-<unix-ts>").
	TrashID string `json:"trash_id"`
	// Path is the absolute path to the backup directory on disk. Not serialised
	// to JSON to avoid leaking server filesystem layout to API clients.
	Path string `json:"-"`
	// ShredAt is the moment the workspace was shredded, parsed from the
	// trailing unix-timestamp segment of the directory name.
	ShredAt time.Time `json:"shred_at"`
	// OriginalRuntimeHome is the runtime-home path the workspace had when
	// it was shredded. Empty if the directory layout cannot be recovered.
	OriginalRuntimeHome string `json:"original_runtime_home,omitempty"`
}

// backupManifest captures the metadata persisted alongside categorized
// workspace backups. It is sufficient to surface a TrashEntry and to
// reconstruct the runtime tree on Restore.
type backupManifest struct {
	Version             int       `json:"version"`
	OriginalName        string    `json:"original_name"`
	OriginalRuntimeHome string    `json:"original_runtime_home"`
	BrokerPort          int       `json:"broker_port,omitempty"`
	WebPort             int       `json:"web_port,omitempty"`
	ShreddedAt          time.Time `json:"shredded_at"`
	Blueprint           string    `json:"blueprint,omitempty"`
	CompanyName         string    `json:"company_name,omitempty"`
}

const (
	backupManifestVersion = 1
	backupManifestFile    = "manifest.json"
	backupWikiDir         = "wiki"
	backupSkillsDir       = "skills"
	backupChatsDir        = "chats"
	backupContextDir      = "context"
)

// categorySkipNames is the set of top-level wuphfDir entries that get their
// own categorized subfolder in a backup (wiki/, sessions/→chats/) and so must
// NOT be re-captured into the context/ catch-all. Anything else under
// wuphfDir is moved into context/ preserving its relative layout, which keeps
// the backup forward-compatible with new workspace state files (e.g. a future
// plugins/ or user-prefs.json) without code edits.
var categorySkipNames = map[string]struct{}{
	"wiki":     {},
	"sessions": {},
}

// movedPath records a single rename so partial failures inside
// writeCategorizedBackup can roll the source tree back to its original
// state. Tracked moves are replayed src↔dst on error before the function
// returns, leaving the runtime tree intact for a retry.
type movedPath struct {
	src string
	dst string
}

// writeCategorizedBackup moves the contents of <runtimeHome>/.wuphf/ into a
// fresh ~/.wuphf-spaces/.backups/<name>-<unix-ts>/ tree, split across the
// wiki / skills / chats / context subfolders, and writes a manifest.json
// describing what was captured. Returns the backup directory path.
//
// Source paths that don't exist in the workspace are silently skipped.
// Anything under wuphfDir that is not wiki/ or sessions/ is moved into
// context/, preserving relative paths so Restore can mirror them back.
//
// Failure semantics: if any move fails after at least one earlier move has
// succeeded, every successful move is rolled back (dst → src) so the source
// tree is restored to its pre-call state; the partial backup directory is
// then removed. Rollback failures are appended to the returned error so
// operators can recover manually rather than silently losing state.
func writeCategorizedBackup(target *Workspace) (backupRootResult string, returnErr error) {
	if target == nil {
		return "", errors.New("workspaces: backup: nil workspace")
	}
	sd, err := spacesDir()
	if err != nil {
		return "", err
	}
	wuphfDir := filepath.Join(target.RuntimeHome, ".wuphf")
	if _, err := os.Stat(wuphfDir); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", fmt.Errorf("workspaces: backup %q: no .wuphf at %s", target.Name, wuphfDir)
		}
		return "", fmt.Errorf("workspaces: backup %q: stat: %w", target.Name, err)
	}

	now := time.Now()
	backupID := fmt.Sprintf("%s-%d", target.Name, now.Unix())
	backupRoot := filepath.Join(sd, backupsDirName, backupID)
	if err := os.MkdirAll(backupRoot, 0o700); err != nil {
		return "", fmt.Errorf("workspaces: backup %q: mkdir: %w", target.Name, err)
	}

	var moves []movedPath
	defer func() {
		if returnErr == nil {
			return
		}
		rollbackErrs := rollbackMoves(moves)
		_ = os.RemoveAll(backupRoot)
		if len(rollbackErrs) > 0 {
			returnErr = fmt.Errorf("%w; rollback also failed: %v", returnErr, rollbackErrs)
		}
	}()

	move := func(src, dst, label string) error {
		if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
			return fmt.Errorf("workspaces: backup %q: mkdir %s: %w", target.Name, filepath.Dir(label), err)
		}
		if err := os.Rename(src, dst); err != nil {
			return fmt.Errorf("workspaces: backup %q: move %s: %w", target.Name, label, err)
		}
		moves = append(moves, movedPath{src: src, dst: dst})
		return nil
	}

	// wiki -> backup/wiki (full subtree). Only skip when the source is
	// genuinely missing; any other stat error (permission, I/O) propagates
	// so we never silently leave wiki out of the backup.
	wikiSrc := filepath.Join(wuphfDir, "wiki")
	wikiDst := filepath.Join(backupRoot, backupWikiDir)
	wikiMoved := false
	switch _, err := os.Stat(wikiSrc); {
	case err == nil:
		if err := move(wikiSrc, wikiDst, "wiki"); err != nil {
			return backupRoot, err
		}
		wikiMoved = true
	case errors.Is(err, os.ErrNotExist):
		// no wiki to back up
	default:
		return backupRoot, fmt.Errorf("workspaces: backup %q: stat wiki: %w", target.Name, err)
	}

	// skills -> backup/skills (duplicate of wiki/team/skills for quick browsing).
	// We duplicate rather than move because the skills are also part of the
	// wiki tree and Restore relies on a complete wiki/ subtree. Copy failure
	// here triggers a rollback because the wiki has already moved.
	if wikiMoved {
		skillsSrc := filepath.Join(wikiDst, "team", "skills")
		skillsDst := filepath.Join(backupRoot, backupSkillsDir)
		switch info, err := os.Stat(skillsSrc); {
		case err == nil && info.IsDir():
			if err := copyDir(skillsSrc, skillsDst); err != nil {
				return backupRoot, fmt.Errorf("workspaces: backup %q: copy skills: %w", target.Name, err)
			}
		case err == nil:
			// skills path exists but isn't a directory — leave the
			// categorized skills/ duplicate absent; wiki still has it.
		case errors.Is(err, os.ErrNotExist):
			// no skills subtree
		default:
			return backupRoot, fmt.Errorf("workspaces: backup %q: stat skills: %w", target.Name, err)
		}
	}

	// sessions -> backup/chats
	sessionsSrc := filepath.Join(wuphfDir, "sessions")
	chatsDst := filepath.Join(backupRoot, backupChatsDir)
	switch _, err := os.Stat(sessionsSrc); {
	case err == nil:
		if err := move(sessionsSrc, chatsDst, "chats"); err != nil {
			return backupRoot, err
		}
	case errors.Is(err, os.ErrNotExist):
		// no sessions to back up
	default:
		return backupRoot, fmt.Errorf("workspaces: backup %q: stat sessions: %w", target.Name, err)
	}

	// context -> remaining wuphfDir entries, layout preserved.
	// We enumerate wuphfDir dynamically (rather than hard-coding paths) so the
	// backup picks up new workspace state files automatically as the runtime
	// layout evolves. Only the categorized siblings (wiki, sessions) are
	// skipped here.
	contextRoot := filepath.Join(backupRoot, backupContextDir)
	if err := os.MkdirAll(contextRoot, 0o700); err != nil {
		return backupRoot, fmt.Errorf("workspaces: backup %q: mkdir context: %w", target.Name, err)
	}
	entries, err := os.ReadDir(wuphfDir)
	if err != nil {
		return backupRoot, fmt.Errorf("workspaces: backup %q: read wuphf dir: %w", target.Name, err)
	}
	for _, entry := range entries {
		name := entry.Name()
		if _, skip := categorySkipNames[name]; skip {
			continue
		}
		src := filepath.Join(wuphfDir, name)
		dst := filepath.Join(contextRoot, name)
		if err := move(src, dst, name); err != nil {
			return backupRoot, err
		}
	}

	manifest := backupManifest{
		Version:             backupManifestVersion,
		OriginalName:        target.Name,
		OriginalRuntimeHome: target.RuntimeHome,
		BrokerPort:          target.BrokerPort,
		WebPort:             target.WebPort,
		ShreddedAt:          now.UTC(),
		Blueprint:           target.Blueprint,
		CompanyName:         target.CompanyName,
	}
	manifestBytes, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return backupRoot, fmt.Errorf("workspaces: backup %q: marshal manifest: %w", target.Name, err)
	}
	if err := os.WriteFile(filepath.Join(backupRoot, backupManifestFile), manifestBytes, 0o600); err != nil {
		return backupRoot, fmt.Errorf("workspaces: backup %q: write manifest: %w", target.Name, err)
	}

	return backupRoot, nil
}

// restoreCategorizedBackup reconstructs a fresh wuphfHome at dstRuntimeHome
// from a categorized backup directory. Falls back to the legacy "flat tree
// with a .wuphf/ child" layout that Doctor's orphan-cleanup produces.
//
// The backup directory itself is removed on success; on failure the backup is
// left in place for manual recovery.
func restoreCategorizedBackup(backupDir, dstRuntimeHome string) error {
	dstWuphf := filepath.Join(dstRuntimeHome, ".wuphf")

	// Legacy fallback: doctor's orphan-cleanup writes the whole runtime tree
	// flat (no manifest, .wuphf/ as a direct child). Detect and rename the
	// entire directory into place — same behavior the previous Restore had.
	if _, err := os.Stat(filepath.Join(backupDir, backupManifestFile)); errors.Is(err, os.ErrNotExist) {
		if _, err := os.Stat(filepath.Join(backupDir, ".wuphf")); err == nil {
			return os.Rename(backupDir, dstRuntimeHome)
		}
	}

	if err := os.MkdirAll(dstWuphf, 0o700); err != nil {
		return fmt.Errorf("workspaces: restore: mkdir wuphf: %w", err)
	}

	// wiki -> wuphfHome/wiki. Treat genuine ENOENT as "nothing to restore";
	// any other stat error propagates so we don't silently drop a backup
	// subtree before deleting the source.
	wikiBackup := filepath.Join(backupDir, backupWikiDir)
	switch _, err := os.Stat(wikiBackup); {
	case err == nil:
		if err := os.Rename(wikiBackup, filepath.Join(dstWuphf, "wiki")); err != nil {
			return fmt.Errorf("workspaces: restore: move wiki: %w", err)
		}
	case errors.Is(err, os.ErrNotExist):
		// no wiki captured in this backup
	default:
		return fmt.Errorf("workspaces: restore: stat wiki: %w", err)
	}

	// chats -> wuphfHome/sessions
	chatsBackup := filepath.Join(backupDir, backupChatsDir)
	switch _, err := os.Stat(chatsBackup); {
	case err == nil:
		if err := os.Rename(chatsBackup, filepath.Join(dstWuphf, "sessions")); err != nil {
			return fmt.Errorf("workspaces: restore: move chats: %w", err)
		}
	case errors.Is(err, os.ErrNotExist):
		// no chats captured
	default:
		return fmt.Errorf("workspaces: restore: stat chats: %w", err)
	}

	// context/* -> wuphfHome/*. context/ now mirrors the wuphfDir top-level
	// layout (one entry per non-categorized dir/file), so iterate dynamically
	// — anything new the backup writer captured restores automatically.
	contextRoot := filepath.Join(backupDir, backupContextDir)
	entries, err := os.ReadDir(contextRoot)
	switch {
	case err == nil:
		for _, entry := range entries {
			name := entry.Name()
			src := filepath.Join(contextRoot, name)
			dst := filepath.Join(dstWuphf, name)
			if err := os.Rename(src, dst); err != nil {
				return fmt.Errorf("workspaces: restore: move %s: %w", name, err)
			}
		}
	case errors.Is(err, os.ErrNotExist):
		// no context captured
	default:
		return fmt.Errorf("workspaces: restore: read context: %w", err)
	}

	// skills/ is a duplicate of wiki/team/skills/ — already restored via wiki.
	// Drop the backup tree wholesale; anything left here is metadata or the
	// skills duplicate, neither of which contributes to a fresh runtime tree.
	if err := os.RemoveAll(backupDir); err != nil {
		return fmt.Errorf("workspaces: restore: cleanup backup dir: %w", err)
	}
	return nil
}

// rollbackMoves replays each recorded rename in reverse order, returning
// each rename back to its source. Used when writeCategorizedBackup detects
// a partial failure: if every reversal succeeds the source tree is restored
// exactly. Errors are collected (rather than fatal-on-first) so the caller
// can surface the full picture in a compound error.
func rollbackMoves(moves []movedPath) []error {
	var errs []error
	for i := len(moves) - 1; i >= 0; i-- {
		m := moves[i]
		if err := os.Rename(m.dst, m.src); err != nil {
			errs = append(errs, fmt.Errorf("rollback %s → %s: %w", m.dst, m.src, err))
		}
	}
	return errs
}

// readBackupManifest loads the manifest from a backup directory. Returns
// (nil, nil) when the manifest is absent (legacy flat-tree backup).
func readBackupManifest(backupDir string) (*backupManifest, error) {
	data, err := os.ReadFile(filepath.Join(backupDir, backupManifestFile))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	var m backupManifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("workspaces: backup: parse manifest: %w", err)
	}
	return &m, nil
}

// copyDir recursively copies a directory tree from src to dst, preserving
// regular files, directories, and symlinks. Permissions on regular files use
// 0o600; directory permissions use 0o700. Used to duplicate the skills subtree
// into the categorized backup view without disturbing the wiki tree itself.
func copyDir(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		switch {
		case info.IsDir():
			return os.MkdirAll(target, 0o700)
		case info.Mode()&os.ModeSymlink != 0:
			link, err := os.Readlink(path)
			if err != nil {
				return err
			}
			return os.Symlink(link, target)
		default:
			return copyFile(path, target)
		}
	})
}

func copyFile(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return err
	}
	return out.Close()
}

// trashIDIsValid reports whether the directory name has the
// "<name>-<unix-ts>" shape used by both shred backups and orphan moves.
func trashIDIsValid(id string) bool {
	return extractOriginalName(id) != "" && !strings.HasPrefix(id, ".")
}
