// internal/channel/slug.go
package channel

import (
	"crypto/sha1"
	"encoding/hex"
	"io"
	"sort"
)

// DirectSlug returns a deterministic lookup key for a 1:1 DM.
// The two slugs are sorted lexicographically and joined with "__".
func DirectSlug(a, b string) string {
	if a > b {
		return b + "__" + a
	}
	return a + "__" + b
}

// GroupSlug returns a deterministic lookup key for a group DM.
// SHA1 hash of sorted member slugs (Mattermost-aligned).
func GroupSlug(members []string) string {
	sorted := make([]string, len(members))
	copy(sorted, members)
	sort.Strings(sorted)
	h := sha1.New()
	for _, m := range sorted {
		_, _ = io.WriteString(h, m)
	}
	return hex.EncodeToString(h.Sum(nil))
}
