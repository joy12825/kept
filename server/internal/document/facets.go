package document

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"sync"

	"kept/server/internal/apperr"
	"kept/server/internal/filter"
	"kept/server/internal/tenant"

	"golang.org/x/sync/errgroup"
)

// Facet groups: the counts next to the options of the filter buttons, and
// the month headings of the list.
const (
	FacetType          = "type"
	FacetCorrespondent = "correspondent"
	FacetTag           = "tag"
	FacetYear          = "year"
	FacetMonth         = "month"
	FacetDrawer        = "drawer"
	FacetPerson        = "person"   // v1.6.3
	FacetLocation      = "location" // v1.6.3
)

// None is the bucket of documents without a type or correspondent.
const None = "none"

// facetField is the filter field whose conditions a group ignores.
var facetField = map[string]string{
	FacetType: "type", FacetCorrespondent: "correspondent", FacetTag: "tag", FacetYear: "created", FacetMonth: "created",
	FacetDrawer: "drawer", FacetPerson: "person", FacetLocation: "location",
}

// Bucket is one option and how many documents it would match.
type Bucket struct {
	Key   string // an ID, a year ("2024"), a month ("2024-03") or None
	Count int64
}

// Facets counts, for each group, the documents matching the filter with the
// group's own conditions taken out (the prototype's matchDoc(…, except)):
// next to “类型” the counts show what choosing another type would give.
// A parent tag counts the documents on its child tags too, once each.
func (s *Service) Facets(ctx context.Context, scope tenant.Scope, f filter.Cond, groups []string) (map[string][]Bucket, error) {
	cat, _, err := s.catalog(ctx, scope)
	if err != nil {
		return nil, err
	}
	comps := make([]filter.Compiled, len(groups))
	for i, g := range groups {
		field, ok := facetField[g]
		if !ok {
			return nil, apperr.New(apperr.KindInvalid, fmt.Sprintf("不支持的分组：%s", g))
		}
		if comps[i], err = filter.Compile(cat, scope, f.Without(field)); err != nil {
			return nil, err
		}
	}
	// Each group reads the whole space: they run side by side (v1.13.5:
	// 300 ms one after another at 100,000 documents), a few at a time.
	out := make(map[string][]Bucket, len(groups))
	var mu sync.Mutex
	eg, gctx := errgroup.WithContext(ctx)
	eg.SetLimit(4)
	for i, g := range groups {
		eg.Go(func() error {
			var buckets []Bucket
			var err error
			switch g {
			case FacetTag:
				buckets, err = s.tagFacet(gctx, cat, comps[i])
			default:
				expr := map[string]string{
					FacetType: "d.document_type_id", FacetCorrespondent: "d.correspondent_id", FacetDrawer: "d.drawer_id",
					FacetPerson: "d.person_id", FacetLocation: "d.location_id",
					FacetYear: "substr(d.created_date, 1, 4)", FacetMonth: "substr(d.created_date, 1, 7)",
				}[g]
				buckets, err = s.groupCount(gctx, expr, comps[i])
			}
			if err != nil {
				return err
			}
			mu.Lock()
			out[g] = buckets
			mu.Unlock()
			return nil
		})
	}
	if err := eg.Wait(); err != nil {
		return nil, err
	}
	return out, nil
}

func (s *Service) groupCount(ctx context.Context, expr string, comp filter.Compiled) ([]Bucket, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT "+expr+", count(*) FROM documents d WHERE "+comp.Where+" GROUP BY 1 ORDER BY 2 DESC, 1", comp.Args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	buckets := []Bucket{}
	for rows.Next() {
		var key any
		var n int64
		if err := rows.Scan(&key, &n); err != nil {
			return nil, err
		}
		b := Bucket{Key: None, Count: n}
		switch k := key.(type) {
		case int64:
			b.Key = strconv.FormatInt(k, 10)
		case string:
			b.Key = k
		case []byte:
			b.Key = string(k)
		}
		buckets = append(buckets, b)
	}
	return buckets, rows.Err()
}

func (s *Service) tagFacet(ctx context.Context, cat *filter.Catalog, comp filter.Compiled) ([]Bucket, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT dt.document_id, dt.tag_id FROM document_tags dt JOIN documents d ON d.id = dt.document_id WHERE "+comp.Where, comp.Args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	perDoc := map[int64]map[int64]bool{}
	for rows.Next() {
		var doc, tag int64
		if err := rows.Scan(&doc, &tag); err != nil {
			return nil, err
		}
		if perDoc[doc] == nil {
			perDoc[doc] = map[int64]bool{}
		}
		for _, a := range cat.Ancestors(tag) {
			perDoc[doc][a] = true
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	counts := map[int64]int64{}
	for _, tags := range perDoc {
		for t := range tags {
			counts[t]++
		}
	}
	buckets := make([]Bucket, 0, len(counts))
	for t, n := range counts {
		buckets = append(buckets, Bucket{Key: strconv.FormatInt(t, 10), Count: n})
	}
	sortBuckets(buckets)
	return buckets, nil
}

// sortBuckets puts the most documents first, then orders by key.
func sortBuckets(b []Bucket) {
	sort.Slice(b, func(i, j int) bool {
		if b[i].Count != b[j].Count {
			return b[i].Count > b[j].Count
		}
		return b[i].Key < b[j].Key
	})
}
