package workspaces

import "time"

// TrashEntry is the orchestrator-level shape for a shredded workspace held
// under ~/.wuphf-spaces/.trash/. The directory name encodes both the
// original workspace name and the shred-time unix timestamp, of the form
// "<name>-<unix-timestamp>". TrashID is that directory name verbatim and
// is what the Restore call takes as input.
type TrashEntry struct {
	// Name is the original workspace name parsed from the trash dir.
	Name string `json:"name"`
	// TrashID is the trash directory name ("<name>-<unix-ts>").
	TrashID string `json:"trash_id"`
	// Path is the absolute path to the trash directory on disk. Not serialised
	// to JSON to avoid leaking server filesystem layout to API clients.
	Path string `json:"-"`
	// ShredAt is the moment the workspace was moved to trash, parsed from
	// the trailing unix-timestamp segment of the directory name.
	ShredAt time.Time `json:"shred_at"`
	// OriginalRuntimeHome is the runtime-home path the workspace had when
	// it was shredded. Empty if the directory layout cannot be recovered.
	OriginalRuntimeHome string `json:"original_runtime_home,omitempty"`
}
