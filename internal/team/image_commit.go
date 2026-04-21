package team

// image_commit.go hosts the repo-level commit path for image assets. Mirrors
// Repo.Commit / Repo.CommitNotebook shape, but:
//
//   - writes to team/assets/{yyyymm}/{sha[:12]}-{name}.{ext}
//   - optionally stages an accompanying thumb under team/assets/{yyyymm}/thumbs/
//   - does NOT regen index/all.md — the wiki index scans .md files, not binary
//     assets. Assets are referenced FROM articles; they are not themselves
//     catalog entries.
//   - commit message and author default to "system" when omitted, matching
//     bootstrap semantics. In practice the caller always supplies a slug.

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// CommitImageAsset writes the image payload + optional thumbnail bytes to the
// canonical content-addressed path and commits both in a single git commit
// under the supplied author slug.
//
// relPath must already be a valid team/assets/ path (see validateAssetPath).
// thumbRelPath may be empty when the asset is SVG or already smaller than
// ThumbWidth; in that case only the source is committed.
func (r *Repo) CommitImageAsset(
	ctx context.Context,
	slug, relPath string,
	payload []byte,
	thumbRelPath string,
	thumbBytes []byte,
	message string,
) (string, int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	slug = strings.TrimSpace(slug)
	if slug == "" {
		slug = "system"
	}
	if err := validateAssetPath(relPath); err != nil {
		return "", 0, err
	}
	if thumbRelPath != "" {
		if err := validateAssetPath(thumbRelPath); err != nil {
			return "", 0, err
		}
	}
	if len(payload) == 0 {
		return "", 0, fmt.Errorf("image: payload is empty")
	}

	fullAsset := filepath.Join(r.root, relPath)
	// Dedupe short-circuit: if the content-addressed path already exists with
	// an identical size, skip the write and return HEAD. Agents retrying the
	// same upload shouldn't thrash the git history.
	if info, err := os.Stat(fullAsset); err == nil && info.Size() == int64(len(payload)) {
		headSha, rerr := r.runGitLocked(ctx, "system", "rev-parse", "--short", "HEAD")
		if rerr == nil {
			return strings.TrimSpace(headSha), len(payload), nil
		}
	}

	if err := os.MkdirAll(filepath.Dir(fullAsset), 0o700); err != nil {
		return "", 0, fmt.Errorf("image: mkdir asset dir: %w", err)
	}
	if err := os.WriteFile(fullAsset, payload, 0o600); err != nil {
		return "", 0, fmt.Errorf("image: write asset: %w", err)
	}
	toAdd := []string{filepath.ToSlash(relPath)}
	if thumbRelPath != "" && len(thumbBytes) > 0 {
		fullThumb := filepath.Join(r.root, thumbRelPath)
		if err := os.MkdirAll(filepath.Dir(fullThumb), 0o700); err != nil {
			return "", 0, fmt.Errorf("image: mkdir thumb dir: %w", err)
		}
		if err := os.WriteFile(fullThumb, thumbBytes, 0o600); err != nil {
			return "", 0, fmt.Errorf("image: write thumb: %w", err)
		}
		toAdd = append(toAdd, filepath.ToSlash(thumbRelPath))
	}

	addArgs := append([]string{"add", "--"}, toAdd...)
	if out, err := r.runGitLocked(ctx, slug, addArgs...); err != nil {
		return "", 0, fmt.Errorf("image: git add: %w: %s", err, out)
	}

	cachedDiff, err := r.runGitLocked(ctx, slug, "diff", "--cached", "--name-only")
	if err != nil {
		return "", 0, fmt.Errorf("image: git diff --cached: %w", err)
	}
	if strings.TrimSpace(cachedDiff) == "" {
		headSha, herr := r.runGitLocked(ctx, "system", "rev-parse", "--short", "HEAD")
		if herr != nil {
			return "", 0, fmt.Errorf("image: resolve HEAD sha: %w", herr)
		}
		return strings.TrimSpace(headSha), len(payload), nil
	}

	commitMsg := strings.TrimSpace(message)
	if commitMsg == "" {
		commitMsg = fmt.Sprintf("wiki: upload image %s", filepath.Base(relPath))
	}
	if out, err := r.runGitLocked(ctx, slug, "commit", "-q", "-m", commitMsg); err != nil {
		return "", 0, fmt.Errorf("image: git commit: %w: %s", err, out)
	}
	sha, err := r.runGitLocked(ctx, slug, "rev-parse", "--short", "HEAD")
	if err != nil {
		return "", 0, fmt.Errorf("image: resolve HEAD sha: %w", err)
	}
	return strings.TrimSpace(sha), len(payload), nil
}

// CommitImageAltText writes a sidecar alt.md next to an asset, describing
// it for accessibility + search. Separate commit because alt-text is
// generated asynchronously by the vision agent — bundling it with the
// upload would block the uploader on an LLM round-trip.
func (r *Repo) CommitImageAltText(
	ctx context.Context,
	slug, altRelPath, altText, message string,
) (string, int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	slug = strings.TrimSpace(slug)
	if slug == "" {
		slug = "archivist"
	}
	if err := validateAssetPath(altRelPath); err != nil {
		return "", 0, err
	}
	if !strings.HasSuffix(altRelPath, ".alt.md") {
		return "", 0, fmt.Errorf("image: alt sidecar must end in .alt.md; got %q", altRelPath)
	}
	altText = strings.TrimSpace(altText)
	if altText == "" {
		return "", 0, fmt.Errorf("image: alt text is empty")
	}
	full := filepath.Join(r.root, altRelPath)
	if err := os.MkdirAll(filepath.Dir(full), 0o700); err != nil {
		return "", 0, fmt.Errorf("image: mkdir alt dir: %w", err)
	}
	if err := os.WriteFile(full, []byte(altText+"\n"), 0o600); err != nil {
		return "", 0, fmt.Errorf("image: write alt: %w", err)
	}
	if out, err := r.runGitLocked(ctx, slug, "add", "--", filepath.ToSlash(altRelPath)); err != nil {
		return "", 0, fmt.Errorf("image: git add alt: %w: %s", err, out)
	}
	cachedDiff, err := r.runGitLocked(ctx, slug, "diff", "--cached", "--name-only")
	if err != nil {
		return "", 0, fmt.Errorf("image: git diff --cached: %w", err)
	}
	if strings.TrimSpace(cachedDiff) == "" {
		headSha, herr := r.runGitLocked(ctx, "system", "rev-parse", "--short", "HEAD")
		if herr != nil {
			return "", 0, fmt.Errorf("image: resolve HEAD sha: %w", herr)
		}
		return strings.TrimSpace(headSha), len(altText), nil
	}
	commitMsg := strings.TrimSpace(message)
	if commitMsg == "" {
		commitMsg = fmt.Sprintf("archivist: alt text for %s", filepath.Base(altRelPath))
	}
	if out, err := r.runGitLocked(ctx, slug, "commit", "-q", "-m", commitMsg); err != nil {
		return "", 0, fmt.Errorf("image: git commit alt: %w: %s", err, out)
	}
	sha, err := r.runGitLocked(ctx, slug, "rev-parse", "--short", "HEAD")
	if err != nil {
		return "", 0, fmt.Errorf("image: resolve HEAD sha: %w", err)
	}
	return strings.TrimSpace(sha), len(altText), nil
}

// ReadImageAlt returns the alt text for an asset, or "" if no sidecar exists.
func (r *Repo) ReadImageAlt(assetRelPath string) (string, error) {
	if err := validateAssetPath(assetRelPath); err != nil {
		return "", err
	}
	altPath := altSidecarRelPath(assetRelPath)
	full := filepath.Join(r.root, altPath)
	data, err := os.ReadFile(full)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}
	return strings.TrimSpace(string(data)), nil
}

// ReadImageAsset returns the raw bytes of an asset for the serve path.
// Caller MUST validate the asset path before calling.
func (r *Repo) ReadImageAsset(assetRelPath string) ([]byte, error) {
	if err := validateAssetPath(assetRelPath); err != nil {
		return nil, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return os.ReadFile(filepath.Join(r.root, assetRelPath))
}
