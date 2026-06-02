package team

// wiki_upload.go — Slice 3 of the cabinet wiki port: human asset upload.
//
// Surface
// =======
//
//	POST /wiki/upload  (multipart/form-data)
//	  field `file` — the uploaded bytes (filename carried in the part header)
//	  field `dir`  — destination folder, repo-root-relative under team/
//	                 (e.g. team/assets). May be `team` itself.
//	  → 200 {"path":"team/<dir>/<filename>","commit_sha":string}
//
// Behaviour
// =========
//
//   - `dir` is validated with resolveTeamRelPath so the destination can never
//     escape team/ (traversal, absolute, control bytes, team-secrets/ leak are
//     all rejected → 400), exactly like the page-ops + file-serve surfaces.
//   - The destination filename is derived SOLELY from the upload's own
//     basename: any path components a client smuggles in the multipart
//     `filename=` header are stripped, the remainder is sanitised to a safe
//     charset, and the extension is preserved. A collision appends -1/-2/…
//   - Dangerous executable extensions are blocked outright → 400. This is a
//     human-authored content store, not an app distribution channel.
//   - The request body is wrapped in http.MaxBytesReader so an oversize upload
//     is rejected (413) without buffering the whole payload to disk/memory.
//   - The write is a single git commit authored as the requesting human,
//     resolved server-side (identical to /wiki/write-human and the page ops);
//     clients cannot forge attribution. Assets are not markdown so they never
//     appear in index/all.md, but the commit goes through commitPathsLocked so
//     the index/ regen + staging convention stays identical to every other
//     human wiki write.
//
// Errors are JSON {"error":...}: 400 bad dir / blocked ext / bad form, 405
// non-POST, 413 too large, 500 (fixed string + server log — never the raw err),
// 503 backend inactive. This matches wiki_fs_handlers.go and wiki_page_ops.go.

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// maxUploadBytes caps a single asset upload at 25 MiB. The MaxBytesReader limit
// is set one byte higher than the cap so a payload of exactly maxUploadBytes is
// accepted while anything larger trips the reader and yields a clean 413.
const maxUploadBytes int64 = 25 << 20 // 25 MiB

// errWikiUploadBlockedExt signals an upload whose extension is on the
// executable blocklist. Mapped to 400 at the HTTP boundary.
var errWikiUploadBlockedExt = errors.New("wiki upload: file type is not allowed")

// blockedUploadExts is the set of executable / installer / script extensions
// that must never be written into the wiki content store. The wiki serves
// files same-origin (see wiki_fs_handlers.go); an executable payload here is a
// distribution + supply-chain vector even though the file-server itself never
// runs it. Extensions are lowercase with the leading dot.
var blockedUploadExts = map[string]struct{}{
	".exe":   {},
	".msi":   {},
	".bat":   {},
	".cmd":   {},
	".com":   {},
	".scr":   {},
	".dmg":   {},
	".app":   {},
	".pkg":   {},
	".deb":   {},
	".rpm":   {},
	".jar":   {},
	".sh":    {},
	".bash":  {},
	".zsh":   {},
	".ps1":   {},
	".psm1":  {},
	".vbs":   {},
	".vbe":   {},
	".js":    {}, // executable when double-clicked on Windows (WSH)
	".jse":   {},
	".wsf":   {},
	".wsh":   {},
	".cpl":   {},
	".dll":   {},
	".so":    {},
	".dylib": {},
	".bin":   {},
	".run":   {},
	".apk":   {},
	// Script-capable / same-origin-dangerous markup. The wiki serves files
	// same-origin (see wiki_fs_handlers.go); even though /wiki/file serves
	// these with a scripts-disabled sandbox CSP, they have no legitimate
	// document-asset use and blocking upload is correct defence-in-depth.
	".html":  {},
	".htm":   {},
	".svg":   {},
	".xml":   {},
	".xhtml": {},
	".xht":   {},
	".mhtml": {},
}

// UploadAsset writes the uploaded bytes into team/<dir>/<filename> and commits
// the file as the supplied human identity in a single commit. dir is a
// repo-root-relative path under team/ (it may be `team` itself); rawName is the
// client-supplied filename from the multipart part header. The on-disk filename
// is derived solely from the basename of rawName (sanitised, extension kept,
// collisions suffixed) so a client cannot steer the write outside team/<dir>.
//
// Returns the repo-root-relative slash path actually written and the new short
// commit SHA.
//
// Errors: errWikiFSBadPath (bad dir → 400), errWikiUploadBlockedExt (blocked
// extension → 400); any other error is an internal fault (→ 500).
func (r *Repo) UploadAsset(ctx context.Context, dir, rawName string, src io.Reader, identity HumanIdentity) (relPath string, commitSHA string, err error) {
	dirClean, dirAbs, err := resolveTeamRelPath(r.root, dir)
	if err != nil {
		return "", "", err
	}

	filename, err := safeUploadFilename(rawName)
	if err != nil {
		return "", "", err
	}

	name, email, _ := effectiveHumanIdentity(identity)

	r.mu.Lock()
	defer r.mu.Unlock()

	// The destination dir must exist and be a directory (created via the wiki
	// layout / a prior page write). MkdirAll is idempotent and confined to
	// dirAbs, which resolveTeamRelPath has already proven lives under team/.
	if err := os.MkdirAll(dirAbs, 0o700); err != nil {
		return "", "", fmt.Errorf("wiki upload: mkdir dest: %w", err)
	}

	// Resolve a non-colliding filename under the destination dir. The loop is
	// bounded so a directory already saturated with -N variants cannot spin.
	finalName, absPath, err := resolveUploadCollision(dirAbs, filename)
	if err != nil {
		return "", "", err
	}
	cleanRel := path.Join(dirClean, finalName)

	dst, err := os.OpenFile(absPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600) //nolint:gosec // absPath is confined to team/ by resolveTeamRelPath + collision loop
	if err != nil {
		return "", "", fmt.Errorf("wiki upload: create dest: %w", err)
	}
	// Bound the copy as a defence-in-depth backstop to the HTTP-layer
	// MaxBytesReader: even if a future caller forgets the reader cap, no more
	// than maxUploadBytes+1 bytes ever land on disk.
	written, copyErr := io.Copy(dst, io.LimitReader(src, maxUploadBytes+1))
	if closeErr := dst.Close(); closeErr != nil && copyErr == nil {
		copyErr = closeErr
	}
	if copyErr != nil {
		if rmErr := os.Remove(absPath); rmErr != nil && !errors.Is(rmErr, os.ErrNotExist) {
			return "", "", errors.Join(
				fmt.Errorf("wiki upload: write dest: %w", copyErr),
				fmt.Errorf("wiki upload: rollback %s: %w", cleanRel, rmErr),
			)
		}
		return "", "", fmt.Errorf("wiki upload: write dest: %w", copyErr)
	}
	if written > maxUploadBytes {
		// The HTTP layer should have rejected this already; treat a slip-through
		// as oversize so the caller still maps it to 413.
		if rmErr := os.Remove(absPath); rmErr != nil && !errors.Is(rmErr, os.ErrNotExist) {
			log.Printf("wiki upload: WARN remove oversize %s: %v", cleanRel, rmErr)
		}
		return "", "", errWikiUploadTooLarge
	}

	msg := fmt.Sprintf("human: upload %s", cleanRel)
	sha, commitErr := r.commitPathsLocked(ctx, name, email, msg, []string{cleanRel})
	if commitErr != nil {
		// Roll back the file so a failed commit does not leave an untracked
		// asset that RecoverDirtyTree would later fold into a recovery commit.
		if rmErr := os.Remove(absPath); rmErr != nil && !errors.Is(rmErr, os.ErrNotExist) {
			return "", "", errors.Join(commitErr, fmt.Errorf("wiki upload: rollback %s: %w", cleanRel, rmErr))
		}
		return "", "", commitErr
	}
	return cleanRel, sha, nil
}

// errWikiUploadTooLarge marks an upload that exceeded maxUploadBytes. It is a
// defence-in-depth signal from UploadAsset; the primary 413 path is the
// http.MaxBytesReader trip in the handler.
var errWikiUploadTooLarge = errors.New("wiki upload: file exceeds size limit")

// safeUploadFilename derives a safe on-disk filename from a client-supplied
// upload name. It strips every path component (basename only), sanitises the
// remainder to [A-Za-z0-9._-] (replacing any other rune with '-'), preserves a
// single extension, and rejects the executable blocklist. A name that
// sanitises down to nothing (or to only dots) is rejected as a bad path.
func safeUploadFilename(rawName string) (string, error) {
	// Take the basename under BOTH separator conventions so a Windows-style
	// "C:\\evil\\x.png" or a POSIX "../../x.png" both collapse to "x.png".
	// filepath.Base only honours the HOST separator, so on a POSIX runtime a
	// backslash survives — normalise '\' to '/' explicitly first, then take the
	// segment after the last '/'.
	name := strings.TrimSpace(rawName)
	name = strings.ReplaceAll(name, "\\", "/")
	name = name[strings.LastIndexByte(name, '/')+1:]
	name = strings.TrimSpace(name)
	if name == "" || name == "." || name == ".." {
		return "", fmt.Errorf("%w: upload filename is required", errWikiFSBadPath)
	}

	ext := strings.ToLower(filepath.Ext(name))
	if _, blocked := blockedUploadExts[ext]; blocked {
		return "", fmt.Errorf("%w: %s", errWikiUploadBlockedExt, ext)
	}

	base := strings.TrimSuffix(name, filepath.Ext(name))
	safeBase := sanitizeUploadToken(base)
	safeExt := sanitizeUploadToken(strings.TrimPrefix(ext, "."))

	if safeBase == "" {
		// e.g. an upload named ".gitignore" or "###.png" — keep a stable stub
		// rather than producing a dotfile or an empty base.
		safeBase = "file"
	}
	// Windows reserved device names (CON, PRN, AUX, NUL, COM1-9, LPT1-9) are
	// special even WITH an extension (e.g. "CON.txt" still resolves to the CON
	// device on Windows). Prefix a stable stub so the stored name is never a
	// reserved device name, regardless of the host OS the wiki later syncs to.
	if isWindowsReservedName(safeBase) {
		safeBase = "file-" + safeBase
	}
	if safeExt == "" {
		return safeBase, nil
	}
	return safeBase + "." + safeExt, nil
}

// windowsReservedNames is the set of legacy DOS/Windows device names. A file
// whose base (the part before the extension) matches one of these is special on
// Windows even with an extension, so we never store under such a base.
var windowsReservedNames = map[string]struct{}{
	"con": {}, "prn": {}, "aux": {}, "nul": {},
	"com1": {}, "com2": {}, "com3": {}, "com4": {}, "com5": {},
	"com6": {}, "com7": {}, "com8": {}, "com9": {},
	"lpt1": {}, "lpt2": {}, "lpt3": {}, "lpt4": {}, "lpt5": {},
	"lpt6": {}, "lpt7": {}, "lpt8": {}, "lpt9": {},
}

// isWindowsReservedName reports whether base (case-insensitive) is a Windows
// reserved device name. base is the already-sanitised filename stem.
func isWindowsReservedName(base string) bool {
	_, reserved := windowsReservedNames[strings.ToLower(base)]
	return reserved
}

// sanitizeUploadToken maps every rune outside [A-Za-z0-9._-] to '-', then
// collapses runs of '-' and trims leading/trailing '-' and '.'. The result is a
// single safe path segment (it can be empty, which the caller handles).
func sanitizeUploadToken(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	prevDash := false
	for _, r := range s {
		ok := (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') ||
			(r >= '0' && r <= '9') || r == '.' || r == '_' || r == '-'
		if !ok {
			r = '-'
		}
		if r == '-' {
			if prevDash {
				continue
			}
			prevDash = true
		} else {
			prevDash = false
		}
		b.WriteRune(r)
	}
	return strings.Trim(b.String(), "-.")
}

// resolveUploadCollision returns the first non-colliding filename under dirAbs,
// appending -1/-2/… before the extension on collision. It returns the chosen
// name and its absolute path. The probe count is bounded so a saturated
// directory fails loudly instead of looping forever.
func resolveUploadCollision(dirAbs, filename string) (string, string, error) {
	const maxProbes = 10000
	ext := filepath.Ext(filename)
	base := strings.TrimSuffix(filename, ext)

	candidate := filename
	for i := 0; i <= maxProbes; i++ {
		if i > 0 {
			candidate = fmt.Sprintf("%s-%d%s", base, i, ext)
		}
		abs := filepath.Join(dirAbs, candidate)
		if _, statErr := os.Lstat(abs); statErr != nil {
			if errors.Is(statErr, os.ErrNotExist) {
				return candidate, abs, nil
			}
			return "", "", fmt.Errorf("wiki upload: stat candidate: %w", statErr)
		}
	}
	return "", "", fmt.Errorf("wiki upload: too many name collisions for %q", filename)
}

// ── HTTP handler ─────────────────────────────────────────────────────────────

// handleWikiUpload handles POST /wiki/upload. multipart/form-data with a `file`
// part and a `dir` field. See UploadAsset for the write semantics.
func (b *Broker) handleWikiUpload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	worker := b.requireWikiWorker(w, "wiki upload")
	if worker == nil {
		return
	}

	// Cap the request body. The +1 lets a payload of exactly maxUploadBytes
	// through (the multipart envelope adds a little overhead, which is fine to
	// allow) while a genuinely oversize file trips the reader and surfaces as a
	// *http.MaxBytesError below → 413.
	r.Body = http.MaxBytesReader(w, r.Body, maxUploadBytes+(1<<20))

	// 32 MiB form-parse memory cap: parts above it spill to temp files, which
	// is fine — we stream the file part to disk anyway. The MaxBytesReader is
	// the real size gate.
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		if isMaxBytesError(err) {
			writeJSON(w, http.StatusRequestEntityTooLarge, map[string]string{"error": "upload exceeds size limit"})
			return
		}
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid multipart form"})
		return
	}
	if r.MultipartForm != nil {
		defer func() { _ = r.MultipartForm.RemoveAll() }()
	}

	dir := strings.TrimSpace(r.FormValue("dir"))
	if dir == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "dir field is required"})
		return
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		if isMaxBytesError(err) {
			writeJSON(w, http.StatusRequestEntityTooLarge, map[string]string{"error": "upload exceeds size limit"})
			return
		}
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "file field is required"})
		return
	}
	defer func() { _ = file.Close() }()

	identity := b.resolvePageIdentity(r)
	relPath, sha, err := worker.Repo().UploadAsset(r.Context(), dir, headerFilename(header), file, identity)
	if err != nil {
		writeUploadError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"path":       relPath,
		"commit_sha": sha,
	})
}

// headerFilename returns the client-supplied filename from a multipart part
// header, falling back to a stable default so an empty/omitted filename does
// not abort the upload (UploadAsset still sanitises it).
func headerFilename(header *multipart.FileHeader) string {
	if header == nil || strings.TrimSpace(header.Filename) == "" {
		return "file"
	}
	return header.Filename
}

// isMaxBytesError reports whether err is (or wraps) the MaxBytesReader limit
// error. ParseMultipartForm and FormFile both surface it, sometimes wrapped.
func isMaxBytesError(err error) bool {
	var maxErr *http.MaxBytesError
	if errors.As(err, &maxErr) {
		return true
	}
	// Older code paths and some wrappers carry only the sentinel string.
	return err != nil && strings.Contains(err.Error(), "http: request body too large")
}

// writeUploadError maps an upload error to the right HTTP status + JSON body,
// mirroring writePageError. Internal faults are logged server-side and reported
// with a fixed string so git stderr / filesystem layout never leaks to clients.
func writeUploadError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, errWikiUploadBlockedExt):
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
	case errors.Is(err, errWikiFSBadPath):
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
	case errors.Is(err, errWikiUploadTooLarge):
		writeJSON(w, http.StatusRequestEntityTooLarge, map[string]string{"error": "upload exceeds size limit"})
	default:
		log.Printf("wiki upload: internal error: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "wiki upload failed"})
	}
}
