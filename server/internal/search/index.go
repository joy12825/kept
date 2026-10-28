package search

import (
	"errors"
	"fmt"
	"html"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/blevesearch/bleve/v2"
	"github.com/blevesearch/bleve/v2/analysis/analyzer/keyword"
	"github.com/blevesearch/bleve/v2/mapping"
	"github.com/blevesearch/bleve/v2/search/query"
	bolt "go.etcd.io/bbolt"

	"kept/server/internal/apperr"
)

// SchemaVersion changes whenever the mapping or the analyzers change;
// an index of another version is rebuilt from scratch.
const SchemaVersion = 2 // 2: the person (v1.6.3)

// Field names in the index.
const (
	fieldWorkspace     = "workspace_id"
	fieldTitle         = "title"
	fieldContent       = "content"
	fieldNotes         = "notes"
	fieldTags          = "tags"
	fieldCorrespondent = "correspondent"
	fieldPerson        = "person"
	fieldType          = "document_type"
	fieldCustom        = "custom_fields"
	fieldASN           = "asn"
	fieldCreated       = "created"
	fieldAdded         = "added"
)

// textFields are searched by plain words; title counts double (5.2).
var textFields = []struct {
	name  string
	boost float64
}{
	{fieldTitle, 2}, {fieldContent, 1}, {fieldNotes, 1}, {fieldTags, 1}, {fieldCorrespondent, 1}, {fieldPerson, 1}, {fieldType, 1}, {fieldCustom, 1},
}

// Doc is what the index holds for one document.
type Doc struct {
	ID, WorkspaceID int64
	Title           string
	Content         string
	Notes           []string
	Tags            []string // full paths, "财务 / 发票"
	Correspondent   string
	Person          string // v1.6.3: 家人
	DocumentType    string
	CustomFields    []string // values of the text and URL fields
	ASN             int64    // 0 when none
	Created         time.Time
	Added           time.Time
}

// Hit is a document matching a query, best first.
type Hit struct {
	ID    int64
	Score float64
}

// Segment is a piece of a snippet; Hit marks the matched words.
type Segment struct {
	Text string
	Hit  bool
}

var (
	ErrClosed = errors.New("search: the index is closed")
	ErrInUse  = errors.New("search: the index is in use by another process (is kept serve running?)")
	// ErrRebuilding is shown to people: the index is being built from scratch.
	ErrRebuilding = apperr.WithCode(apperr.KindUnavailable, "search_unavailable", "搜索索引正在重建，请稍后再试")
)

// Index is the instance-wide full-text index. Every query is confined to
// one workspace here; callers cannot leave that out.
type Index struct {
	mu  sync.RWMutex
	idx bleve.Index
}

const schemaKey = "kept_schema_version"

// Open opens the index in dir, creating it when missing. A different schema
// version or an unreadable index is deleted and recreated empty; rebuilt
// then reports that every document must be indexed again.
func Open(dir string) (ix *Index, rebuilt bool, err error) {
	go LoadDictionary() // 0.3 s; the first tokenizing waits for it
	path := filepath.Join(dir, "documents.bleve")
	idx, err := bleve.OpenUsing(path, map[string]any{"bolt_timeout": "2s"})
	if errors.Is(err, bolt.ErrTimeout) {
		return nil, false, ErrInUse // another process has it open: never delete it
	}
	if err == nil {
		v, verr := idx.GetInternal([]byte(schemaKey))
		if verr == nil && string(v) == strconv.Itoa(SchemaVersion) {
			return &Index{idx: idx}, false, nil
		}
		idx.Close()
	}
	if err := os.RemoveAll(path); err != nil {
		return nil, false, err
	}
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return nil, false, err
	}
	m, err := newMapping()
	if err != nil {
		return nil, false, err
	}
	idx, err = bleve.New(path, m)
	if err != nil {
		return nil, false, fmt.Errorf("create the search index: %w", err)
	}
	if err := idx.SetInternal([]byte(schemaKey), []byte(strconv.Itoa(SchemaVersion))); err != nil {
		idx.Close()
		return nil, false, err
	}
	return &Index{idx: idx}, true, nil
}

func newMapping() (*mapping.IndexMappingImpl, error) {
	m := bleve.NewIndexMapping()
	for name, tok := range map[string]string{indexAnalyzer: "kept_gse_index", queryAnalyzer: "kept_gse_query"} {
		if err := m.AddCustomAnalyzer(name, map[string]any{"type": "custom", "tokenizer": tok}); err != nil {
			return nil, err
		}
	}
	text := func(store bool) *mapping.FieldMapping {
		f := bleve.NewTextFieldMapping()
		f.Analyzer = indexAnalyzer
		f.Store = store
		f.IncludeTermVectors = true // highlights
		f.IncludeInAll = false
		return f
	}
	ws := bleve.NewTextFieldMapping()
	ws.Analyzer = keyword.Name
	ws.Store, ws.IncludeInAll, ws.IncludeTermVectors = false, false, false
	num := bleve.NewNumericFieldMapping()
	num.Store, num.IncludeInAll = false, false
	date := bleve.NewDateTimeFieldMapping()
	date.Store, date.IncludeInAll = false, false

	doc := bleve.NewDocumentStaticMapping()
	doc.AddFieldMappingsAt(fieldWorkspace, ws)
	doc.AddFieldMappingsAt(fieldTitle, text(true))
	doc.AddFieldMappingsAt(fieldContent, text(true))
	doc.AddFieldMappingsAt(fieldNotes, text(true))
	for _, f := range []string{fieldTags, fieldCorrespondent, fieldPerson, fieldType, fieldCustom} {
		doc.AddFieldMappingsAt(f, text(false))
	}
	doc.AddFieldMappingsAt(fieldASN, num)
	doc.AddFieldMappingsAt(fieldCreated, date)
	doc.AddFieldMappingsAt(fieldAdded, date)
	m.DefaultMapping = doc
	m.DefaultAnalyzer = indexAnalyzer
	return m, nil
}

func docID(id int64) string { return strconv.FormatInt(id, 10) }

func (d Doc) fields() map[string]any {
	f := map[string]any{
		fieldWorkspace: docID(d.WorkspaceID), fieldTitle: d.Title, fieldContent: d.Content, fieldNotes: strings.Join(d.Notes, "\n"),
		fieldTags: strings.Join(d.Tags, "\n"), fieldCorrespondent: d.Correspondent, fieldPerson: d.Person, fieldType: d.DocumentType,
		fieldCustom: strings.Join(d.CustomFields, "\n"), fieldCreated: d.Created, fieldAdded: d.Added,
	}
	if d.ASN != 0 {
		f[fieldASN] = float64(d.ASN)
	}
	return f
}

// Put adds or replaces documents.
func (ix *Index) Put(docs []Doc) error {
	ix.mu.RLock()
	defer ix.mu.RUnlock()
	if ix.idx == nil {
		return ErrClosed
	}
	b := ix.idx.NewBatch()
	for _, d := range docs {
		if err := b.Index(docID(d.ID), d.fields()); err != nil {
			return err
		}
	}
	return ix.idx.Batch(b)
}

// Delete removes documents; unknown IDs are fine.
func (ix *Index) Delete(ids []int64) error {
	ix.mu.RLock()
	defer ix.mu.RUnlock()
	if ix.idx == nil {
		return ErrClosed
	}
	b := ix.idx.NewBatch()
	for _, id := range ids {
		b.Delete(docID(id))
	}
	return ix.idx.Batch(b)
}

// DeleteWorkspace removes every document of a purged workspace.
func (ix *Index) DeleteWorkspace(ws int64) error {
	for {
		hits, _, err := ix.search(ws, bleve.NewMatchAllQuery(), 1000)
		if err != nil || len(hits) == 0 {
			return err
		}
		ids := make([]int64, len(hits))
		for i, h := range hits {
			ids[i] = h.ID
		}
		if err := ix.Delete(ids); err != nil {
			return err
		}
	}
}

// IDs lists the documents of workspace ws in the index (integrity check).
func (ix *Index) IDs(ws int64) ([]int64, error) {
	total, err := ix.Count()
	if err != nil {
		return nil, err
	}
	hits, _, err := ix.search(ws, bleve.NewMatchAllQuery(), int(total)+1)
	if err != nil {
		return nil, err
	}
	ids := make([]int64, len(hits))
	for i, h := range hits {
		ids[i] = h.ID
	}
	return ids, nil
}

// Count is the number of documents in the index (all workspaces).
func (ix *Index) Count() (uint64, error) {
	ix.mu.RLock()
	defer ix.mu.RUnlock()
	if ix.idx == nil {
		return 0, ErrClosed
	}
	return ix.idx.DocCount()
}

// Search returns up to limit documents of workspace ws matching q, best
// first, and how many matched in all.
func (ix *Index) Search(ws int64, q query.Query, limit int) ([]Hit, uint64, error) {
	return ix.search(ws, q, limit)
}

func inWorkspace(ws int64, q query.Query) query.Query {
	t := bleve.NewTermQuery(docID(ws))
	t.SetField(fieldWorkspace)
	return bleve.NewConjunctionQuery(t, q)
}

func (ix *Index) search(ws int64, q query.Query, limit int) ([]Hit, uint64, error) {
	ix.mu.RLock()
	defer ix.mu.RUnlock()
	if ix.idx == nil {
		return nil, 0, ErrClosed
	}
	req := bleve.NewSearchRequestOptions(inWorkspace(ws, q), limit, 0, false)
	res, err := ix.idx.Search(req)
	if err != nil {
		return nil, 0, err
	}
	hits := make([]Hit, 0, len(res.Hits))
	for _, h := range res.Hits {
		id, err := strconv.ParseInt(h.ID, 10, 64)
		if err != nil {
			continue
		}
		hits = append(hits, Hit{ID: id, Score: h.Score})
	}
	return hits, res.Total, nil
}

// snippetAround is how many characters a snippet keeps before the first
// match and in total.
const (
	snippetBefore = 20
	snippetTotal  = 80
)

// Highlight returns, for each of ids that matches q, a snippet of the
// recognised text, the notes or the title (the first that matches).
func (ix *Index) Highlight(ws int64, q query.Query, ids []int64) (map[int64][]Segment, error) {
	ix.mu.RLock()
	defer ix.mu.RUnlock()
	if ix.idx == nil || len(ids) == 0 {
		return map[int64][]Segment{}, nil
	}
	sids := make([]string, len(ids))
	for i, id := range ids {
		sids[i] = docID(id)
	}
	req := bleve.NewSearchRequestOptions(bleve.NewConjunctionQuery(bleve.NewDocIDQuery(sids), inWorkspace(ws, q)), len(ids), 0, false)
	req.Highlight = bleve.NewHighlightWithStyle("html")
	req.Highlight.Fields = []string{fieldContent, fieldNotes, fieldTitle}
	res, err := ix.idx.Search(req)
	if err != nil {
		return nil, err
	}
	out := map[int64][]Segment{}
	for _, h := range res.Hits {
		id, err := strconv.ParseInt(h.ID, 10, 64)
		if err != nil {
			continue
		}
	fields:
		for _, f := range []string{fieldContent, fieldNotes, fieldTitle} {
			for _, frag := range h.Fragments[f] { // fields without a match still get a plain fragment
				if strings.Contains(frag, "<mark>") {
					out[id] = trimSnippet(parseFragment(frag))
					break fields
				}
			}
		}
	}
	return out, nil
}

var spaces = regexp.MustCompile(`\s+`)

// parseFragment splits Bleve's HTML fragment ("…<mark>电费</mark>…") into
// plain segments.
func parseFragment(frag string) []Segment {
	var out []Segment
	for frag != "" {
		i := strings.Index(frag, "<mark>")
		if i < 0 {
			out = append(out, Segment{Text: html.UnescapeString(frag)})
			break
		}
		if i > 0 {
			out = append(out, Segment{Text: html.UnescapeString(frag[:i])})
		}
		frag = frag[i+len("<mark>"):]
		j := strings.Index(frag, "</mark>")
		if j < 0 {
			j = len(frag)
		}
		out = append(out, Segment{Text: html.UnescapeString(frag[:j]), Hit: true})
		frag = strings.TrimPrefix(frag[j:], "</mark>")
	}
	for i := range out {
		out[i].Text = spaces.ReplaceAllString(out[i].Text, " ") // line breaks fold into spaces
	}
	return mergeSegments(out)
}

func mergeSegments(in []Segment) []Segment {
	var out []Segment
	for _, s := range in {
		if s.Text == "" {
			continue
		}
		if n := len(out); n > 0 && out[n-1].Hit == s.Hit {
			out[n-1].Text += s.Text
			continue
		}
		out = append(out, s)
	}
	return out
}

// trimSnippet keeps about snippetBefore characters before the first match
// and snippetTotal in all, marking cuts with “…”.
func trimSnippet(segs []Segment) []Segment {
	first := -1
	for i, s := range segs {
		if s.Hit {
			first = i
			break
		}
	}
	if first > 0 {
		r := []rune(segs[first-1].Text)
		if len(r) > snippetBefore {
			segs[first-1].Text = "…" + string(r[len(r)-snippetBefore:])
		}
		segs = segs[first-1:]
	}
	var out []Segment
	left := snippetTotal
	for _, s := range segs {
		r := []rune(s.Text)
		if len(r) > left {
			if s.Hit {
				out = append(out, s, Segment{Text: "…"})
			} else {
				out = append(out, Segment{Text: string(r[:left]) + "…"})
			}
			return out
		}
		out = append(out, s)
		left -= len(r)
	}
	return out
}

// Close closes the index; later calls fail with ErrClosed.
func (ix *Index) Close() error {
	ix.mu.Lock()
	defer ix.mu.Unlock()
	if ix.idx == nil {
		return nil
	}
	err := ix.idx.Close()
	ix.idx = nil
	return err
}
