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

// contextPaths lists the relative-to-wuphfHome paths that get moved into the
// categorized "context" subfolder during shred. Anything under wuphfHome that
// is NOT wiki/ or sessions/ falls into this bucket so a categorized backup is
// a complete capture of the workspace state. Paths absent from a given
// workspace are silently skipped.
//
// The relative layout is preserved inside context/ so Restore can mirror it
// back without per-entry remapping (e.g. team/broker-state.json restores to
// <wuphfHome>/team/broker-state.json).
var contextPaths = []string{
	"team/broker-state.json",
	"team/broker-state.json.last-good",
	"onboarded.json",
	"company.json",
	"calendar.json",
	"office",
	"workflows",
	"logs",
	"providers",
	"codex-headless",
	"wiki.bak",
}

// writeCategorizedBackup moves the contents of <runtimeHome>/.wuphf/ into a
// fresh ~/.wuphf-spaces/.backups/<name>-<unix-ts>/ tree, split across the
// wiki / skills / chats / context subfolders, and writes a manifest.json
// describing what was captured. Returns the backup directory path.
//
// Source paths that don't exist in the workspace are silently skipped.
// Failures during partial moves leave the backup tree in place for forensic
// recovery; the caller is responsible for deciding whether to abort the
// subsequent runtime-home removal.
func writeCategorizedBackup(target *Workspace) (string, error) {
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

	// wiki -> backup/wiki (full subtree)
	wikiSrc := filepath.Join(wuphfDir, "wiki")
	wikiDst := filepath.Join(backupRoot, backupWikiDir)
	wikiMoved := false
	if _, err := os.Stat(wikiSrc); err == nil {
		if err := os.Rename(wikiSrc, wikiDst); err != nil {
			return backupRoot, fmt.Errorf("workspaces: backup %q: move wiki: %w", target.Name, err)
		}
		wikiMoved = true
	}

	// skills -> backup/skills (duplicate of wiki/team/skills for quick browsing).
	// We duplicate rather than move because the skills are also part of the
	// wiki tree and Restore relies on a complete wiki/ subtree.
	if wikiMoved {
		skillsSrc := filepath.Join(wikiDst, "team", "skills")
		skillsDst := filepath.Join(backupRoot, backupSkillsDir)
		if info, err := os.Stat(skillsSrc); err == nil && info.IsDir() {
			if err := copyDir(skillsSrc, skillsDst); err != nil {
				return backupRoot, fmt.Errorf("workspaces: backup %q: copy skills: %w", target.Name, err)
			}
		}
	}

	// sessions -> backup/chats
	sessionsSrc := filepath.Join(wuphfDir, "sessions")
	chatsDst := filepath.Join(backupRoot, backupChatsDir)
	if _, err := os.Stat(sessionsSrc); err == nil {
		if err := os.Rename(sessionsSrc, chatsDst); err != nil {
			return backupRoot, fmt.Errorf("workspaces: backup %q: move chats: %w", target.Name, err)
		}
	}

	// context -> remaining wuphfHome state, layout preserved
	contextRoot := filepath.Join(backupRoot, backupContextDir)
	if err := os.MkdirAll(contextRoot, 0o700); err != nil {
		return backupRoot, fmt.Errorf("workspaces: backup %q: mkdir context: %w", target.Name, err)
	}
	for _, rel := range contextPaths {
		src := filepath.Join(wuphfDir, rel)
		if _, err := os.Stat(src); err != nil {
			continue
		}
		dst := filepath.Join(contextRoot, rel)
		if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
			return backupRoot, fmt.Errorf("workspaces: backup %q: mkdir %s: %w", target.Name, filepath.Dir(rel), err)
		}
		if err := os.Rename(src, dst); err != nil {
			return backupRoot, fmt.Errorf("workspaces: backup %q: move %s: %w", target.Name, rel, err)
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

	// wiki -> wuphfHome/wiki
	if _, err := os.Stat(filepath.Join(backupDir, backupWikiDir)); err == nil {
		if err := os.Rename(filepath.Join(backupDir, backupWikiDir), filepath.Join(dstWuphf, "wiki")); err != nil {
			return fmt.Errorf("workspaces: restore: move wiki: %w", err)
		}
	}

	// chats -> wuphfHome/sessions
	if _, err := os.Stat(filepath.Join(backupDir, backupChatsDir)); err == nil {
		if err := os.Rename(filepath.Join(backupDir, backupChatsDir), filepath.Join(dstWuphf, "sessions")); err != nil {
			return fmt.Errorf("workspaces: restore: move chats: %w", err)
		}
	}

	// context/* -> wuphfHome/*, layout preserved
	contextRoot := filepath.Join(backupDir, backupContextDir)
	if _, err := os.Stat(contextRoot); err == nil {
		for _, rel := range contextPaths {
			src := filepath.Join(contextRoot, rel)
			if _, err := os.Stat(src); err != nil {
				continue
			}
			dst := filepath.Join(dstWuphf, rel)
			if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
				return fmt.Errorf("workspaces: restore: mkdir %s: %w", filepath.Dir(rel), err)
			}
			if err := os.Rename(src, dst); err != nil {
				return fmt.Errorf("workspaces: restore: move %s: %w", rel, err)
			}
		}
	}

	// skills/ is a duplicate of wiki/team/skills/ — already restored via wiki.
	// Drop the backup tree wholesale; anything left here is metadata or the
	// skills duplicate, neither of which contributes to a fresh runtime tree.
	if err := os.RemoveAll(backupDir); err != nil {
		return fmt.Errorf("workspaces: restore: cleanup backup dir: %w", err)
	}
	return nil
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
	defer in.Close()
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
