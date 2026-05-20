package team

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func newEmbedFixture(t *testing.T) *Repo {
	t.Helper()
	t.Setenv("WUPHF_RUNTIME_HOME", t.TempDir())
	root := filepath.Join(t.TempDir(), "wiki")
	backup := filepath.Join(t.TempDir(), "wiki.bak")
	repo := NewRepoAt(root, backup)
	if err := repo.Init(context.Background()); err != nil {
		t.Fatalf("init repo: %v", err)
	}
	return repo
}

func TestIngestImageEmbeds_MovesBareFilenameIntoInboxRaw(t *testing.T) {
	repo := newEmbedFixture(t)
	briefRel := "team/people/sarah.md"
	briefDir := filepath.Dir(filepath.Join(repo.Root(), filepath.FromSlash(briefRel)))
	if err := os.MkdirAll(briefDir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	imgPath := filepath.Join(briefDir, "diagram.png")
	if err := os.WriteFile(imgPath, []byte("PNGDATA"), 0o600); err != nil {
		t.Fatalf("write png: %v", err)
	}

	body := "See ![[diagram.png]] for the architecture.\n"
	got, ingested, err := IngestImageEmbeds(repo, briefRel, body)
	if err != nil {
		t.Fatalf("ingest: %v", err)
	}
	if !strings.Contains(got, "![[inbox/raw/diagram.png]]") {
		t.Fatalf("body not rewritten: %q", got)
	}
	if len(ingested) != 1 || ingested[0] != "inbox/raw/diagram.png" {
		t.Fatalf("unexpected ingested list: %#v", ingested)
	}
	dst := filepath.Join(repo.Root(), "team", "inbox", "raw", "diagram.png")
	if _, err := os.Stat(dst); err != nil {
		t.Fatalf("expected png at %s: %v", dst, err)
	}
	if _, err := os.Stat(imgPath); !os.IsNotExist(err) {
		t.Fatalf("expected original path removed; got err=%v", err)
	}
}

func TestIngestImageEmbeds_LeavesAlreadyCanonical(t *testing.T) {
	repo := newEmbedFixture(t)
	body := "Already canonical: ![[inbox/raw/foo.png]]\n"
	got, ingested, err := IngestImageEmbeds(repo, "team/people/sarah.md", body)
	if err != nil {
		t.Fatalf("ingest: %v", err)
	}
	if got != body {
		t.Fatalf("body mutated: %q", got)
	}
	if len(ingested) != 0 {
		t.Fatalf("expected nothing ingested; got %#v", ingested)
	}
}

func TestIngestImageEmbeds_LeavesBrokenReferenceInPlace(t *testing.T) {
	repo := newEmbedFixture(t)
	body := "Missing: ![[ghost.png]]\n"
	got, ingested, err := IngestImageEmbeds(repo, "team/people/sarah.md", body)
	if err != nil {
		t.Fatalf("ingest: %v", err)
	}
	if got != body {
		t.Fatalf("body mutated for broken ref: %q", got)
	}
	if len(ingested) != 0 {
		t.Fatalf("expected nothing ingested; got %#v", ingested)
	}
}

func TestIngestImageEmbeds_SkipsNonBriefPath(t *testing.T) {
	repo := newEmbedFixture(t)
	body := "Has ![[anything.png]] but path is wrong"
	got, ingested, err := IngestImageEmbeds(repo, "team/inbox/raw/something.md", body)
	if err != nil {
		t.Fatalf("ingest: %v", err)
	}
	if got != body || len(ingested) != 0 {
		t.Fatalf("expected no-op for non-brief path; got %q ingested=%#v", got, ingested)
	}
}

func TestIngestImageEmbeds_PreservesAltText(t *testing.T) {
	repo := newEmbedFixture(t)
	briefRel := "team/companies/acme.md"
	briefDir := filepath.Dir(filepath.Join(repo.Root(), filepath.FromSlash(briefRel)))
	if err := os.MkdirAll(briefDir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	imgPath := filepath.Join(briefDir, "logo.png")
	if err := os.WriteFile(imgPath, []byte("LOGO"), 0o600); err != nil {
		t.Fatalf("write png: %v", err)
	}
	body := "Logo ![[logo.png|Acme Corp logo]]"
	got, _, err := IngestImageEmbeds(repo, briefRel, body)
	if err != nil {
		t.Fatalf("ingest: %v", err)
	}
	want := "Logo ![[inbox/raw/logo.png|Acme Corp logo]]"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestIngestImageEmbeds_PathWithSlashIsSkipped(t *testing.T) {
	repo := newEmbedFixture(t)
	body := "![[some/nested/path.png]]"
	got, ingested, err := IngestImageEmbeds(repo, "team/people/sarah.md", body)
	if err != nil {
		t.Fatalf("ingest: %v", err)
	}
	if got != body || len(ingested) != 0 {
		t.Fatalf("expected no-op for nested-path embed; got %q ingested=%#v", got, ingested)
	}
}
