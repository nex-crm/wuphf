package main

// `wuphf workspace list` — table or JSON view of registered workspaces.
//
// Default output is a fixed-width human-readable table; `--json` emits a
// stable JSON shape for automation; `--trash` switches to trashed-workspace
// listings (so users can find a trash ID for `restore`). The flags are
// mutually compatible — `--json --trash` returns the trash slice as JSON.

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"time"
)

// runWorkspaceList is the entry point invoked by runWorkspace. Splits flag
// parsing from output rendering so listOutput can be unit-tested without
// stubbing flag parsing.
func runWorkspaceList(args []string) {
	fs := flag.NewFlagSet("workspace list", flag.ContinueOnError)
	jsonOut := fs.Bool("json", false, "Emit stable JSON instead of the human-readable table")
	trash := fs.Bool("trash", false, "List trashed workspaces with their trash IDs")
	fs.SetOutput(os.Stderr)
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "wuphf workspace list — show workspaces")
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "Usage:")
		fmt.Fprintln(os.Stderr, "  wuphf workspace list              Human-readable table")
		fmt.Fprintln(os.Stderr, "  wuphf workspace list --json       Stable JSON for scripts")
		fmt.Fprintln(os.Stderr, "  wuphf workspace list --trash      Trashed workspaces with restore IDs")
	}
	if err := fs.Parse(args); err != nil {
		os.Exit(2)
	}

	orch := resolveOrchestrator()
	ctx, cancel := workspaceCtx()
	defer cancel()

	res, err := orch.List(ctx, ListOpts{IncludeTrash: *trash})
	if err != nil {
		printError("list workspaces: %v", err)
	}

	if err := renderList(os.Stdout, res, *jsonOut, *trash); err != nil {
		printError("render list: %v", err)
	}
}

// renderList is the side-effect-free rendering core. Tests pump bytes into a
// bytes.Buffer to assert content and shape.
func renderList(w io.Writer, res ListResult, jsonOut, trashOnly bool) error {
	if jsonOut {
		// Stable JSON shape: always emit both fields so consumers can rely on
		// presence (`workspaces: []` vs `null`). Sorting keeps the output
		// deterministic across runs.
		out := ListResult{
			Workspaces: append([]Workspace(nil), res.Workspaces...),
			Trash:      append([]TrashEntry(nil), res.Trash...),
		}
		sort.Slice(out.Workspaces, func(i, j int) bool {
			return out.Workspaces[i].Name < out.Workspaces[j].Name
		})
		sort.Slice(out.Trash, func(i, j int) bool {
			return out.Trash[i].TrashID < out.Trash[j].TrashID
		})
		// Empty slices render as `[]` not `null` — matters for jq pipelines.
		if out.Workspaces == nil {
			out.Workspaces = []Workspace{}
		}
		if out.Trash == nil {
			out.Trash = []TrashEntry{}
		}
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(out)
	}

	if trashOnly {
		return renderTrashTable(w, res.Trash)
	}
	return renderWorkspaceTable(w, res.Workspaces)
}

// renderWorkspaceTable formats the workspaces list as a fixed-width table.
// Columns: NAME | STATE | BROKER | LAST USED | COST. The formatting is
// stable for any reasonable terminal width; we don't truncate aggressively
// because workspace names are slug-validated to ≤31 chars.
func renderWorkspaceTable(w io.Writer, workspaces []Workspace) error {
	if len(workspaces) == 0 {
		_, err := fmt.Fprintln(w, "No workspaces yet. Run `wuphf workspace create <name>` to start one.")
		return err
	}
	// Sort by name for stable output. Active/CLI-current marker is rendered
	// per-row via prefix glyph, NOT by reordering, because users scanning the
	// list expect alphabetical regardless of which one is "current".
	sorted := append([]Workspace(nil), workspaces...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Name < sorted[j].Name })

	if _, err := fmt.Fprintf(w, "%-2s %-31s  %-12s  %-7s  %-16s  %s\n",
		"", "NAME", "STATE", "BROKER", "LAST USED", "COST",
	); err != nil {
		return err
	}
	for _, ws := range sorted {
		marker := " "
		if ws.IsCLICurrent {
			marker = "*"
		}
		brokerCol := fmt.Sprintf(":%d", ws.BrokerPort)
		lastCol := "-"
		if !ws.LastUsedAt.IsZero() {
			lastCol = ws.LastUsedAt.Local().Format("2006-01-02 15:04")
		}
		costCol := "-"
		if ws.CostUSD > 0 {
			costCol = fmt.Sprintf("$%.2f", ws.CostUSD)
		}
		state := string(ws.State)
		if state == "" {
			state = string(WorkspaceStateNeverStarted)
		}
		if _, err := fmt.Fprintf(w, "%-2s %-31s  %-12s  %-7s  %-16s  %s\n",
			marker, ws.Name, state, brokerCol, lastCol, costCol,
		); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintln(w, ""); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(w, "* = active CLI workspace. Switch with `wuphf workspace switch <name>`."); err != nil {
		return err
	}
	return nil
}

// renderTrashTable formats trashed entries. The trash ID column is wide
// because `<name>-<unix-timestamp>` runs ~40 chars and users need to copy
// it whole into `restore`.
func renderTrashTable(w io.Writer, trash []TrashEntry) error {
	if len(trash) == 0 {
		_, err := fmt.Fprintln(w, "Trash is empty. Shredded workspaces appear here for 30 days.")
		return err
	}
	sorted := append([]TrashEntry(nil), trash...)
	// Newest first — most likely the user just shredded and wants it back.
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].ShreddedAt.After(sorted[j].ShreddedAt)
	})

	if _, err := fmt.Fprintf(w, "%-50s  %-31s  %-16s  %s\n",
		"TRASH ID", "ORIGINAL NAME", "SHREDDED AT", "SIZE",
	); err != nil {
		return err
	}
	for _, t := range sorted {
		shredCol := "-"
		if !t.ShreddedAt.IsZero() {
			shredCol = t.ShreddedAt.Local().Format("2006-01-02 15:04")
		}
		sizeCol := "-"
		if t.SizeBytes > 0 {
			sizeCol = humanBytes(t.SizeBytes)
		}
		if _, err := fmt.Fprintf(w, "%-50s  %-31s  %-16s  %s\n",
			t.TrashID, t.Name, shredCol, sizeCol,
		); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintln(w, ""); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(w, "Restore with `wuphf workspace restore <trash-id>` (allocates a fresh port pair)."); err != nil {
		return err
	}
	return nil
}

// humanBytes formats a byte count as KB/MB/GB with one decimal of precision.
// Local helper because the rest of the CLI doesn't have one yet — pulling in
// a dep for this would be overkill.
func humanBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for n2 := n / unit; n2 >= unit; n2 /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %sB", float64(n)/float64(div), "KMGTPE"[exp:exp+1])
}

// formatDuration is a small helper used for human-friendly "X ago" rendering
// in workspace_pause and workspace_doctor. Kept in this file because the
// list command is the most common consumer of relative timestamps.
func formatDurationSince(t time.Time) string {
	if t.IsZero() {
		return "-"
	}
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds ago", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd ago", int(d.Hours())/24)
	}
}

// firstNonEmpty picks the first non-blank string. Used for falling back
// inheritance defaults when the user omits the flag.
func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
