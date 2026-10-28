package document

import (
	"context"
	"encoding/json"

	"kept/server/internal/audit"
	"kept/server/internal/filter"
	"kept/server/internal/search"
	"kept/server/internal/tenant"
)

// searches answers the text conditions of one request from the full-text
// index (design doc 5.1) and remembers the hits, so the list can rank and
// highlight by them without asking twice.
type searches struct {
	ix        *search.Index
	ready     func() error
	ws        int64
	limit     int
	hits      map[searchKey][]search.Hit
	truncated bool
}

type searchKey struct {
	text   string
	prefix bool
}

// catalog loads the workspace's classifications with text conditions wired
// to the index.
func (s *Service) catalog(ctx context.Context, scope tenant.Scope) (*filter.Catalog, *searches, error) {
	cat, err := filter.LoadCatalog(ctx, s.q, scope)
	if err != nil {
		return nil, nil, err
	}
	cat.UserID = audit.ActorFrom(ctx).UserID // the caller's own reading status
	sr := &searches{ix: s.opt.Search, ready: s.opt.SearchReady, ws: scope.WorkspaceID(), limit: s.opt.SearchLimit, hits: map[searchKey][]search.Hit{}}
	if sr.limit <= 0 {
		sr.limit = 10000
	}
	if sr.ix != nil {
		cat.Text = sr.resolve
	}
	return cat, sr, nil
}

func (sr *searches) resolve(text string, prefix bool) ([]int64, bool, error) {
	q := search.Parse(text, search.QueryOptions{Prefix: prefix})
	if q == nil {
		return nil, true, nil // nothing to search for
	}
	if sr.ready != nil {
		if err := sr.ready(); err != nil {
			return nil, false, err
		}
	}
	key := searchKey{text, prefix}
	hits, ok := sr.hits[key]
	if !ok {
		var total uint64
		var err error
		hits, total, err = sr.ix.Search(sr.ws, q, sr.limit)
		if err != nil {
			return nil, false, err
		}
		if total > uint64(len(hits)) {
			sr.truncated = true
		}
		sr.hits[key] = hits
	}
	ids := make([]int64, len(hits))
	for i, h := range hits {
		ids[i] = h.ID
	}
	return ids, false, nil
}

// primary is the text condition results are ranked and highlighted by: the
// first one a matching document satisfies.
func primary(c filter.Cond) (searchKey, bool) {
	texts := c.Texts()
	if len(texts) == 0 {
		return searchKey{}, false
	}
	return searchKey{*texts[0].Text, texts[0].Prefix}, true
}

// ranks is the JSON array of the primary search's hits, best first, for
// joining with json_each (its key is the rank).
func (sr *searches) ranks(k searchKey) string {
	hits := sr.hits[k]
	ids := make([]int64, len(hits))
	for i, h := range hits {
		ids[i] = h.ID
	}
	b, _ := json.Marshal(ids)
	return string(b)
}

// highlight fills in snippets for a page of results.
func (sr *searches) highlight(k searchKey, items []Summary) error {
	q := search.Parse(k.text, search.QueryOptions{Prefix: k.prefix})
	if q == nil || sr.ix == nil || len(items) == 0 {
		return nil
	}
	ids := make([]int64, len(items))
	for i, it := range items {
		ids[i] = it.ID
	}
	snippets, err := sr.ix.Highlight(sr.ws, q, ids)
	if err != nil {
		return err
	}
	for i := range items {
		items[i].Highlight = snippets[items[i].ID]
	}
	return nil
}
