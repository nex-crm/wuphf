package team

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/nex-crm/wuphf/internal/config"
	"gopkg.in/yaml.v3"
)

const (
	maxSurfacesReturned     = 100
	maxWidgetsPerSurface    = 50
	maxWidgetSourceBytes    = 64 * 1024
	maxTableRowsRendered    = 200
	maxTableColumnsRendered = 20
	maxPreviewTextBytes     = 8 * 1024
	maxHistoryEntriesRead   = 100
)

var (
	errSurfaceNotFound = errors.New("surface not found")
	errWidgetNotFound  = errors.New("widget not found")
	surfaceFileMu      sync.Mutex
)

type SurfaceStore struct {
	root string
}

type SurfaceManifest struct {
	ID          string         `json:"id" yaml:"id"`
	Title       string         `json:"title" yaml:"title"`
	Channel     string         `json:"channel" yaml:"channel"`
	Owner       string         `json:"owner,omitempty" yaml:"owner,omitempty"`
	CreatedBy   string         `json:"created_by,omitempty" yaml:"created_by,omitempty"`
	CreatedAt   string         `json:"created_at" yaml:"created_at"`
	UpdatedAt   string         `json:"updated_at" yaml:"updated_at"`
	Layout      map[string]any `json:"layout,omitempty" yaml:"layout,omitempty"`
	Permissions map[string]any `json:"permissions,omitempty" yaml:"permissions,omitempty"`
	WidgetCount int            `json:"widget_count,omitempty" yaml:"-"`
}

type SurfaceWidgetFile struct {
	ID            string           `json:"id" yaml:"id"`
	Title         string           `json:"title" yaml:"title"`
	Description   string           `json:"description,omitempty" yaml:"description,omitempty"`
	Kind          string           `json:"kind" yaml:"kind"`
	SchemaVersion string           `json:"schema_version,omitempty" yaml:"schema_version,omitempty"`
	Layout        map[string]any   `json:"layout,omitempty" yaml:"layout,omitempty"`
	Source        string           `json:"source" yaml:"source"`
	DataBindings  map[string]any   `json:"data_bindings,omitempty" yaml:"data_bindings,omitempty"`
	Actions       []map[string]any `json:"actions,omitempty" yaml:"actions,omitempty"`
	CreatedBy     string           `json:"created_by,omitempty" yaml:"created_by,omitempty"`
	UpdatedBy     string           `json:"updated_by,omitempty" yaml:"updated_by,omitempty"`
	CreatedAt     string           `json:"created_at,omitempty" yaml:"created_at,omitempty"`
	UpdatedAt     string           `json:"updated_at,omitempty" yaml:"updated_at,omitempty"`
}

type SurfaceDetail struct {
	Surface SurfaceManifest       `json:"surface"`
	Widgets []SurfaceWidgetRecord `json:"widgets"`
	History []SurfaceHistoryEntry `json:"history,omitempty"`
}

type SurfaceWidgetRecord struct {
	Widget      SurfaceWidgetFile  `json:"widget"`
	SourceLines []NumberedLine     `json:"source_lines"`
	Render      WidgetRenderResult `json:"render"`
}

type NumberedLine struct {
	Number int    `json:"number"`
	Text   string `json:"text"`
}

type WidgetRenderResult struct {
	SchemaOK         bool              `json:"schema_ok"`
	RenderOK         bool              `json:"render_ok"`
	NormalizedWidget *NormalizedWidget `json:"normalized_widget,omitempty"`
	PreviewText      string            `json:"preview_text,omitempty"`
	Errors           []string          `json:"errors,omitempty"`
}

type NormalizedWidget struct {
	ID            string           `json:"id"`
	Title         string           `json:"title"`
	Description   string           `json:"description,omitempty"`
	Kind          string           `json:"kind"`
	SchemaVersion string           `json:"schema_version"`
	Layout        map[string]any   `json:"layout,omitempty"`
	Checklist     []ChecklistItem  `json:"checklist,omitempty"`
	Table         *NormalizedTable `json:"table,omitempty"`
	Markdown      string           `json:"markdown,omitempty"`
}

type ChecklistItem struct {
	ID      string `json:"id,omitempty"`
	Label   string `json:"label"`
	Checked bool   `json:"checked"`
}

type NormalizedTable struct {
	Columns []TableColumn       `json:"columns"`
	Rows    []map[string]string `json:"rows"`
}

type TableColumn struct {
	Key   string `json:"key"`
	Label string `json:"label"`
}

type SurfaceHistoryEntry struct {
	ID        string `json:"id" yaml:"id"`
	SurfaceID string `json:"surface_id" yaml:"surface_id"`
	WidgetID  string `json:"widget_id,omitempty" yaml:"widget_id,omitempty"`
	Kind      string `json:"kind" yaml:"kind"`
	Actor     string `json:"actor,omitempty" yaml:"actor,omitempty"`
	Summary   string `json:"summary,omitempty" yaml:"summary,omitempty"`
	CreatedAt string `json:"created_at" yaml:"created_at"`
}

type WidgetPatchRequest struct {
	Actor       string `json:"actor,omitempty"`
	Mode        string `json:"mode,omitempty"`
	StartLine   int    `json:"start_line,omitempty"`
	EndLine     int    `json:"end_line,omitempty"`
	Replacement string `json:"replacement,omitempty"`
	Search      string `json:"search,omitempty"`
	Replace     string `json:"replace,omitempty"`
	Old         string `json:"old,omitempty"`
	New         string `json:"new,omitempty"`
	Snippet     string `json:"snippet,omitempty"`
}

func NewSurfaceStore(root string) *SurfaceStore {
	return &SurfaceStore{root: strings.TrimSpace(root)}
}

func brokerSurfacesRoot(statePath string) string {
	if p := strings.TrimSpace(os.Getenv("WUPHF_SURFACES_PATH")); p != "" {
		return p
	}
	if strings.TrimSpace(statePath) != "" {
		return filepath.Join(filepath.Dir(statePath), "surfaces")
	}
	home := config.RuntimeHomeDir()
	if home == "" {
		return filepath.Join(".wuphf", "team", "surfaces")
	}
	return filepath.Join(home, ".wuphf", "team", "surfaces")
}

func (b *Broker) surfaceStore() *SurfaceStore {
	return NewSurfaceStore(brokerSurfacesRoot(b.statePath))
}

func (s *SurfaceStore) ListSurfaces() ([]SurfaceManifest, error) {
	entries, err := os.ReadDir(s.root)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return []SurfaceManifest{}, nil
		}
		return nil, err
	}
	rows := make([]SurfaceManifest, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() || !surfaceIDValid(entry.Name()) {
			continue
		}
		manifest, err := s.ReadSurfaceManifest(entry.Name())
		if err != nil {
			continue
		}
		manifest.WidgetCount = s.widgetCount(entry.Name())
		rows = append(rows, manifest)
	}
	sort.SliceStable(rows, func(i, j int) bool {
		return rows[i].UpdatedAt > rows[j].UpdatedAt
	})
	if len(rows) > maxSurfacesReturned {
		rows = rows[:maxSurfacesReturned]
	}
	return rows, nil
}

func (s *SurfaceStore) CreateSurface(input SurfaceManifest) (SurfaceManifest, error) {
	surfaceFileMu.Lock()
	defer surfaceFileMu.Unlock()

	now := time.Now().UTC().Format(time.RFC3339)
	id := strings.TrimSpace(input.ID)
	if id == "" {
		id = slugFromTitle(input.Title, "surface")
	}
	if !surfaceIDValid(id) {
		return SurfaceManifest{}, fmt.Errorf("invalid surface id %q", id)
	}
	channel := normalizeChannelSlug(input.Channel)
	if channel == "" {
		channel = "general"
	}
	title := strings.TrimSpace(input.Title)
	if title == "" {
		title = id
	}
	manifest := SurfaceManifest{
		ID:          id,
		Title:       title,
		Channel:     channel,
		Owner:       strings.TrimSpace(input.Owner),
		CreatedBy:   strings.TrimSpace(input.CreatedBy),
		CreatedAt:   now,
		UpdatedAt:   now,
		Layout:      input.Layout,
		Permissions: input.Permissions,
	}
	dir := s.surfaceDir(id)
	if _, err := os.Stat(filepath.Join(dir, "surface.yaml")); err == nil {
		return SurfaceManifest{}, fmt.Errorf("surface %q already exists", id)
	}
	if err := os.MkdirAll(filepath.Join(dir, "widgets"), 0o700); err != nil {
		return SurfaceManifest{}, err
	}
	if err := os.MkdirAll(filepath.Join(dir, "history"), 0o700); err != nil {
		return SurfaceManifest{}, err
	}
	if err := s.writeYAML(filepath.Join(dir, "surface.yaml"), manifest); err != nil {
		return SurfaceManifest{}, err
	}
	_ = s.appendHistoryLocked(id, "", "surface_created", manifest.CreatedBy, "Created surface "+manifest.Title)
	return manifest, nil
}

func (s *SurfaceStore) ReadSurfaceManifest(id string) (SurfaceManifest, error) {
	if !surfaceIDValid(id) {
		return SurfaceManifest{}, fmt.Errorf("invalid surface id %q", id)
	}
	var manifest SurfaceManifest
	if err := s.readYAML(filepath.Join(s.surfaceDir(id), "surface.yaml"), &manifest); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return SurfaceManifest{}, errSurfaceNotFound
		}
		return SurfaceManifest{}, err
	}
	manifest.ID = strings.TrimSpace(manifest.ID)
	manifest.Channel = normalizeChannelSlug(manifest.Channel)
	if manifest.Channel == "" {
		manifest.Channel = "general"
	}
	manifest.WidgetCount = s.widgetCount(id)
	return manifest, nil
}

func (s *SurfaceStore) ReadSurface(id string) (SurfaceDetail, error) {
	manifest, err := s.ReadSurfaceManifest(id)
	if err != nil {
		return SurfaceDetail{}, err
	}
	widgets, err := s.ListWidgetRecords(id)
	if err != nil {
		return SurfaceDetail{}, err
	}
	history, _ := s.ListHistory(id)
	return SurfaceDetail{Surface: manifest, Widgets: widgets, History: history}, nil
}

func (s *SurfaceStore) UpdateSurface(input SurfaceManifest, actor string) (SurfaceManifest, error) {
	surfaceFileMu.Lock()
	defer surfaceFileMu.Unlock()
	current, err := s.ReadSurfaceManifest(input.ID)
	if err != nil {
		return SurfaceManifest{}, err
	}
	if title := strings.TrimSpace(input.Title); title != "" {
		current.Title = title
	}
	if input.Layout != nil {
		current.Layout = input.Layout
	}
	if input.Permissions != nil {
		current.Permissions = input.Permissions
	}
	current.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	if err := s.writeYAML(filepath.Join(s.surfaceDir(current.ID), "surface.yaml"), current); err != nil {
		return SurfaceManifest{}, err
	}
	_ = s.appendHistoryLocked(current.ID, "", "surface_updated", actor, "Updated surface "+current.Title)
	return current, nil
}

func (s *SurfaceStore) DeleteSurface(id, actor string) error {
	if !surfaceIDValid(id) {
		return fmt.Errorf("invalid surface id %q", id)
	}
	surfaceFileMu.Lock()
	defer surfaceFileMu.Unlock()
	if _, err := s.ReadSurfaceManifest(id); err != nil {
		return err
	}
	_ = s.appendHistoryLocked(id, "", "surface_deleted", actor, "Deleted surface "+id)
	return os.RemoveAll(s.surfaceDir(id))
}

func (s *SurfaceStore) ListWidgetRecords(surfaceID string) ([]SurfaceWidgetRecord, error) {
	if !surfaceIDValid(surfaceID) {
		return nil, fmt.Errorf("invalid surface id %q", surfaceID)
	}
	widgetDir := filepath.Join(s.surfaceDir(surfaceID), "widgets")
	entries, err := os.ReadDir(widgetDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return []SurfaceWidgetRecord{}, nil
		}
		return nil, err
	}
	rows := make([]SurfaceWidgetRecord, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".yaml") {
			continue
		}
		id := strings.TrimSuffix(entry.Name(), ".yaml")
		if !surfaceIDValid(id) {
			continue
		}
		record, err := s.ReadWidget(surfaceID, id)
		if err != nil {
			continue
		}
		rows = append(rows, record)
	}
	sort.SliceStable(rows, func(i, j int) bool {
		return rows[i].Widget.UpdatedAt < rows[j].Widget.UpdatedAt
	})
	return rows, nil
}

func (s *SurfaceStore) ReadWidget(surfaceID, widgetID string) (SurfaceWidgetRecord, error) {
	if !surfaceIDValid(surfaceID) {
		return SurfaceWidgetRecord{}, fmt.Errorf("invalid surface id %q", surfaceID)
	}
	if !surfaceIDValid(widgetID) {
		return SurfaceWidgetRecord{}, fmt.Errorf("invalid widget id %q", widgetID)
	}
	var widget SurfaceWidgetFile
	if err := s.readYAML(s.widgetPath(surfaceID, widgetID), &widget); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return SurfaceWidgetRecord{}, errWidgetNotFound
		}
		return SurfaceWidgetRecord{}, err
	}
	widget.ID = widgetID
	render := RenderWidget(widget)
	return SurfaceWidgetRecord{
		Widget:      widget,
		SourceLines: numberedLines(widget.Source),
		Render:      render,
	}, nil
}

func (s *SurfaceStore) UpsertWidget(surfaceID string, widget SurfaceWidgetFile, actor string) (SurfaceWidgetRecord, error) {
	if !surfaceIDValid(surfaceID) {
		return SurfaceWidgetRecord{}, fmt.Errorf("invalid surface id %q", surfaceID)
	}
	if _, err := s.ReadSurfaceManifest(surfaceID); err != nil {
		return SurfaceWidgetRecord{}, err
	}
	surfaceFileMu.Lock()
	defer surfaceFileMu.Unlock()
	return s.upsertWidgetLocked(surfaceID, widget, actor, "widget_upserted")
}

func (s *SurfaceStore) PatchWidget(surfaceID, widgetID string, patch WidgetPatchRequest) (SurfaceWidgetRecord, error) {
	if !surfaceIDValid(surfaceID) {
		return SurfaceWidgetRecord{}, fmt.Errorf("invalid surface id %q", surfaceID)
	}
	if !surfaceIDValid(widgetID) {
		return SurfaceWidgetRecord{}, fmt.Errorf("invalid widget id %q", widgetID)
	}
	surfaceFileMu.Lock()
	defer surfaceFileMu.Unlock()
	current, err := s.readWidgetFile(surfaceID, widgetID)
	if err != nil {
		return SurfaceWidgetRecord{}, err
	}
	source, err := applyWidgetSourcePatch(current.Source, patch)
	if err != nil {
		return SurfaceWidgetRecord{}, err
	}
	current.Source = source
	return s.upsertWidgetLocked(surfaceID, current, patch.Actor, "widget_patched")
}

func (s *SurfaceStore) RenderCheck(surfaceID, widgetID string, candidate *SurfaceWidgetFile) (WidgetRenderResult, error) {
	if candidate != nil {
		return RenderWidget(*candidate), nil
	}
	record, err := s.ReadWidget(surfaceID, widgetID)
	if err != nil {
		return WidgetRenderResult{}, err
	}
	return record.Render, nil
}

func (s *SurfaceStore) upsertWidgetLocked(surfaceID string, widget SurfaceWidgetFile, actor, historyKind string) (SurfaceWidgetRecord, error) {
	now := time.Now().UTC().Format(time.RFC3339)
	id := strings.TrimSpace(widget.ID)
	if id == "" {
		id = slugFromTitle(widget.Title, "widget")
	}
	if !surfaceIDValid(id) {
		return SurfaceWidgetRecord{}, fmt.Errorf("invalid widget id %q", id)
	}
	if len([]byte(widget.Source)) > maxWidgetSourceBytes {
		return SurfaceWidgetRecord{}, fmt.Errorf("widget source exceeds %d bytes", maxWidgetSourceBytes)
	}
	widget.ID = id
	widget.Kind = strings.TrimSpace(strings.ToLower(widget.Kind))
	widget.Title = strings.TrimSpace(widget.Title)
	if widget.Title == "" {
		widget.Title = id
	}
	if widget.SchemaVersion == "" {
		widget.SchemaVersion = "surface.widget.v1"
	}
	if widget.CreatedAt == "" {
		if existing, err := s.readWidgetFile(surfaceID, id); err == nil {
			widget.CreatedAt = existing.CreatedAt
			widget.CreatedBy = existing.CreatedBy
		}
	}
	if widget.CreatedAt == "" {
		widget.CreatedAt = now
	}
	if widget.CreatedBy == "" {
		widget.CreatedBy = strings.TrimSpace(actor)
	}
	widget.UpdatedAt = now
	widget.UpdatedBy = strings.TrimSpace(actor)
	render := RenderWidget(widget)
	if !render.SchemaOK || !render.RenderOK {
		return SurfaceWidgetRecord{}, fmt.Errorf("widget render check failed: %s", strings.Join(render.Errors, "; "))
	}
	widgets, _ := s.ListWidgetRecords(surfaceID)
	if _, err := s.readWidgetFile(surfaceID, id); errors.Is(err, errWidgetNotFound) && len(widgets) >= maxWidgetsPerSurface {
		return SurfaceWidgetRecord{}, fmt.Errorf("surface %q already has maximum %d widgets", surfaceID, maxWidgetsPerSurface)
	}
	if err := os.MkdirAll(filepath.Join(s.surfaceDir(surfaceID), "widgets"), 0o700); err != nil {
		return SurfaceWidgetRecord{}, err
	}
	if err := s.writeYAML(s.widgetPath(surfaceID, id), widget); err != nil {
		return SurfaceWidgetRecord{}, err
	}
	if manifest, err := s.ReadSurfaceManifest(surfaceID); err == nil {
		manifest.UpdatedAt = now
		_ = s.writeYAML(filepath.Join(s.surfaceDir(surfaceID), "surface.yaml"), manifest)
	}
	_ = s.appendHistoryLocked(surfaceID, id, historyKind, actor, "Updated widget "+widget.Title)
	return SurfaceWidgetRecord{Widget: widget, SourceLines: numberedLines(widget.Source), Render: render}, nil
}

func (s *SurfaceStore) readWidgetFile(surfaceID, widgetID string) (SurfaceWidgetFile, error) {
	var widget SurfaceWidgetFile
	if err := s.readYAML(s.widgetPath(surfaceID, widgetID), &widget); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return SurfaceWidgetFile{}, errWidgetNotFound
		}
		return SurfaceWidgetFile{}, err
	}
	widget.ID = widgetID
	return widget, nil
}

func RenderWidget(widget SurfaceWidgetFile) WidgetRenderResult {
	normalized, preview, errs := normalizeWidget(widget)
	if len(errs) > 0 {
		return WidgetRenderResult{SchemaOK: false, RenderOK: false, Errors: errs}
	}
	return WidgetRenderResult{
		SchemaOK:         true,
		RenderOK:         true,
		NormalizedWidget: &normalized,
		PreviewText:      truncateBytes(preview, maxPreviewTextBytes),
	}
}

func normalizeWidget(widget SurfaceWidgetFile) (NormalizedWidget, string, []string) {
	var errs []string
	if strings.TrimSpace(widget.ID) == "" {
		errs = append(errs, "widget id is required")
	}
	if strings.TrimSpace(widget.Title) == "" {
		errs = append(errs, "widget title is required")
	}
	if strings.TrimSpace(widget.Source) == "" {
		errs = append(errs, "widget source is required")
	}
	if len([]byte(widget.Source)) > maxWidgetSourceBytes {
		errs = append(errs, fmt.Sprintf("widget source exceeds %d bytes", maxWidgetSourceBytes))
	}
	var raw map[string]any
	if strings.TrimSpace(widget.Source) != "" {
		if err := yaml.Unmarshal([]byte(widget.Source), &raw); err != nil {
			errs = append(errs, "source yaml invalid: "+err.Error())
		}
	}
	kind := strings.ToLower(strings.TrimSpace(widget.Kind))
	if sourceKind, _ := rawString(raw, "kind"); sourceKind != "" {
		kind = strings.ToLower(sourceKind)
	}
	if kind == "" {
		errs = append(errs, "widget kind is required")
	}
	normalized := NormalizedWidget{
		ID:            strings.TrimSpace(widget.ID),
		Title:         strings.TrimSpace(widget.Title),
		Description:   strings.TrimSpace(widget.Description),
		Kind:          kind,
		SchemaVersion: fallbackString(widget.SchemaVersion, "surface.widget.v1"),
		Layout:        widget.Layout,
	}
	if len(errs) > 0 {
		return normalized, "", errs
	}
	switch kind {
	case "checklist":
		items, itemErrs := normalizeChecklist(raw)
		if len(itemErrs) > 0 {
			errs = append(errs, itemErrs...)
			break
		}
		normalized.Checklist = items
		var lines []string
		for _, item := range items {
			mark := "[ ]"
			if item.Checked {
				mark = "[x]"
			}
			lines = append(lines, fmt.Sprintf("%s %s", mark, item.Label))
		}
		return normalized, strings.Join(lines, "\n"), nil
	case "table":
		table, tableErrs := normalizeTable(raw)
		if len(tableErrs) > 0 {
			errs = append(errs, tableErrs...)
			break
		}
		normalized.Table = &table
		return normalized, previewTable(table), nil
	case "markdown":
		body := firstRawString(raw, "markdown", "body", "text", "content")
		if strings.TrimSpace(body) == "" {
			errs = append(errs, "markdown widget requires markdown, body, text, or content")
			break
		}
		normalized.Markdown = body
		return normalized, body, nil
	default:
		errs = append(errs, fmt.Sprintf("unsupported widget kind %q", kind))
	}
	return normalized, "", errs
}

func normalizeChecklist(raw map[string]any) ([]ChecklistItem, []string) {
	itemsRaw, ok := raw["items"].([]any)
	if !ok || len(itemsRaw) == 0 {
		return nil, []string{"checklist widget requires non-empty items"}
	}
	items := make([]ChecklistItem, 0, len(itemsRaw))
	for i, itemRaw := range itemsRaw {
		m, ok := itemRaw.(map[string]any)
		if !ok {
			if label, ok := itemRaw.(string); ok && strings.TrimSpace(label) != "" {
				items = append(items, ChecklistItem{ID: fmt.Sprintf("item-%d", i+1), Label: strings.TrimSpace(label)})
				continue
			}
			return nil, []string{fmt.Sprintf("checklist item %d must be an object or string", i+1)}
		}
		label := firstRawString(m, "label", "title", "text")
		if strings.TrimSpace(label) == "" {
			return nil, []string{fmt.Sprintf("checklist item %d requires label", i+1)}
		}
		item := ChecklistItem{
			ID:      fallbackString(firstRawString(m, "id"), fmt.Sprintf("item-%d", i+1)),
			Label:   strings.TrimSpace(label),
			Checked: rawBool(m, "checked"),
		}
		items = append(items, item)
	}
	return items, nil
}

func normalizeTable(raw map[string]any) (NormalizedTable, []string) {
	columnsRaw, ok := raw["columns"].([]any)
	if !ok || len(columnsRaw) == 0 {
		return NormalizedTable{}, []string{"table widget requires non-empty columns"}
	}
	if len(columnsRaw) > maxTableColumnsRendered {
		return NormalizedTable{}, []string{fmt.Sprintf("table widget exceeds %d columns", maxTableColumnsRendered)}
	}
	columns := make([]TableColumn, 0, len(columnsRaw))
	for i, colRaw := range columnsRaw {
		switch col := colRaw.(type) {
		case string:
			key := strings.TrimSpace(col)
			if key == "" {
				return NormalizedTable{}, []string{fmt.Sprintf("table column %d is blank", i+1)}
			}
			columns = append(columns, TableColumn{Key: key, Label: key})
		case map[string]any:
			key := firstRawString(col, "key", "id")
			label := fallbackString(firstRawString(col, "label", "title"), key)
			if strings.TrimSpace(key) == "" {
				return NormalizedTable{}, []string{fmt.Sprintf("table column %d requires key", i+1)}
			}
			columns = append(columns, TableColumn{Key: strings.TrimSpace(key), Label: strings.TrimSpace(label)})
		default:
			return NormalizedTable{}, []string{fmt.Sprintf("table column %d must be a string or object", i+1)}
		}
	}
	rowsRaw, ok := raw["rows"].([]any)
	if !ok {
		return NormalizedTable{}, []string{"table widget requires rows"}
	}
	if len(rowsRaw) > maxTableRowsRendered {
		return NormalizedTable{}, []string{fmt.Sprintf("table widget exceeds %d rows", maxTableRowsRendered)}
	}
	rows := make([]map[string]string, 0, len(rowsRaw))
	for i, rowRaw := range rowsRaw {
		m, ok := rowRaw.(map[string]any)
		if !ok {
			return NormalizedTable{}, []string{fmt.Sprintf("table row %d must be an object", i+1)}
		}
		row := map[string]string{}
		for _, col := range columns {
			row[col.Key] = strings.TrimSpace(fmt.Sprint(m[col.Key]))
		}
		rows = append(rows, row)
	}
	return NormalizedTable{Columns: columns, Rows: rows}, nil
}

func previewTable(table NormalizedTable) string {
	var lines []string
	var header []string
	for _, col := range table.Columns {
		header = append(header, col.Label)
	}
	lines = append(lines, strings.Join(header, " | "))
	for _, row := range table.Rows {
		var cells []string
		for _, col := range table.Columns {
			cells = append(cells, row[col.Key])
		}
		lines = append(lines, strings.Join(cells, " | "))
	}
	return strings.Join(lines, "\n")
}

func applyWidgetSourcePatch(source string, patch WidgetPatchRequest) (string, error) {
	mode := strings.ToLower(strings.TrimSpace(patch.Mode))
	if mode == "" {
		if patch.StartLine > 0 || patch.EndLine > 0 {
			mode = "line"
		} else {
			mode = "snippet"
		}
	}
	replacement := patch.Replacement
	if replacement == "" {
		replacement = patch.New
	}
	if replacement == "" {
		replacement = patch.Replace
	}
	switch mode {
	case "line", "lines":
		if patch.StartLine <= 0 || patch.EndLine <= 0 || patch.EndLine < patch.StartLine {
			return "", fmt.Errorf("line patch requires valid start_line and end_line")
		}
		hadTrailingNewline := strings.HasSuffix(source, "\n")
		lines := splitSourceLines(source)
		if patch.StartLine > len(lines) || patch.EndLine > len(lines) {
			return "", fmt.Errorf("line patch range %d-%d outside source with %d lines", patch.StartLine, patch.EndLine, len(lines))
		}
		replacementLines := splitSourceLines(replacement)
		out := append([]string{}, lines[:patch.StartLine-1]...)
		out = append(out, replacementLines...)
		out = append(out, lines[patch.EndLine:]...)
		joined := strings.Join(out, "\n")
		if hadTrailingNewline {
			joined += "\n"
		}
		return joined, nil
	case "snippet":
		search := patch.Search
		if search == "" {
			search = patch.Old
		}
		if search == "" {
			search = patch.Snippet
		}
		if search == "" {
			return "", fmt.Errorf("snippet patch requires search, old, or snippet")
		}
		count := strings.Count(source, search)
		if count == 0 {
			return "", fmt.Errorf("snippet not found")
		}
		if count > 1 {
			return "", fmt.Errorf("snippet matched %d places; patch is ambiguous", count)
		}
		return strings.Replace(source, search, replacement, 1), nil
	default:
		return "", fmt.Errorf("unsupported patch mode %q", mode)
	}
}

func (s *SurfaceStore) ListHistory(surfaceID string) ([]SurfaceHistoryEntry, error) {
	if !surfaceIDValid(surfaceID) {
		return nil, fmt.Errorf("invalid surface id %q", surfaceID)
	}
	dir := filepath.Join(s.surfaceDir(surfaceID), "history")
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return []SurfaceHistoryEntry{}, nil
		}
		return nil, err
	}
	sort.SliceStable(entries, func(i, j int) bool { return entries[i].Name() > entries[j].Name() })
	if len(entries) > maxHistoryEntriesRead {
		entries = entries[:maxHistoryEntriesRead]
	}
	out := make([]SurfaceHistoryEntry, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".yaml") {
			continue
		}
		var h SurfaceHistoryEntry
		if err := s.readYAML(filepath.Join(dir, entry.Name()), &h); err == nil {
			out = append(out, h)
		}
	}
	return out, nil
}

func (s *SurfaceStore) appendHistoryLocked(surfaceID, widgetID, kind, actor, summary string) error {
	now := time.Now().UTC()
	id := fmt.Sprintf("%s-%s", now.Format("20060102T150405.000000000Z"), slugFromTitle(kind, "event"))
	entry := SurfaceHistoryEntry{
		ID:        id,
		SurfaceID: surfaceID,
		WidgetID:  widgetID,
		Kind:      strings.TrimSpace(kind),
		Actor:     strings.TrimSpace(actor),
		Summary:   strings.TrimSpace(summary),
		CreatedAt: now.Format(time.RFC3339),
	}
	dir := filepath.Join(s.surfaceDir(surfaceID), "history")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	return s.writeYAML(filepath.Join(dir, id+".yaml"), entry)
}

func (s *SurfaceStore) surfaceDir(id string) string {
	return filepath.Join(s.root, id)
}

func (s *SurfaceStore) widgetPath(surfaceID, widgetID string) string {
	return filepath.Join(s.surfaceDir(surfaceID), "widgets", widgetID+".yaml")
}

func (s *SurfaceStore) widgetCount(surfaceID string) int {
	entries, err := os.ReadDir(filepath.Join(s.surfaceDir(surfaceID), "widgets"))
	if err != nil {
		return 0
	}
	n := 0
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".yaml") {
			n++
		}
	}
	return n
}

func (s *SurfaceStore) readYAML(path string, dest any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return yaml.Unmarshal(data, dest)
}

func (s *SurfaceStore) writeYAML(path string, value any) error {
	data, err := yaml.Marshal(value)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return atomicWriteFile(path, data)
}

func surfaceIDValid(id string) bool {
	if len(id) > 63 {
		return false
	}
	return slugPattern.MatchString(strings.TrimSpace(id))
}

func slugFromTitle(title, fallback string) string {
	title = strings.ToLower(strings.TrimSpace(title))
	var b strings.Builder
	lastDash := false
	for _, r := range title {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			lastDash = false
		case unicode.IsSpace(r) || r == '-' || r == '_' || r == '/':
			if b.Len() > 0 && !lastDash {
				b.WriteByte('-')
				lastDash = true
			}
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		out = fallback
	}
	if len(out) > 63 {
		out = strings.Trim(out[:63], "-")
	}
	if out == "" {
		return fallback
	}
	return out
}

func numberedLines(source string) []NumberedLine {
	lines := splitSourceLines(source)
	out := make([]NumberedLine, 0, len(lines))
	for i, line := range lines {
		out = append(out, NumberedLine{Number: i + 1, Text: line})
	}
	return out
}

func splitSourceLines(source string) []string {
	source = strings.TrimRight(source, "\n")
	if source == "" {
		return []string{}
	}
	return strings.Split(source, "\n")
}

func rawString(m map[string]any, key string) (string, bool) {
	if m == nil {
		return "", false
	}
	v, ok := m[key]
	if !ok {
		return "", false
	}
	s, ok := v.(string)
	if !ok {
		return strings.TrimSpace(fmt.Sprint(v)), true
	}
	return strings.TrimSpace(s), true
}

func firstRawString(m map[string]any, keys ...string) string {
	for _, key := range keys {
		if v, ok := rawString(m, key); ok && strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func rawBool(m map[string]any, key string) bool {
	if m == nil {
		return false
	}
	switch v := m[key].(type) {
	case bool:
		return v
	case string:
		return strings.EqualFold(strings.TrimSpace(v), "true") || strings.EqualFold(strings.TrimSpace(v), "yes")
	default:
		return false
	}
}

func fallbackString(value, fallback string) string {
	if strings.TrimSpace(value) != "" {
		return strings.TrimSpace(value)
	}
	return fallback
}

func truncateBytes(value string, max int) string {
	if len([]byte(value)) <= max {
		return value
	}
	if max <= 0 {
		return ""
	}
	data := []byte(value)
	if len(data) > max {
		data = data[:max]
	}
	return strings.TrimRight(string(data), "\x00") + "\n[truncated]"
}
