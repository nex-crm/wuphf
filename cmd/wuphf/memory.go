package main

// memory.go hosts the `wuphf memory` subcommand family. Today it owns
// one verb: `migrate`, which ports legacy Nex / GBrain memory into the
// markdown wiki at ~/.wuphf/wiki/team/.
//
// Why a separate file
// ===================
//
// main.go already routes top-level subcommands. `memory migrate` is a
// two-word verb that carries its own flag set and exit codes, so the
// parsing + plumbing lives here to keep main.go short.

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/nex-crm/wuphf/internal/api"
	"github.com/nex-crm/wuphf/internal/config"
	"github.com/nex-crm/wuphf/internal/migration"
	"github.com/nex-crm/wuphf/internal/team"
)

// runMemory dispatches `wuphf memory <verb>`. Called from main.go when
// args[0] == "memory".
func runMemory(args []string) {
	if len(args) == 0 || subcommandWantsHelp(args) {
		printMemoryHelp()
		return
	}
	verb := args[0]
	rest := args[1:]
	switch verb {
	case "migrate":
		runMemoryMigrate(rest)
	default:
		fmt.Fprintf(os.Stderr, "wuphf memory: unknown verb %q — run `wuphf memory --help` for the list.\n", verb)
		os.Exit(1)
	}
}

func printMemoryHelp() {
	fmt.Fprintln(os.Stderr, "wuphf memory — manage the team wiki and legacy memory backends")
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "Verbs:")
	fmt.Fprintln(os.Stderr, "  migrate    Port Nex or GBrain content into ~/.wuphf/wiki/team/")
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "Usage:")
	fmt.Fprintln(os.Stderr, "  wuphf memory migrate --from nex [--dry-run] [--limit N]")
	fmt.Fprintln(os.Stderr, "  wuphf memory migrate --from gbrain [--dry-run] [--limit N]")
}

// runMemoryMigrate parses flags and orchestrates the migration. On any
// fatal error it writes to stderr and exits non-zero.
func runMemoryMigrate(args []string) {
	fs := flag.NewFlagSet("memory migrate", flag.ContinueOnError)
	from := fs.String("from", "", "Source backend: nex or gbrain")
	dryRun := fs.Bool("dry-run", false, "Print the plan without committing")
	limit := fs.Int("limit", 0, "Cap the number of records imported (0 = unlimited)")
	apiKeyFlag := fs.String("api-key", "", "API key for Nex authentication (overrides WUPHF_API_KEY)")
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "wuphf memory migrate — import legacy memory into the team wiki")
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "Usage:")
		fmt.Fprintln(os.Stderr, "  wuphf memory migrate --from {nex,gbrain} [--dry-run] [--limit N]")
		fmt.Fprintln(os.Stderr, "")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		// ContinueOnError already wrote the message.
		os.Exit(2)
	}
	source := strings.ToLower(strings.TrimSpace(*from))
	if source == "" {
		fs.Usage()
		os.Exit(2)
	}

	adapter, err := buildAdapter(source, strings.TrimSpace(*apiKeyFlag))
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	writer, stop, err := openWikiWriter()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
	defer stop()

	migrator := migration.NewMigrator(writer)
	ctx := context.Background()
	summary, err := migrator.Run(ctx, adapter, migration.RunOptions{
		DryRun: *dryRun,
		Limit:  *limit,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	if *dryRun {
		printPlanTable(summary.Plans)
		fmt.Fprintf(os.Stdout, "\nDry run complete. %d would create, %d would skip, %d would collision-rename.\n",
			countAction(summary.Plans, "create"),
			summary.Skipped,
			summary.Collisions)
		return
	}
	fmt.Fprintf(os.Stdout, "Migrated from %s: %d written, %d skipped (identical), %d collision-renamed.\n",
		source, summary.Written, summary.Skipped, summary.Collisions)
}

// buildAdapter returns the concrete adapter for a source flag value.
func buildAdapter(source, apiKeyOverride string) (migration.Adapter, error) {
	switch source {
	case "nex":
		key := apiKeyOverride
		if key == "" {
			key = strings.TrimSpace(config.ResolveAPIKey(""))
		}
		if key == "" {
			return nil, fmt.Errorf("nex adapter requires an API key (set WUPHF_API_KEY or pass --api-key)")
		}
		client := api.NewClient(key)
		return migration.NewNexAdapter(client), nil
	case "gbrain":
		if !migration.GBrainReady() {
			return nil, fmt.Errorf("gbrain binary not found on PATH; install GBrain before migrating")
		}
		return migration.NewGBrainAdapter(), nil
	default:
		return nil, fmt.Errorf("unsupported --from value %q (expected nex or gbrain)", source)
	}
}

// workerWriter adapts *team.WikiWorker onto migration.WikiWriter. The
// worker already exposes Enqueue; Root is forwarded from its Repo.
type workerWriter struct{ w *team.WikiWorker }

func (ww workerWriter) Enqueue(ctx context.Context, slug, path, content, mode, commitMsg string) (string, int, error) {
	return ww.w.Enqueue(ctx, slug, path, content, mode, commitMsg)
}
func (ww workerWriter) Root() string { return ww.w.Repo().Root() }

// openWikiWriter initialises the wiki repo and starts a short-lived
// worker scoped to the CLI run. Returns the writer plus a teardown
// callback the caller must defer.
func openWikiWriter() (migration.WikiWriter, func(), error) {
	repo := team.NewRepo()
	ctx, cancel := context.WithCancel(context.Background())
	if err := repo.Init(ctx); err != nil {
		cancel()
		return nil, nil, fmt.Errorf("initialise wiki repo at %s: %w", repo.Root(), err)
	}
	worker := team.NewWikiWorker(repo, nil)
	worker.Start(ctx)
	teardown := func() {
		cancel()
		worker.Stop()
	}
	return workerWriter{w: worker}, teardown, nil
}

// printPlanTable renders a dry-run plan table. Tabwriter keeps the
// output scannable when slugs are long; stdout so it's easy to diff.
func printPlanTable(plans []migration.Plan) {
	tw := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "ACTION\tPATH\tSIZE\tAUTHOR\tSOURCE")
	for _, p := range plans {
		path := p.Path
		if p.Action == "collision-rename" && p.CollisionWith != "" {
			path = path + " (was: " + p.CollisionWith + ")"
		}
		fmt.Fprintf(tw, "%s\t%s\t%d\t%s\t%s\n", p.Action, path, p.Bytes, p.Author, p.Source)
	}
	_ = tw.Flush()
}

func countAction(plans []migration.Plan, action string) int {
	n := 0
	for _, p := range plans {
		if p.Action == action {
			n++
		}
	}
	return n
}
