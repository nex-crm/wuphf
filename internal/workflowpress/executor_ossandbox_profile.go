package workflowpress

import (
	"sort"
	"strings"
)

// executor_ossandbox_profile.go holds the PURE profile/args generators for the
// OS-native sandbox backend. They take an ExecConfig and emit a deterministic
// Seatbelt profile (darwin) or a bwrap argv (linux). Keeping them pure and
// GOOS-free means they compile and unit-test on ANY host, while the live exec
// in Execute is gated on the real platform.
//
// Both generators are deny-by-default and canonicalize every allow-listed path
// (symlinks resolved) so the boundary matches what the kernel actually enforces.

// systemReadBase is the minimal set of read paths any process needs to start on
// macOS: the dynamic loader, the shared cache, the system frameworks, and the
// root literals used for path resolution. WITHOUT this base a deny-default
// profile aborts the process in dyld (SIGABRT) before main, so it would not be a
// "deny" so much as a crash. These grant READ ONLY of OS-owned, non-secret
// locations; they never grant write, and never cover a user's data directories.
// Derived empirically against sandbox-exec on this macOS box and aligned with
// @anthropic-ai/sandbox-runtime's base profile.
var systemReadBase = []string{
	"/usr",
	"/System",
	"/Library",
	"/bin",
	"/sbin",
	"/private/var/db/dyld",
	"/private/var/db/timezone",
	"/dev",
}

// systemReadLiterals are individual files/dirs (not subpaths) the loader and
// libc consult for path resolution and locale.
var systemReadLiterals = []string{
	"/",
	"/private",
	"/private/etc",
	"/private/etc/localtime",
	"/private/var",
}

// seatbeltProfile generates a deterministic macOS Seatbelt (sandbox-exec) profile
// from cfg. The profile:
//
//   - (deny default)            — nothing is permitted unless explicitly allowed;
//   - (deny network*)           — egress is denied (Net allow-list is refused
//     upstream; this tier is deny-all-network);
//   - (allow process*)          — the action may exec/fork (the boundary is FS +
//     network, not process control);
//   - (allow file-read* ...)    — ONLY the system read base plus cfg.FS read
//     entries;
//   - (allow file-write* ...)   — ONLY cfg.FS entries with Write=true.
//
// Every path is canonicalized and escaped, and the read/write entries are sorted
// for byte-stable output.
func seatbeltProfile(cfg ExecConfig) string {
	var b strings.Builder
	b.WriteString("(version 1)\n")
	b.WriteString("(deny default)\n")
	// Egress denied. Net allow-list is refused by Execute before we get here, so
	// this is always deny-all in this tier.
	b.WriteString("(deny network*)\n")
	// Process control is not the boundary; the FS + network are.
	b.WriteString("(allow process-exec)\n")
	b.WriteString("(allow process-fork)\n")
	b.WriteString("(allow signal (target self))\n")
	b.WriteString("(allow sysctl-read)\n")
	b.WriteString("(allow mach-lookup)\n")
	b.WriteString("(allow mach-priv-host-port)\n")
	b.WriteString("(allow iokit-open)\n")
	b.WriteString("(allow file-read-metadata)\n")

	// Reads: system base + literals + canonicalized, de-duplicated user reads.
	b.WriteString("(allow file-read*\n")
	for _, p := range systemReadBase {
		b.WriteString("  (subpath ")
		b.WriteString(seatbeltString(p))
		b.WriteString(")\n")
	}
	for _, p := range systemReadLiterals {
		b.WriteString("  (literal ")
		b.WriteString(seatbeltString(p))
		b.WriteString(")\n")
	}
	for _, p := range canonicalReadPaths(cfg) {
		b.WriteString("  (subpath ")
		b.WriteString(seatbeltString(p))
		b.WriteString(")\n")
	}
	b.WriteString(")\n")

	// Writes: ONLY canonicalized, de-duplicated user write entries. Omit the
	// clause entirely when there are none, so deny-default governs all writes.
	writes := canonicalWritePaths(cfg)
	if len(writes) > 0 {
		b.WriteString("(allow file-write*\n")
		for _, p := range writes {
			b.WriteString("  (subpath ")
			b.WriteString(seatbeltString(p))
			b.WriteString(")\n")
		}
		b.WriteString(")\n")
	}
	return b.String()
}

// canonicalReadPaths returns the canonicalized, sorted, de-duplicated set of FS
// allow-list paths that grant read (every entry grants read; Write only adds
// write on top).
func canonicalReadPaths(cfg ExecConfig) []string {
	set := map[string]struct{}{}
	for _, fs := range cfg.FS {
		if fs.Path == "" {
			continue
		}
		set[canonicalPath(fs.Path)] = struct{}{}
	}
	return sortedPathSet(set)
}

// canonicalWritePaths returns the canonicalized, sorted, de-duplicated set of FS
// allow-list paths whose Write flag is set.
func canonicalWritePaths(cfg ExecConfig) []string {
	set := map[string]struct{}{}
	for _, fs := range cfg.FS {
		if fs.Path == "" || !fs.Write {
			continue
		}
		set[canonicalPath(fs.Path)] = struct{}{}
	}
	return sortedPathSet(set)
}

func sortedPathSet(set map[string]struct{}) []string {
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// seatbeltString renders a Go string as a Seatbelt (TinyScheme) string literal,
// escaping backslashes and double-quotes so a crafted path cannot break out of
// the quoted token and inject a profile directive. This is the injection guard
// for the FS allow-list.
func seatbeltString(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '\\', '"':
			b.WriteByte('\\')
			b.WriteRune(r)
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}

// bwrapArgs generates the bubblewrap (bwrap) argv (WITHOUT the leading bwrap
// binary and WITHOUT the inner command) from cfg. The args:
//
//   - --unshare-all              — new user/ipc/pid/uts/cgroup/net namespaces;
//   - --unshare-net              — explicit net unshare (egress denied) UNLESS a
//     reviewed proxy is wired (Net is refused upstream, so this is always set
//     in this tier);
//   - --die-with-parent          — the sandbox dies if the runner dies;
//   - --new-session              — detach the controlling terminal (no TIOCSTI);
//   - --no-new-privs             — block setuid/privilege escalation;
//   - --proc /proc, --dev /dev   — minimal pseudo-filesystems;
//   - --ro-bind <base> <base>    — read-only system dirs needed to run a binary;
//   - --ro-bind <p> <p>          — each cfg.FS read entry, read-only;
//   - --bind <p> <p>             — each cfg.FS write entry, read-write.
//
// Paths are canonicalized; bwrap takes paths as distinct argv tokens, so there is
// no string-injection surface (no shell, no profile DSL).
func bwrapArgs(cfg ExecConfig) []string {
	args := []string{
		"--unshare-all",
		"--unshare-net",
		"--die-with-parent",
		"--new-session",
		"--no-new-privs",
		"--proc", "/proc",
		"--dev", "/dev",
	}
	// Read-only system bases so a dynamically-linked binary can start.
	for _, base := range linuxSystemReadBase {
		args = append(args, "--ro-bind-try", base, base)
	}
	// User reads: read-only bind. User writes: read-write bind. A write entry
	// implies read, so a path that is both read and write is bound rw once.
	writes := map[string]struct{}{}
	for _, p := range canonicalWritePaths(cfg) {
		writes[p] = struct{}{}
		args = append(args, "--bind", p, p)
	}
	for _, p := range canonicalReadPaths(cfg) {
		if _, isWrite := writes[p]; isWrite {
			continue // already bound read-write above
		}
		args = append(args, "--ro-bind", p, p)
	}
	return args
}

// linuxSystemReadBase is the minimal set of host directories a dynamically-linked
// binary needs to start under bwrap. --ro-bind-try tolerates a path that is
// absent on a given distro rather than failing the whole sandbox.
var linuxSystemReadBase = []string{
	"/usr",
	"/bin",
	"/sbin",
	"/lib",
	"/lib64",
	"/etc/alternatives",
}
