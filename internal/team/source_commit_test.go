package team

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// newSourceCommitRepo returns an initialized Repo rooted at a tempdir.
func newSourceCommitRepo(t *testing.T) *Repo {
	t.Helper()
	root := filepath.Join(t.TempDir(), "wiki")
	backup := filepath.Join(t.TempDir(), "wiki.bak")
	repo := NewRepoAt(root, backup)
	if err := repo.Init(context.Background()); err != nil {
		t.Fatalf("init: %v", err)
	}
	return repo
}

func renderSourceForTest(t *testing.T, kind SourceKind, origin, title, content string) (SourceRecord, string) {
	t.Helper()
	id := DeriveSourceID(kind, origin, title, content)
	rec, err := NewSourceRecord(id, kind, title, origin, content, time.Now().UTC())
	if err != nil {
		t.Fatalf("NewSourceRecord: %v", err)
	}
	body, err := RenderSourceMarkdown(rec)
	if err != nil {
		t.Fatalf("RenderSourceMarkdown: %v", err)
	}
	return rec, string(body)
}

func TestCommitSource_PathValidation(t *testing.T) {
	repo := newSourceCommitRepo(t)
	ctx := context.Background()

	tests := []struct {
		name    string
		relPath string
		content string
		wantErr string
	}{
		{
			name:    "rejects traversal",
			relPath: "sources/note/../../etc/passwd.md",
			content: "x",
			wantErr: "traversal",
		},
		{
			name:    "rejects non-source path",
			relPath: "team/people/nazz.md",
			content: "x",
			wantErr: "sources/{kind}/{id}.md",
		},
		{
			name:    "rejects non-md path",
			relPath: "sources/note/foo.txt",
			content: "x",
			wantErr: "sources/{kind}/{id}.md",
		},
		{
			name:    "rejects empty content",
			relPath: "sources/note/note-foo.md",
			content: "   ",
			wantErr: "content is required",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := repo.CommitSource(ctx, tc.relPath, tc.content, "msg")
			if err == nil {
				t.Fatalf("expected error, got nil")
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("error %q does not contain %q", err.Error(), tc.wantErr)
			}
		})
	}
}

func TestCommitSource_WriteThenIdempotentNoOp(t *testing.T) {
	repo := newSourceCommitRepo(t)
	ctx := context.Background()

	rec, body := renderSourceForTest(t, SourceKindNote, "", "Launch retro", "We shipped the wiki.")
	relPath := rec.RelPath()

	sha1, n1, err := repo.CommitSource(ctx, relPath, body, "")
	if err != nil {
		t.Fatalf("first commit: %v", err)
	}
	if sha1 == "" {
		t.Fatal("expected non-empty sha on first commit")
	}
	if n1 != len(body) {
		t.Fatalf("bytes written = %d, want %d", n1, len(body))
	}

	// File landed on disk with the rendered bytes.
	full := filepath.Join(repo.Root(), filepath.FromSlash(relPath))
	onDisk, err := os.ReadFile(full)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if string(onDisk) != body {
		t.Fatalf("on-disk content mismatch:\n got %q\nwant %q", onDisk, body)
	}

	// Second commit of the SAME path with DIFFERENT bytes is a write-once
	// no-op: the original content is preserved and HEAD is returned unchanged.
	sha2, n2, err := repo.CommitSource(ctx, relPath, body+"\n\nTAMPERED", "")
	if err != nil {
		t.Fatalf("second commit: %v", err)
	}
	if sha2 != sha1 {
		t.Fatalf("expected no-op to return same HEAD %q, got %q", sha1, sha2)
	}
	if n2 != len(body+"\n\nTAMPERED") {
		t.Fatalf("no-op bytes = %d, want %d", n2, len(body+"\n\nTAMPERED"))
	}
	after, err := os.ReadFile(full)
	if err != nil {
		t.Fatalf("read back after no-op: %v", err)
	}
	if string(after) != body {
		t.Fatalf("write-once violated: content changed on second commit:\n%q", after)
	}
}

func TestCommitSource_DistinctSourcesEachCommit(t *testing.T) {
	repo := newSourceCommitRepo(t)
	ctx := context.Background()

	recA, bodyA := renderSourceForTest(t, SourceKindDoc, "", "Doc A", "alpha")
	recB, bodyB := renderSourceForTest(t, SourceKindDoc, "", "Doc B", "beta")

	shaA, _, err := repo.CommitSource(ctx, recA.RelPath(), bodyA, "")
	if err != nil {
		t.Fatalf("commit A: %v", err)
	}
	shaB, _, err := repo.CommitSource(ctx, recB.RelPath(), bodyB, "")
	if err != nil {
		t.Fatalf("commit B: %v", err)
	}
	if shaA == shaB {
		t.Fatalf("distinct sources should produce distinct commits; both = %q", shaA)
	}
}
