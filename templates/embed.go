// Package templates embeds the on-disk project templates the broker needs at
// runtime, so a shipped binary can materialize them without the source checkout
// being present on disk.
//
// AppScaffold is the Vite + React + TS starter the App Builder builds from. It
// is embedded (not just read from templates/app-scaffold/ at runtime) so the
// broker can PRE-SCAFFOLD a new app the instant its build task is created — the
// live preview then boots a real dev server in seconds instead of showing
// minutes of "Building…" dead air. The same files remain on disk for the agent
// to read directly; this embed is the single source of truth either way.
//
// The go:embed directive cannot reference parent directories, so this package
// lives in templates/ alongside the assets it embeds.
package templates

import "embed"

// AppScaffold is the App Builder starter project, rooted at "app-scaffold/".
// `all:` includes dotfiles (e.g. .gitignore) so the materialized project is
// byte-identical to the checked-in template.
//
//go:embed all:app-scaffold
var AppScaffold embed.FS

// AppScaffoldRoot is the directory prefix every AppScaffold path carries.
const AppScaffoldRoot = "app-scaffold"
