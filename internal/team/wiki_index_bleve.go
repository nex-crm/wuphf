package team

// wiki_index_bleve.go — BleveTextIndex: pure-Go BM25 TextIndex backend.
//
// Uses github.com/blevesearch/bleve/v2 (no cgo). The index is stored on disk
// at the path passed to NewBleveTextIndex. Callers must call Close() when done.
//
// Mapping: the `text` field is English-analysed for BM25 scoring. `id` and
// `entity_slug` are stored+indexed as keywords so hits carry entity context
// without a follow-up lookup.
//
// TopK cap: Search clamps topK at 100. The WikiIndex.Search wrapper already
// applies the caller-supplied limit; this is a belt-and-suspenders guard.

import (
	"context"
	"fmt"

	"github.com/blevesearch/bleve/v2"
	"github.com/blevesearch/bleve/v2/analysis/lang/en"
	"github.com/blevesearch/bleve/v2/mapping"
)

const bleveMaxTopK = 100

// bleveDoc is the document stored in the bleve index per TypedFact.
type bleveDoc struct {
	ID         string `json:"id"`
	EntitySlug string `json:"entity_slug"`
	Text       string `json:"text"`
}

// BleveTextIndex implements TextIndex via blevesearch/bleve/v2.
type BleveTextIndex struct {
	idx bleve.Index
}

// NewBleveTextIndex opens (or creates) the bleve index at dir.
// The caller must call Close() when done.
func NewBleveTextIndex(dir string) (*BleveTextIndex, error) {
	// Try opening an existing index first.
	existing, err := bleve.Open(dir)
	if err == nil {
		return &BleveTextIndex{idx: existing}, nil
	}
	// ErrorIndexPathDoesNotExist — the dir or bleve metadata does not exist yet.
	// ErrorIndexMetaMissing     — the dir exists but is empty (e.g. t.TempDir()).
	// Both cases: create a new index.
	if err != bleve.ErrorIndexPathDoesNotExist && err != bleve.ErrorIndexMetaMissing {
		return nil, fmt.Errorf("bleve open %s: %w", dir, err)
	}

	// Build index mapping.
	im := buildBleveMapping()
	idx, err := bleve.New(dir, im)
	if err != nil {
		return nil, fmt.Errorf("bleve new %s: %w", dir, err)
	}
	return &BleveTextIndex{idx: idx}, nil
}

// buildBleveMapping returns the index mapping used by BleveTextIndex.
//
//   - text     — English analyser for BM25 term scoring
//   - id       — keyword (stored+indexed) for retrieval without rescan
//   - entity_slug — keyword (stored+indexed) for entity context in hits
func buildBleveMapping() mapping.IndexMapping {
	im := bleve.NewIndexMapping()
	im.DefaultAnalyzer = "standard"

	// Document mapping for bleveDoc.
	dm := bleve.NewDocumentMapping()

	// text: English-analysed for BM25 ranking.
	textField := bleve.NewTextFieldMapping()
	textField.Analyzer = en.AnalyzerName
	textField.Store = true
	textField.Index = true
	dm.AddFieldMappingsAt("text", textField)

	// id: keyword — exact match + stored.
	idField := bleve.NewKeywordFieldMapping()
	idField.Store = true
	idField.Index = true
	dm.AddFieldMappingsAt("id", idField)

	// entity_slug: keyword — exact match + stored.
	slugField := bleve.NewKeywordFieldMapping()
	slugField.Store = true
	slugField.Index = true
	dm.AddFieldMappingsAt("entity_slug", slugField)

	im.DefaultMapping = dm
	return im
}

// --- TextIndex interface --------------------------------------------------

// Index adds or replaces a fact in the bleve index.
func (b *BleveTextIndex) Index(_ context.Context, f TypedFact) error {
	doc := bleveDoc{
		ID:         f.ID,
		EntitySlug: f.EntitySlug,
		Text:       f.Text,
	}
	return b.idx.Index(f.ID, doc)
}

// Delete removes a fact from the bleve index by its ID.
func (b *BleveTextIndex) Delete(_ context.Context, factID string) error {
	return b.idx.Delete(factID)
}

// Search runs a BM25 query against the `text` field and returns up to topK
// hits ordered by descending relevance score. topK is clamped at bleveMaxTopK.
//
// Uses a MatchQuery with the English analyser so stemmed terms ("promoted",
// "promotion" → "promot") match correctly against the indexed tokens.
func (b *BleveTextIndex) Search(_ context.Context, query string, topK int) ([]SearchHit, error) {
	if topK <= 0 {
		return nil, nil
	}
	if topK > bleveMaxTopK {
		topK = bleveMaxTopK
	}

	// MatchQuery applies the same English analyser used at index time so that
	// "promoted" and "promotion" both match facts containing either form.
	q := bleve.NewMatchQuery(query)
	q.SetField("text")
	q.Analyzer = en.AnalyzerName

	req := bleve.NewSearchRequestOptions(q, topK, 0, false)
	req.Fields = []string{"id", "entity_slug", "text"}

	res, err := b.idx.Search(req)
	if err != nil {
		return nil, fmt.Errorf("bleve search: %w", err)
	}

	hits := make([]SearchHit, 0, len(res.Hits))
	for _, h := range res.Hits {
		hit := SearchHit{
			FactID: h.ID,
			Score:  h.Score,
		}
		if v, ok := h.Fields["entity_slug"]; ok {
			if s, ok := v.(string); ok {
				hit.Entity = s
			}
		}
		if v, ok := h.Fields["text"]; ok {
			if s, ok := v.(string); ok {
				hit.Snippet = s
			}
		}
		hits = append(hits, hit)
	}
	return hits, nil
}

// Close releases the bleve index handle.
func (b *BleveTextIndex) Close() error {
	return b.idx.Close()
}
