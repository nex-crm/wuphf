package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// external orchestrator state types (input format — JSON file path)

type legacyAgent struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Role    string `json:"role"`
	Adapter string `json:"adapter"`
	Status  string `json:"status"`
}

type legacyCompany struct {
	ID     string        `json:"id"`
	Name   string        `json:"name"`
	Agents []legacyAgent `json:"agents"`
}

type legacyIssue struct {
	ID         string `json:"id"`
	Title      string `json:"title"`
	Status     string `json:"status"`
	AssigneeID string `json:"assignee_id"`
}

type legacyBudget struct {
	TotalUSD float64 `json:"total_usd"`
	SpentUSD float64 `json:"spent_usd"`
}

type legacyState struct {
	Companies []legacyCompany `json:"companies"`
	Issues    []legacyIssue   `json:"issues"`
	Budget    *legacyBudget   `json:"budget"`
}

// WUPHF broker state types (output format, mirrors internal/team unexported structs)

type importedMember struct {
	Slug      string `json:"slug"`
	Name      string `json:"name"`
	Role      string `json:"role,omitempty"`
	CreatedBy string `json:"created_by,omitempty"`
	CreatedAt string `json:"created_at,omitempty"`
}

type importedTask struct {
	ID        string `json:"id"`
	Channel   string `json:"channel,omitempty"`
	Title     string `json:"title"`
	Status    string `json:"status"`
	Owner     string `json:"owner,omitempty"`
	CreatedBy string `json:"created_by,omitempty"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

type importedChannel struct {
	Slug      string   `json:"slug"`
	Name      string   `json:"name"`
	Members   []string `json:"members,omitempty"`
	CreatedBy string   `json:"created_by,omitempty"`
	CreatedAt string   `json:"created_at,omitempty"`
}

type importedBrokerState struct {
	Messages []json.RawMessage `json:"messages"`
	Members  []importedMember  `json:"members,omitempty"`
	Channels []importedChannel `json:"channels,omitempty"`
	Tasks    []importedTask    `json:"tasks,omitempty"`
	Counter  int               `json:"counter"`
}

func runImport(args []string) {
	fs := flag.NewFlagSet("import", flag.ExitOnError)
	fromPath := fs.String("from", "", "Path to external orchestrator data directory, a .json export file, or \"legacy\" to auto-detect")
	port := fs.Int("port", 0, "Override external orchestrator's Postgres port (default: 54329)")
	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: %s import --from <source>\n\n", appName)
		fmt.Fprintf(os.Stderr, "Import external orchestrator state into WUPHF. No export step required.\n\n")
		fmt.Fprintf(os.Stderr, "Sources:\n")
		fmt.Fprintf(os.Stderr, "  legacy         Auto-connect to a running external orchestrator instance\n")
		fmt.Fprintf(os.Stderr, "  <directory>       Directory containing state.json or export.json\n")
		fmt.Fprintf(os.Stderr, "  <file.json>       Direct path to a JSON export file\n\n")
		fs.PrintDefaults()
	}
	_ = fs.Parse(args) // ExitOnError

	if *fromPath == "" {
		fs.Usage()
		os.Exit(1)
	}

	var state importedBrokerState
	var agentCount, taskCount int
	var err error

	if strings.ToLower(strings.TrimSpace(*fromPath)) == "legacy" {
		state, agentCount, taskCount, err = importFromLegacyDB(*port)
	} else {
		state, agentCount, taskCount, err = importFromJSONFile(*fromPath)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	if agentCount == 0 {
		fmt.Fprintf(os.Stderr, "warning: no agents found in source, importing empty state\n")
	}

	destPath := wuphfBrokerStatePath()
	if _, err := os.Stat(destPath); err == nil {
		fmt.Fprintf(os.Stderr, "warning: overwriting existing state at %s\n", destPath)
	}
	if err := os.MkdirAll(filepath.Dir(destPath), 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "error: could not create state directory: %v\n", err)
		os.Exit(1)
	}

	out, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: could not marshal state: %v\n", err)
		os.Exit(1)
	}
	if err := os.WriteFile(destPath, out, 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "error: could not write state: %v\n", err)
		os.Exit(1)
	}

	source := *fromPath
	if strings.ToLower(strings.TrimSpace(*fromPath)) == "legacy" {
		source = "external orchestrator"
	}
	fmt.Printf("Imported %d agents, %d tasks from %s. Run wuphf to launch.\n", agentCount, taskCount, source)
}

// importFromLegacyDB connects to external orchestrator's embedded Postgres and reads
// agents and issues directly. external orchestrator must be running.
func importFromLegacyDB(portOverride int) (importedBrokerState, int, int, error) {
	port := 54329
	if portOverride > 0 {
		port = portOverride
	}

	// Try to read external orchestrator config for a custom port
	if portOverride == 0 {
		if p, ok := readLegacyPort(); ok {
			port = p
		}
	}

	// External orchestrator runs an embedded Postgres on localhost with the
	// default `postgres:postgres` creds — not configurable, not a secret.
	// Only reached by `wuphf import --from legacy` against a user's local
	// orchestrator instance. No production code path uses this.
	// secretlint-disable-next-line @secretlint/secretlint-rule-database-connection-string
	connStr := fmt.Sprintf("postgres://postgres:postgres@localhost:%d/postgres?sslmode=disable", port)
	fmt.Printf("Connecting to external orchestrator (localhost:%d)...\n", port)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	conn, err := pgx.Connect(ctx, connStr)
	if err != nil {
		return importedBrokerState{}, 0, 0, fmt.Errorf(
			"could not connect to external orchestrator Postgres (localhost:%d): %w "+
				"(make sure external orchestrator is running before importing; "+
				"if it uses a different port, pass --port <number>)",
			port, err,
		)
	}
	defer func() { _ = conn.Close(context.Background()) }()

	// Read companies
	type dbCompany struct {
		ID   string
		Name string
	}
	companyRows, err := conn.Query(ctx, "SELECT id::text, name FROM companies ORDER BY created_at LIMIT 50")
	if err != nil {
		return importedBrokerState{}, 0, 0, fmt.Errorf("query companies: %w", err)
	}
	var companies []dbCompany
	for companyRows.Next() {
		var c dbCompany
		if err := companyRows.Scan(&c.ID, &c.Name); err != nil {
			fmt.Fprintf(os.Stderr, "warning: scan company row: %v\n", err)
			continue
		}
		companies = append(companies, c)
	}
	companyRows.Close()
	if err := companyRows.Err(); err != nil {
		return importedBrokerState{}, 0, 0, fmt.Errorf("iterate companies: %w", err)
	}

	if len(companies) == 0 {
		return importedBrokerState{}, 0, 0, fmt.Errorf("no companies found in external orchestrator database")
	}

	// Read agents
	type dbAgent struct {
		ID        string
		CompanyID string
		Name      string
		Role      string
		Status    string
	}
	agentRows, err := conn.Query(ctx,
		"SELECT id::text, company_id::text, name, COALESCE(role, ''), COALESCE(status, 'idle') FROM agents ORDER BY created_at",
	)
	if err != nil {
		return importedBrokerState{}, 0, 0, fmt.Errorf("query agents: %w", err)
	}
	var dbAgents []dbAgent
	agentIDToSlug := map[string]string{}
	for agentRows.Next() {
		var a dbAgent
		if err := agentRows.Scan(&a.ID, &a.CompanyID, &a.Name, &a.Role, &a.Status); err != nil {
			fmt.Fprintf(os.Stderr, "warning: scan agent row: %v\n", err)
			continue
		}
		dbAgents = append(dbAgents, a)
		agentIDToSlug[a.ID] = toSlug(a.Name)
	}
	agentRows.Close()
	if err := agentRows.Err(); err != nil {
		return importedBrokerState{}, 0, 0, fmt.Errorf("iterate agents: %w", err)
	}

	// Read issues
	type dbIssue struct {
		ID              string
		Title           string
		Status          string
		AssigneeAgentID string
		CreatedAt       time.Time
		UpdatedAt       time.Time
	}
	issueRows, err := conn.Query(ctx,
		"SELECT id::text, title, COALESCE(status, 'backlog'), COALESCE(assignee_agent_id::text, ''), created_at, updated_at FROM issues ORDER BY created_at",
	)
	if err != nil {
		return importedBrokerState{}, 0, 0, fmt.Errorf("query issues: %w", err)
	}
	var dbIssues []dbIssue
	for issueRows.Next() {
		var i dbIssue
		if err := issueRows.Scan(&i.ID, &i.Title, &i.Status, &i.AssigneeAgentID, &i.CreatedAt, &i.UpdatedAt); err != nil {
			fmt.Fprintf(os.Stderr, "warning: scan issue row: %v\n", err)
			continue
		}
		dbIssues = append(dbIssues, i)
	}
	issueRows.Close()
	if err := issueRows.Err(); err != nil {
		return importedBrokerState{}, 0, 0, fmt.Errorf("iterate issues: %w", err)
	}

	fmt.Printf("Found %d agents, %d tasks across %d company.\n", len(dbAgents), len(dbIssues), len(companies))

	now := time.Now().UTC().Format(time.RFC3339)
	var members []importedMember
	for _, a := range dbAgents {
		members = append(members, importedMember{
			Slug:      toSlug(a.Name),
			Name:      a.Name,
			Role:      a.Role,
			CreatedBy: "import",
			CreatedAt: now,
		})
	}

	var tasks []importedTask
	for i, issue := range dbIssues {
		owner := agentIDToSlug[issue.AssigneeAgentID]
		tasks = append(tasks, importedTask{
			ID:        fmt.Sprintf("task-%d", i+1),
			Channel:   "general",
			Title:     issue.Title,
			Status:    mapTaskStatus(issue.Status),
			Owner:     owner,
			CreatedBy: "import",
			CreatedAt: issue.CreatedAt.UTC().Format(time.RFC3339),
			UpdatedAt: issue.UpdatedAt.UTC().Format(time.RFC3339),
		})
	}

	memberSlugs := make([]string, 0, len(members))
	for _, m := range members {
		memberSlugs = append(memberSlugs, m.Slug)
	}
	channels := []importedChannel{{
		Slug:      "general",
		Name:      "General",
		Members:   memberSlugs,
		CreatedBy: "import",
		CreatedAt: now,
	}}

	state := importedBrokerState{
		Messages: []json.RawMessage{},
		Members:  members,
		Channels: channels,
		Tasks:    tasks,
		Counter:  len(tasks),
	}
	return state, len(members), len(tasks), nil
}

// readLegacyPort reads the external orchestrator config file to find a custom Postgres port.
func readLegacyPort() (int, bool) {
	home, err := os.UserHomeDir()
	if err != nil {
		return 0, false
	}
	legacyHome := os.Getenv("PAPERCLIP_HOME")
	if legacyHome == "" {
		legacyHome = filepath.Join(home, ".legacy")
	}
	instanceID := os.Getenv("PAPERCLIP_INSTANCE_ID")
	if instanceID == "" {
		instanceID = "default"
	}
	configPath := filepath.Join(legacyHome, "instances", instanceID, "config.json")
	data, err := os.ReadFile(configPath)
	if err != nil {
		return 0, false
	}
	var cfg struct {
		Database struct {
			EmbeddedPostgresPort int `json:"embeddedPostgresPort"`
		} `json:"database"`
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return 0, false
	}
	if p := cfg.Database.EmbeddedPostgresPort; p > 0 {
		return p, true
	}
	return 0, false
}

// importFromJSONFile reads a external orchestrator JSON export and converts it.
func importFromJSONFile(path string) (importedBrokerState, int, int, error) {
	source, err := resolveSourcePath(path)
	if err != nil {
		return importedBrokerState{}, 0, 0, err
	}
	data, err := os.ReadFile(source)
	if err != nil {
		return importedBrokerState{}, 0, 0, fmt.Errorf("could not read %s: %w", source, err)
	}
	var pc legacyState
	if err := json.Unmarshal(data, &pc); err != nil {
		return importedBrokerState{}, 0, 0, fmt.Errorf("invalid JSON in %s: %w", source, err)
	}
	state, agentCount, taskCount := convertToWUPHF(pc)
	return state, agentCount, taskCount, nil
}

// resolveSourcePath figures out the actual JSON file to read.
func resolveSourcePath(path string) (string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return "", fmt.Errorf("path does not exist: %s", path)
	}
	if !info.IsDir() {
		return path, nil
	}
	for _, name := range []string{"state.json", "export.json"} {
		candidate := filepath.Join(path, name)
		if _, err := os.Stat(candidate); err == nil {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("directory %s does not contain state.json or export.json", path)
}

// convertToWUPHF transforms external orchestrator JSON state into WUPHF broker state.
func convertToWUPHF(pc legacyState) (importedBrokerState, int, int) {
	now := time.Now().UTC().Format(time.RFC3339)

	var members []importedMember
	agentIDToSlug := map[string]string{}

	for _, co := range pc.Companies {
		for _, agent := range co.Agents {
			slug := toSlug(agent.Name)
			agentIDToSlug[agent.ID] = slug
			members = append(members, importedMember{
				Slug:      slug,
				Name:      agent.Name,
				Role:      agent.Role,
				CreatedBy: "import",
				CreatedAt: now,
			})
		}
	}

	var tasks []importedTask
	for i, issue := range pc.Issues {
		owner := agentIDToSlug[issue.AssigneeID]
		tasks = append(tasks, importedTask{
			ID:        fmt.Sprintf("task-%d", i+1),
			Channel:   "general",
			Title:     issue.Title,
			Status:    mapTaskStatus(issue.Status),
			Owner:     owner,
			CreatedBy: "import",
			CreatedAt: now,
			UpdatedAt: now,
		})
	}

	memberSlugs := make([]string, 0, len(members))
	for _, m := range members {
		memberSlugs = append(memberSlugs, m.Slug)
	}
	channels := []importedChannel{{
		Slug:      "general",
		Name:      "General",
		Members:   memberSlugs,
		CreatedBy: "import",
		CreatedAt: now,
	}}

	state := importedBrokerState{
		Messages: []json.RawMessage{},
		Members:  members,
		Channels: channels,
		Tasks:    tasks,
		Counter:  len(tasks),
	}
	return state, len(members), len(tasks)
}

func toSlug(name string) string {
	s := strings.ToLower(strings.TrimSpace(name))
	s = strings.ReplaceAll(s, " ", "-")
	return s
}

func mapTaskStatus(status string) string {
	switch status {
	case "todo", "backlog":
		return "todo"
	case "in_progress", "in progress":
		return "in_progress"
	case "done", "completed", "cancelled":
		return "done"
	default:
		return "todo"
	}
}

// wuphfBrokerStatePath mirrors defaultBrokerStatePath in internal/team/broker.go.
func wuphfBrokerStatePath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".wuphf", "team", "broker-state.json")
	}
	return filepath.Join(home, ".wuphf", "team", "broker-state.json")
}
