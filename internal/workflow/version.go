package workflow

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// VersionManifest tracks the active version and version count for a workflow.
type VersionManifest struct {
	Key           string `json:"key"`
	ActiveVersion int    `json:"active_version"`
	VersionCount  int    `json:"version_count"`
	CreatedAt     string `json:"created_at"`
	UpdatedAt     string `json:"updated_at"`
}

// versionedDir returns the directory where versioned specs are stored.
func versionedDir() string {
	return filepath.Join(effectiveBasePath(), "interactive")
}

// versionFilePath returns the path for a specific version of a workflow spec.
func versionFilePath(key string, version int) string {
	return filepath.Join(versionedDir(), fmt.Sprintf("%s.v%d.json", sanitizeKey(key), version))
}

// manifestFilePath returns the path for a workflow's version manifest.
func manifestFilePath(key string) string {
	return filepath.Join(versionedDir(), sanitizeKey(key)+".manifest.json")
}

// SaveVersion saves a new version of a workflow spec. Returns the version number.
func SaveVersion(key string, spec WorkflowSpec) (int, error) {
	dir := versionedDir()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return 0, fmt.Errorf("create version dir: %w", err)
	}

	// Load or create manifest.
	manifest, err := loadManifest(key)
	if err != nil {
		return 0, err
	}

	now := time.Now().UTC().Format(time.RFC3339)
	newVersion := manifest.VersionCount + 1

	if manifest.CreatedAt == "" {
		manifest.CreatedAt = now
	}
	manifest.Key = sanitizeKey(key)
	manifest.ActiveVersion = newVersion
	manifest.VersionCount = newVersion
	manifest.UpdatedAt = now

	// Write the versioned spec file.
	specData, err := json.MarshalIndent(spec, "", "  ")
	if err != nil {
		return 0, fmt.Errorf("marshal spec: %w", err)
	}
	specData = append(specData, '\n')
	specPath := versionFilePath(key, newVersion)
	if err := os.WriteFile(specPath, specData, 0o600); err != nil {
		return 0, fmt.Errorf("write version file: %w", err)
	}

	// Write the manifest.
	if err := saveManifest(key, manifest); err != nil {
		return 0, err
	}

	return newVersion, nil
}

// LoadVersion loads a specific version of a workflow spec.
// Pass 0 for the latest (active) version.
func LoadVersion(key string, version int) (*WorkflowSpec, int, error) {
	manifest, err := loadManifest(key)
	if err != nil {
		return nil, 0, err
	}
	if manifest.VersionCount == 0 {
		return nil, 0, fmt.Errorf("no versions found for workflow %q", key)
	}

	targetVersion := version
	if targetVersion == 0 {
		targetVersion = manifest.ActiveVersion
	}
	if targetVersion < 1 || targetVersion > manifest.VersionCount {
		return nil, 0, fmt.Errorf("version %d out of range [1, %d] for workflow %q", targetVersion, manifest.VersionCount, key)
	}

	path := versionFilePath(key, targetVersion)
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, 0, fmt.Errorf("read version %d: %w", targetVersion, err)
	}

	var spec WorkflowSpec
	if err := json.Unmarshal(data, &spec); err != nil {
		return nil, 0, fmt.Errorf("unmarshal version %d: %w", targetVersion, err)
	}

	return &spec, targetVersion, nil
}

// ListVersions returns the manifest for a workflow.
// Returns nil with no error if the workflow has never been versioned.
func ListVersions(key string) (*VersionManifest, error) {
	manifest, err := loadManifest(key)
	if err != nil {
		return nil, err
	}
	if manifest.VersionCount == 0 {
		return nil, nil
	}
	return &manifest, nil
}

// loadManifest reads the manifest file, returning a zero-value manifest if missing.
func loadManifest(key string) (VersionManifest, error) {
	path := manifestFilePath(key)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return VersionManifest{}, nil
		}
		return VersionManifest{}, fmt.Errorf("read manifest: %w", err)
	}
	var m VersionManifest
	if err := json.Unmarshal(data, &m); err != nil {
		return VersionManifest{}, fmt.Errorf("unmarshal manifest: %w", err)
	}
	return m, nil
}

// saveManifest writes the manifest file.
func saveManifest(key string, manifest VersionManifest) error {
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal manifest: %w", err)
	}
	data = append(data, '\n')
	path := manifestFilePath(key)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("write manifest: %w", err)
	}
	return nil
}
