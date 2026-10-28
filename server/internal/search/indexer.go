package search

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"strings"
	"sync/atomic"
	"time"

	"kept/server/internal/events"
)

// Indexer keeps the index up to date with the documents table. A document
// whose indexed_rev is behind its index_rev is (re)written, or removed when
// it is in the trash; indexed_rev is then set to the index_rev it read, so a
// change made while it was being indexed is picked up on the next pass.
type Indexer struct {
	// rebuilding is set while every document waits to be indexed (a new or
	// recreated index): keyword search would miss most of them.
	rebuilding atomic.Bool

	db  *sql.DB
	ix  *Index
	bus *events.Bus
	log *slog.Logger
}

// indexBatch is how many documents one pass handles.
const indexBatch = 200

func NewIndexer(db *sql.DB, ix *Index, bus *events.Bus, log *slog.Logger) *Indexer {
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	return &Indexer{db: db, ix: ix, bus: bus, log: log}
}

// MarkAll makes every document behind (after the index was recreated).
func (x *Indexer) MarkAll(ctx context.Context) error {
	x.rebuilding.Store(true)
	_, err := x.db.ExecContext(ctx, `UPDATE documents SET indexed_rev = 0`)
	return err
}

// Ready refuses keyword searches while the whole index is being rebuilt.
func (x *Indexer) Ready() error {
	if x.rebuilding.Load() {
		return ErrRebuilding
	}
	return nil
}

// Run indexes in the background until ctx ends: at start, whenever a
// document event arrives, and every 30 seconds as a safety net.
func (x *Indexer) Run(ctx context.Context) {
	var wake <-chan events.Event
	if x.bus != nil {
		ch, cancel := x.bus.SubscribeAll()
		defer cancel()
		wake = ch
	}
	tick := time.NewTicker(30 * time.Second)
	defer tick.Stop()
	for {
		start := time.Now()
		total := 0
		for {
			n, err := x.Pass(ctx)
			if err != nil {
				if ctx.Err() == nil {
					x.log.Error("index documents", "err", err)
				}
				break
			}
			total += n
			if n < indexBatch {
				x.rebuilding.Store(false) // caught up
				break
			}
		}
		if total >= indexBatch {
			x.log.Info("indexed documents", "count", total, "took", time.Since(start).Round(time.Millisecond))
		}
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		case <-wake:
			// Coalesce a burst of events (a bulk edit sends one per document).
			time.Sleep(200 * time.Millisecond)
			for len(wake) > 0 {
				<-wake
			}
		}
	}
}

// Behind reports how many documents wait to be indexed.
func (x *Indexer) Behind(ctx context.Context) (int, error) {
	var n int
	err := x.db.QueryRowContext(ctx, `SELECT count(*) FROM documents WHERE indexed_rev < index_rev`).Scan(&n)
	return n, err
}

type pending struct {
	doc     Doc
	deleted bool
	rev     int64
	corrID  sql.NullInt64
	person  sql.NullInt64
	typeID  sql.NullInt64
	created string
	addedMs int64
}

// Pass indexes one batch of documents that are behind and returns how many.
func (x *Indexer) Pass(ctx context.Context) (int, error) {
	rows, err := x.db.QueryContext(ctx, `SELECT id, workspace_id, title, content, created_date, added_at, correspondent_id, document_type_id, person_id,
		coalesce(asn, 0), deleted_at IS NOT NULL, index_rev FROM documents WHERE indexed_rev < index_rev ORDER BY id LIMIT ?`, indexBatch)
	if err != nil {
		return 0, err
	}
	var batch []*pending
	for rows.Next() {
		p := &pending{}
		if err := rows.Scan(&p.doc.ID, &p.doc.WorkspaceID, &p.doc.Title, &p.doc.Content, &p.created, &p.addedMs, &p.corrID, &p.typeID, &p.person,
			&p.doc.ASN, &p.deleted, &p.rev); err != nil {
			rows.Close()
			return 0, err
		}
		batch = append(batch, p)
	}
	rows.Close()
	if err := rows.Err(); err != nil || len(batch) == 0 {
		return 0, err
	}

	var put []Doc
	var del []int64
	live := map[int64]*pending{}
	for _, p := range batch {
		if p.deleted {
			del = append(del, p.doc.ID)
			continue
		}
		p.doc.Created, _ = time.ParseInLocation("2006-01-02", p.created, time.Local)
		p.doc.Added = time.UnixMilli(p.addedMs)
		live[p.doc.ID] = p
	}
	if err := x.decorate(ctx, live); err != nil {
		return 0, err
	}
	for _, p := range batch {
		if !p.deleted {
			put = append(put, p.doc)
		}
	}
	if len(put) > 0 {
		if err := x.ix.Put(put); err != nil {
			return 0, err
		}
	}
	if len(del) > 0 {
		if err := x.ix.Delete(del); err != nil {
			return 0, err
		}
	}
	for _, p := range batch {
		if _, err := x.db.ExecContext(ctx, `UPDATE documents SET indexed_rev = ? WHERE id = ? AND indexed_rev < ?`, p.rev, p.doc.ID, p.rev); err != nil {
			return 0, err
		}
	}
	return len(batch), nil
}

// decorate fills in what lives outside the documents row: classification
// names, tag paths, notes and text custom fields.
func (x *Indexer) decorate(ctx context.Context, docs map[int64]*pending) error {
	if len(docs) == 0 {
		return nil
	}
	ids := make([]any, 0, len(docs))
	wsSet := map[int64]bool{}
	for id, p := range docs {
		ids = append(ids, id)
		wsSet[p.doc.WorkspaceID] = true
	}
	in := "(" + strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",") + ")"
	wss := make([]any, 0, len(wsSet))
	for ws := range wsSet {
		wss = append(wss, ws)
	}
	wsIn := "(" + strings.TrimSuffix(strings.Repeat("?,", len(wss)), ",") + ")"

	names := func(table string) (map[int64]string, error) {
		rows, err := x.db.QueryContext(ctx, "SELECT id, name FROM "+table+" WHERE workspace_id IN "+wsIn, wss...)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		m := map[int64]string{}
		for rows.Next() {
			var id int64
			var n string
			if err := rows.Scan(&id, &n); err != nil {
				return nil, err
			}
			m[id] = n
		}
		return m, rows.Err()
	}
	corrs, err := names("correspondents")
	if err != nil {
		return err
	}
	types, err := names("document_types")
	if err != nil {
		return err
	}
	persons, err := names("persons")
	if err != nil {
		return err
	}
	for _, p := range docs {
		p.doc.Correspondent = corrs[p.corrID.Int64]
		p.doc.DocumentType = types[p.typeID.Int64]
		p.doc.Person = persons[p.person.Int64]
	}

	// Tags as full paths, so "财务" also finds documents tagged 财务 / 发票.
	rows, err := x.db.QueryContext(ctx, "SELECT id, name, coalesce(parent_id, 0) FROM tags WHERE workspace_id IN "+wsIn, wss...)
	if err != nil {
		return err
	}
	type tag struct {
		name   string
		parent int64
	}
	tags := map[int64]tag{}
	for rows.Next() {
		var id int64
		var t tag
		if err := rows.Scan(&id, &t.name, &t.parent); err != nil {
			rows.Close()
			return err
		}
		tags[id] = t
	}
	rows.Close()
	path := func(id int64) string {
		var parts []string
		for guard := 0; id != 0 && guard < 10; guard++ {
			t, ok := tags[id]
			if !ok {
				break
			}
			parts = append([]string{t.name}, parts...)
			id = t.parent
		}
		return strings.Join(parts, " / ")
	}

	each := func(query string, f func(id int64, value string)) error {
		rows, err := x.db.QueryContext(ctx, query, ids...)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var id int64
			var v string
			if err := rows.Scan(&id, &v); err != nil {
				return err
			}
			f(id, v)
		}
		return rows.Err()
	}
	if err := each("SELECT document_id, CAST(tag_id AS TEXT) FROM document_tags WHERE document_id IN "+in, func(id int64, v string) {
		var tid int64
		fmt.Sscan(v, &tid)
		if p := path(tid); p != "" {
			docs[id].doc.Tags = append(docs[id].doc.Tags, p)
		}
	}); err != nil {
		return err
	}
	if err := each("SELECT document_id, body FROM notes WHERE document_id IN "+in+" ORDER BY id", func(id int64, v string) {
		docs[id].doc.Notes = append(docs[id].doc.Notes, v)
	}); err != nil {
		return err
	}
	if err := each(`SELECT v.document_id, v.value_text FROM custom_field_values v JOIN custom_fields f ON f.id = v.field_id
		WHERE f.data_type IN ('text', 'url') AND v.value_text IS NOT NULL AND v.document_id IN `+in, func(id int64, v string) {
		docs[id].doc.CustomFields = append(docs[id].doc.CustomFields, v)
	}); err != nil {
		return err
	}
	// Book records (v0.4): authors, publisher, series and ISBN are searchable
	// like text custom fields.
	return each(`SELECT document_id, trim(publisher || ' ' || series || ' ' || isbn || ' ' ||
		coalesce((SELECT group_concat(a.value, ' ') FROM json_each(authors) a), '')) FROM books WHERE document_id IN `+in, func(id int64, v string) {
		if v != "" {
			docs[id].doc.CustomFields = append(docs[id].doc.CustomFields, v)
		}
	})
}
