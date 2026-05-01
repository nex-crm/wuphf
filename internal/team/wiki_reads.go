package team

// wiki_reads.go tracks who has accessed wiki articles and when.
//
// Reads are stored in ~/.wuphf/wiki/.reads/reads.jsonl — one JSON line per
// access event. This file is intentionally NOT inside the git-tracked wiki
// repo and does not go through the WikiWorker queue. It is ephemeral telemetry,
// not canonical content.
//
// Both human (web UI) and agent (MCP team_wiki_read) accesses are tracked.
// An article accessed by agents only is still valuable — it is being consumed.
// Staleness means "not accessed by anyone (human or agent)".
//
// Concurrency: ReadLog uses a sync.Mutex. The broker is a single process;
// no cross-process locking is needed.

import (
	"bufio"
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// ReaderHuman is the reader identifier used when a human opens an article in
// the web UI. Any other non-empty reader value is treated as an agent slug.
const ReaderHuman = "web"

// CatalogSortLastRead is the sort key accepted by BuildCatalog to sort
// articles by access time, oldest-accessed first.
const CatalogSortLastRead = "last_read"

// ReadEvent is one access record written to reads.jsonl.
type ReadEvent struct {
	Path      string    `json:"path"`
	Timestamp time.Time `json:"ts"`
	// Reader is "web" for human browser access or an agent slug (e.g.
	// "slack-agent"). Empty string is never written — Append is a no-op
	// when reader is empty.
	Reader  string `json:"reader"`
	IsAgent bool   `json:"is_agent"` // true when Reader != "web"
}

// ReadStats summarises access history for one article path.
type ReadStats struct {
	LastRead       *time.Time // nil if the article has never been accessed
	HumanReadCount int        // reads where IsAgent == false
	AgentReadCount int        // reads where IsAgent == true
	DaysUnread     int        // whole days since LastRead; 0 if accessed today or never
}

// ReadLog appends access events to reads.jsonl and answers stats queries.
// It is safe to share across goroutines.
type ReadLog struct {
	path       string // absolute path to reads.jsonl
	mu         sync.Mutex
	dirEnsured bool // set after first successful ensureDir to skip the mkdir syscall
}

// NewReadLog constructs a ReadLog whose backing file sits at
// <wikiRoot>/.reads/reads.jsonl.
func NewReadLog(wikiRoot string) *ReadLog {
	return &ReadLog{
		path: filepath.Join(wikiRoot, ".reads", "reads.jsonl"),
	}
}

// Append records one access event. It is a no-op when reader is empty.
// Errors are logged but not returned — read tracking is best-effort and
// must never block the article response.
func (l *ReadLog) Append(relPath, reader string) {
	if l == nil || reader == "" {
		return
	}
	ev := ReadEvent{
		Path:      relPath,
		Timestamp: time.Now().UTC(),
		Reader:    reader,
		IsAgent:   reader != ReaderHuman,
	}
	line, err := json.Marshal(ev)
	if err != nil {
		log.Printf("wiki reads: marshal: %v", err)
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if !l.dirEnsured {
		if err := l.ensureDir(); err != nil {
			log.Printf("wiki reads: ensureDir: %v", err)
			return
		}
		l.dirEnsured = true
	}
	f, err := os.OpenFile(l.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		log.Printf("wiki reads: open: %v", err)
		return
	}
	defer f.Close()
	line = append(line, '\n')
	if _, err := f.Write(line); err != nil {
		log.Printf("wiki reads: write: %v", err)
	}
}

// Stats returns access statistics for a single article path by calling AllStats
// (full file scan). For multiple paths, call AllStats directly to avoid repeated
// scans.
func (l *ReadLog) Stats(relPath string) ReadStats {
	if l == nil {
		return ReadStats{}
	}
	all := l.AllStats()
	return all[relPath]
}

// AllStats returns access statistics for every tracked path in a single
// scan of reads.jsonl. Use this when you need stats for multiple articles
// (e.g. BuildCatalog sort=last_read) to avoid O(n*m) scans.
func (l *ReadLog) AllStats() map[string]ReadStats {
	if l == nil {
		return map[string]ReadStats{}
	}
	l.mu.Lock()
	defer l.mu.Unlock()

	result := map[string]ReadStats{}

	f, err := os.Open(l.path)
	if err != nil {
		if os.IsNotExist(err) {
			return result // zero reads — not an error
		}
		log.Printf("wiki reads: open for stats: %v", err)
		return result
	}
	defer f.Close()

	now := time.Now().UTC()
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var ev ReadEvent
		if err := json.Unmarshal(line, &ev); err != nil {
			log.Printf("wiki reads: skipping malformed line: %v", err)
			continue
		}
		s := result[ev.Path]
		if s.LastRead == nil || ev.Timestamp.After(*s.LastRead) {
			t := ev.Timestamp
			s.LastRead = &t
		}
		if ev.IsAgent {
			s.AgentReadCount++
		} else {
			s.HumanReadCount++
		}
		result[ev.Path] = s
	}
	if err := scanner.Err(); err != nil {
		log.Printf("wiki reads: scan error: %v", err)
	}

	// Compute DaysUnread for every entry now that LastRead is final.
	for path, s := range result {
		if s.LastRead != nil {
			days := int(now.Sub(*s.LastRead).Hours() / 24)
			if days < 0 {
				days = 0
			}
			s.DaysUnread = days
		}
		result[path] = s
	}

	return result
}

// ensureDir creates the .reads directory if it does not exist.
// Must be called with l.mu held.
func (l *ReadLog) ensureDir() error {
	return os.MkdirAll(filepath.Dir(l.path), 0o755)
}
