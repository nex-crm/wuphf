package team

// IsSafeTaskID guards task IDs that flow into filesystem paths or
// process launchers. The allow-list (alphanumeric + `-` + `_`, ≤128
// chars) was originally introduced in the CLI to keep `open`/`xdg-open`
// from re-parsing shell-meta characters; it is now also enforced at the
// broker HTTP layer + the on-disk Decision Packet store to close the
// path-traversal vector that an authenticated WUPHF_BROKER_TOKEN caller
// could otherwise reach by POSTing `/tasks/..%2F..%2Fetc%2Fx/block`.
func IsSafeTaskID(id string) bool {
	if id == "" || len(id) > 128 {
		return false
	}
	for _, r := range id {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= 'A' && r <= 'Z':
		case r >= '0' && r <= '9':
		case r == '-' || r == '_':
		default:
			return false
		}
	}
	return true
}
