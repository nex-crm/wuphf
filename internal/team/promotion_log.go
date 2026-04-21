package team

// promotion_log.go is the JSONL replay layer for the promotion state
// machine. Kept separate from promotion_state.go so the state-machine file
// stays under the soft size target — the two together own the persistence
// story for Lane C.
//
// Recovery posture: malformed lines are SKIPPED with a log warning. This
// matches the scanner's behaviour — one corrupted line must not cost us
// the whole review history. Unknown record types, missing IDs, and records
// that parse as JSON but fail schema checks are all treated as "skip + log".

import (
	"bufio"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strings"
)

// loadLocked replays the JSONL at l.path into the in-memory cache. Caller
// holds l.mu.
func (l *ReviewLog) loadLocked() error {
	f, err := os.Open(l.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("review log: open: %w", err)
	}
	defer func() { _ = f.Close() }()

	scanner := bufio.NewScanner(f)
	// Allow long comment bodies + state snapshots; 1 MiB per line.
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)

	lineNo := 0
	var commentsByID = map[string][]Comment{}
	for scanner.Scan() {
		lineNo++
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var peek struct {
			Type logRecordType `json:"type"`
		}
		if err := json.Unmarshal([]byte(line), &peek); err != nil {
			log.Printf("review log: skip malformed line %d: %v", lineNo, err)
			continue
		}
		switch peek.Type {
		case logRecordHeader:
			// Header is advisory; nothing to do on load.
			continue
		case logRecordState:
			var rec stateRecord
			if err := json.Unmarshal([]byte(line), &rec); err != nil {
				log.Printf("review log: skip malformed state line %d: %v", lineNo, err)
				continue
			}
			if rec.Promotion.ID == "" {
				log.Printf("review log: skip state line %d without promotion id", lineNo)
				continue
			}
			// Each state record is a full snapshot; the latest wins.
			p := rec.Promotion
			// Rebuild cache pointer (we want to append further state +
			// comments on the same struct, not the unmarshaled copy).
			cached := &p
			// Preserve comments accumulated from earlier comment lines —
			// snapshot carries its own Comments slice but if a later
			// comment line precedes the next state snapshot we need both.
			if earlier := l.promotions[p.ID]; earlier != nil {
				// Merge comment histories — dedupe by Comment.ID.
				seen := map[string]bool{}
				for _, c := range p.Comments {
					seen[c.ID] = true
				}
				for _, c := range earlier.Comments {
					if !seen[c.ID] {
						cached.Comments = append(cached.Comments, c)
						seen[c.ID] = true
					}
				}
			}
			l.promotions[p.ID] = cached
		case logRecordComment:
			var rec commentRecord
			if err := json.Unmarshal([]byte(line), &rec); err != nil {
				log.Printf("review log: skip malformed comment line %d: %v", lineNo, err)
				continue
			}
			if rec.PromotionID == "" {
				log.Printf("review log: skip comment line %d without promotion_id", lineNo)
				continue
			}
			commentsByID[rec.PromotionID] = append(commentsByID[rec.PromotionID], rec.Comment)
		default:
			log.Printf("review log: skip unknown record type %q at line %d", peek.Type, lineNo)
		}
	}
	if err := scanner.Err(); err != nil {
		// Don't fail the whole load on a single scan error — return what we
		// have. The broker keeps running and newer writes append normally.
		log.Printf("review log: scanner error after line %d: %v", lineNo, err)
	}

	// Fold late-arriving comments onto their promotion (dedupe by comment ID).
	for id, newComments := range commentsByID {
		p, ok := l.promotions[id]
		if !ok {
			continue
		}
		seen := map[string]bool{}
		for _, c := range p.Comments {
			seen[c.ID] = true
		}
		for _, c := range newComments {
			if seen[c.ID] {
				continue
			}
			p.Comments = append(p.Comments, c)
			seen[c.ID] = true
		}
	}

	// Recover the monotonic sequence so freshly-generated IDs don't collide
	// with replayed ones on the same second. Parse the trailing -NNNN off
	// the largest known ID and seed nextSeq with it.
	for id := range l.promotions {
		if seq := parseIDSeq(id); seq > l.nextSeq {
			l.nextSeq = seq
		}
	}
	return nil
}

// parseIDSeq extracts the trailing seq number from a review ID of the form
// `rvw-<unix>-<seq>`. Returns 0 when the ID doesn't match.
func parseIDSeq(id string) uint64 {
	parts := strings.Split(id, "-")
	if len(parts) < 3 {
		return 0
	}
	tail := parts[len(parts)-1]
	var n uint64
	for _, r := range tail {
		if r < '0' || r > '9' {
			return 0
		}
		n = n*10 + uint64(r-'0')
	}
	return n
}
