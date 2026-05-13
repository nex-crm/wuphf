package workspaces

import (
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// slugRe is the valid workspace slug pattern: lowercase letter, then
// lowercase letters/digits/hyphens, max 31 chars total.
var slugRe = regexp.MustCompile(`^[a-z][a-z0-9-]{0,30}$`)

// ReservedSlugs is the canonical, sorted list of names that may not be used
// for user-created workspaces because they collide with directory layout
// entries or carry implicit meaning. Add here whenever a new entry is added
// to ~/.wuphf-spaces/ layout.
var ReservedSlugs = func() []string {
	names := []string{
		"current",
		"default",
		"dev",
		"main", // auto-assigned to the migrated primary workspace
		"prod",
		"tokens", // collides with ~/.wuphf-spaces/tokens/ directory
		"trash",  // reserved (historical name for shred backups; see ~/.wuphf-spaces/.backups/)
	}
	sort.Strings(names)
	return names
}()

var errSlugEmpty = errors.New("workspaces: slug must not be empty")

// ErrSlugInvalid is returned for slugs that do not match the regex.
type ErrSlugInvalid struct{ Slug string }

func (e ErrSlugInvalid) Error() string {
	return fmt.Sprintf("workspaces: slug %q is invalid (must match ^[a-z][a-z0-9-]{0,30}$)", e.Slug)
}

// ErrSlugReserved is returned when slug matches a reserved name.
type ErrSlugReserved struct{ Slug string }

func (e ErrSlugReserved) Error() string {
	return fmt.Sprintf("workspaces: slug %q is reserved", e.Slug)
}

// ValidateSlug returns a non-nil error if name is not a legal workspace slug.
// It enforces the regex, reserved-name list, and filesystem-safety rules.
func ValidateSlug(name string) error {
	if name == "" {
		return errSlugEmpty
	}

	// Filesystem-safety: reject path traversal and leading dots.
	if strings.Contains(name, "/") {
		return fmt.Errorf("workspaces: slug %q must not contain '/'", name)
	}
	if strings.Contains(name, "..") {
		return fmt.Errorf("workspaces: slug %q must not contain '..'", name)
	}
	if strings.HasPrefix(name, ".") {
		return fmt.Errorf("workspaces: slug %q must not start with '.'", name)
	}
	if strings.HasPrefix(name, "__") {
		return fmt.Errorf("workspaces: slug %q must not start with '__'", name)
	}

	// Regex check.
	if !slugRe.MatchString(name) {
		return ErrSlugInvalid{Slug: name}
	}

	// Reserved-name check (binary search on sorted slice).
	idx := sort.SearchStrings(ReservedSlugs, name)
	if idx < len(ReservedSlugs) && ReservedSlugs[idx] == name {
		return ErrSlugReserved{Slug: name}
	}

	return nil
}
