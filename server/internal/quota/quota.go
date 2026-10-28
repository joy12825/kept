// Package quota counts what each workspace stores and enforces the limit
// an instance administrator set (v1.0 stage 36).
package quota

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"kept/server/internal/apperr"
	"kept/server/internal/audit"
	"kept/server/internal/events"
	"kept/server/internal/files"
	"kept/server/internal/store/sqlc"
)

// maxAge is how long a count is used without anything changing.
const maxAge = 10 * time.Minute

// Usage is what a workspace stores, in bytes. Documents in the trash keep
// their files until purged: Trash counts all of theirs, the other parts
// only the documents not in the trash; Total is everything.
type Usage struct {
	Originals  int64
	Searchable int64 // OCR copies and the base files of edited pages
	Thumbnails int64 // thumbnails and book covers
	Versions   int64 // earlier versions of documents
	Trash      int64
	Total      int64
	Documents  int // not in the trash
	CountedAt  time.Time
}

// Limit is a workspace's quota.
type Limit struct {
	Bytes   int64 // 0: no limit
	Default bool  // follows KEPT_DEFAULT_QUOTA
}

type Options struct {
	// Room refuses bytes the workspace's storage has no room for (v1.7.10:
	// a storage's capacity); nil checks nothing more.
	Room         func(ctx context.Context, ws, need int64) error
	Files        *files.Store
	DefaultBytes int64 // KEPT_DEFAULT_QUOTA; 0 is no limit
	Log          *slog.Logger
	Now          func() time.Time
}

type Service struct {
	db  *sql.DB
	q   *sqlc.Queries
	opt Options

	mu    sync.Mutex
	cache map[int64]Usage
	dirty map[int64]bool

	// Space held by uploads checked but not queued yet (v1.7.4), so two
	// at once cannot both fit in the room left for one.
	resMu    sync.Mutex
	wsLocks  map[int64]*sync.Mutex
	reserved map[int64]int64
	byUser   map[userKey]int64 // the same, by uploader (v1.7.11)
}

type userKey struct{ ws, user int64 }

func (s *Service) heldBy(ws, user int64) int64 {
	s.resMu.Lock()
	defer s.resMu.Unlock()
	return s.byUser[userKey{ws, user}]
}

// Estimate is the room an upload of n bytes is given: the original and
// the searchable PDF made from it, about as large (v1.7.4).
func Estimate(n int64) int64 { return 2 * n }

func NewService(db *sql.DB, opt Options) *Service {
	if opt.Now == nil {
		opt.Now = time.Now
	}
	if opt.Log == nil {
		opt.Log = slog.New(slog.DiscardHandler)
	}
	return &Service{db: db, q: sqlc.New(db), opt: opt, cache: map[int64]Usage{}, dirty: map[int64]bool{}}
}

// ErrExceeded is returned when something new would not fit.
func errExceeded(u Usage, l Limit) error {
	return apperr.WithCode(apperr.KindConflict, "quota_exceeded",
		fmt.Sprintf("空间已满（已用 %s / 配额 %s）。清空回收站或删除不需要的文档，或请管理员提高配额。", Human(u.Total), Human(l.Bytes)))
}

// Human writes a size the way people read it: 1.5 GB, 320 MB.
func Human(n int64) string {
	switch {
	case n >= 1<<30:
		return strings.TrimSuffix(strconv.FormatFloat(float64(n)/(1<<30), 'f', 1, 64), ".0") + " GB"
	case n >= 1<<20:
		return strconv.FormatInt((n+(1<<19))>>20, 10) + " MB"
	case n >= 1<<10:
		return strconv.FormatInt((n+(1<<9))>>10, 10) + " KB"
	}
	return strconv.FormatInt(n, 10) + " B"
}

func size(p string) int64 {
	st, err := os.Stat(p)
	if err != nil {
		return 0
	}
	return st.Size()
}

// MarkDirty makes the next Usage count the workspace again.
func (s *Service) MarkDirty(ws int64) {
	s.mu.Lock()
	s.dirty[ws] = true
	s.mu.Unlock()
}

// Usage is the workspace's count, counted again when something changed or
// the last count is old.
func (s *Service) Usage(ctx context.Context, ws int64) (Usage, error) {
	s.mu.Lock()
	u, ok := s.cache[ws]
	fresh := ok && !s.dirty[ws] && s.opt.Now().Sub(u.CountedAt) < maxAge
	s.mu.Unlock()
	if fresh {
		return u, nil
	}
	return s.Recount(ctx, ws)
}

// Recount adds up the workspace's files. Stored files (original,
// searchable PDF, page-edit base, earlier versions) count by the sizes
// recorded in the database (v1.7.4); a document whose sizes are not known
// yet (new files, or from before v1.7.4) is asked from its storage once
// and the answer recorded. Thumbnails and covers are files of this
// machine, counted directly.
func (s *Service) Recount(ctx context.Context, ws int64) (Usage, error) {
	s.mu.Lock()
	delete(s.dirty, ws) // changes from now on mark it again
	s.mu.Unlock()
	u := Usage{CountedAt: s.opt.Now()}
	if s.opt.Files == nil {
		return u, nil
	}
	docs, err := s.q.ListDocumentFiles(ctx, ws)
	if err != nil {
		return u, err
	}
	fs := s.opt.Files
	// known: the size, and whether it can be recorded (a missing file is
	// 0; a storage that does not answer is counted 0 this time only).
	stat := func(storage string, r files.Ref) (int64, bool) {
		info, err := fs.Stat(ctx, storage, r)
		if err == nil {
			return info.Size, true
		}
		return 0, errors.Is(err, os.ErrNotExist)
	}
	for _, d := range docs {
		o, a := d.OriginalSize, d.DerivedSize
		if !o.Valid || !a.Valid {
			record := true
			if !o.Valid {
				n, ok := stat(d.Storage, files.Original(ws, d.ID, d.OriginalExt))
				o, record = sql.NullInt64{Int64: n, Valid: true}, ok
			}
			if !a.Valid {
				n1, ok1 := stat(d.Storage, files.Archive(ws, d.ID))
				n2, ok2 := stat(d.Storage, files.Base(ws, d.ID))
				a, record = sql.NullInt64{Int64: n1 + n2, Valid: true}, record && ok1 && ok2
			}
			if record {
				if err := s.q.SetDocumentSizes(ctx, sqlc.SetDocumentSizesParams{OriginalSize: o, DerivedSize: a, ID: d.ID}); err != nil {
					return u, err
				}
			}
		}
		t := size(fs.ThumbnailPath(ws, d.ID)) + size(fs.CoverPath(ws, d.ID))
		if d.InTrash {
			u.Trash += o.Int64 + a.Int64 + t
			continue
		}
		u.Documents++
		u.Originals, u.Searchable, u.Thumbnails = u.Originals+o.Int64, u.Searchable+a.Int64, u.Thumbnails+t
	}
	v, err := s.q.VersionSizes(ctx, ws)
	if err != nil {
		return u, err
	}
	u.Versions, u.Trash = v.Live, u.Trash+v.Trash
	u.Total = u.Originals + u.Searchable + u.Thumbnails + u.Versions + u.Trash
	s.mu.Lock()
	s.cache[ws] = u
	s.mu.Unlock()
	return u, nil
}

// DefaultBytes is KEPT_DEFAULT_QUOTA (0: no limit).
func (s *Service) DefaultBytes() int64 { return s.opt.DefaultBytes }

// Limit is the workspace's quota.
func (s *Service) Limit(ctx context.Context, ws int64) (Limit, error) {
	q, err := s.q.GetWorkspaceQuota(ctx, ws)
	if errors.Is(err, sql.ErrNoRows) {
		return Limit{}, apperr.New(apperr.KindNotFound, "找不到这个空间")
	}
	if err != nil {
		return Limit{}, err
	}
	return limitOf(q, s.opt.DefaultBytes), nil
}

func limitOf(q sql.NullInt64, def int64) Limit {
	if !q.Valid {
		return Limit{Bytes: def, Default: true}
	}
	return Limit{Bytes: q.Int64}
}

// pending adds up the room of the uploads waiting to be processed.
func (s *Service) pending(ctx context.Context, ws int64) (int64, error) {
	rows, err := s.q.PendingUploadPayloads(ctx, sql.NullInt64{Int64: ws, Valid: true})
	if err != nil {
		return 0, err
	}
	var n int64
	for _, raw := range rows {
		var p struct {
			Size int64 `json:"size"`
		}
		if json.Unmarshal([]byte(raw), &p) == nil {
			n += Estimate(p.Size)
		}
	}
	return n, nil
}

// Check refuses add more bytes when they, the uploads waiting, and what is
// stored would go over the workspace's quota.
func (s *Service) Check(ctx context.Context, ws, add int64) error {
	l, err := s.Limit(ctx, ws)
	if err != nil || l.Bytes == 0 {
		return err
	}
	u, err := s.Usage(ctx, ws)
	if err != nil {
		return err
	}
	waiting, err := s.pending(ctx, ws)
	if err != nil {
		return err
	}
	if u.Total+waiting+add > l.Bytes {
		return errExceeded(u, l)
	}
	return nil
}

func (s *Service) wsLock(ws int64) *sync.Mutex {
	s.resMu.Lock()
	defer s.resMu.Unlock()
	if s.wsLocks == nil {
		s.wsLocks, s.reserved, s.byUser = map[int64]*sync.Mutex{}, map[int64]int64{}, map[userKey]int64{}
	}
	if s.wsLocks[ws] == nil {
		s.wsLocks[ws] = &sync.Mutex{}
	}
	return s.wsLocks[ws]
}

// Reserve is Check for an upload: the room is held until release, called
// once the upload is queued (then it counts as waiting) or given up.
func (s *Service) Reserve(ctx context.Context, ws, need int64) (release func(), err error) {
	l := s.wsLock(ws)
	l.Lock()
	defer l.Unlock()
	s.resMu.Lock()
	held := s.reserved[ws]
	s.resMu.Unlock()
	if err := s.Check(ctx, ws, held+need); err != nil {
		return func() {}, err
	}
	if s.opt.Room != nil {
		if err := s.opt.Room(ctx, ws, held+need); err != nil {
			return func() {}, err
		}
	}
	// The uploader's own limit (v1.7.11); automatic sources have none.
	user := audit.ActorFrom(ctx).UserID
	if user != 0 {
		if err := s.checkMember(ctx, ws, user, s.heldBy(ws, user)+need); err != nil {
			return func() {}, err
		}
	}
	s.resMu.Lock()
	s.reserved[ws] += need
	s.byUser[userKey{ws, user}] += need
	s.resMu.Unlock()
	var once sync.Once
	return func() {
		once.Do(func() {
			s.resMu.Lock()
			s.reserved[ws] -= need
			s.byUser[userKey{ws, user}] -= need
			s.resMu.Unlock()
		})
	}, nil
}

// WorkspaceRow is a workspace in the administrator's list.
type WorkspaceRow struct {
	ID          int64
	Name        string
	Kind, Color string
	Members     int
	Owners      string
	Usage       Usage
	Limit       Limit
}

// Workspaces lists every workspace with its usage and quota.
func (s *Service) Workspaces(ctx context.Context) ([]WorkspaceRow, error) {
	rows, err := s.q.ListWorkspacesForAdmin(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]WorkspaceRow, 0, len(rows))
	for _, r := range rows {
		u, err := s.Usage(ctx, r.ID)
		if err != nil {
			return nil, err
		}
		out = append(out, WorkspaceRow{ID: r.ID, Name: r.Name, Kind: r.Kind, Color: r.Color, Members: int(r.Members), Owners: r.Owners,
			Usage: u, Limit: limitOf(r.QuotaBytes, s.opt.DefaultBytes)})
	}
	return out, nil
}

// Watch marks workspaces dirty when their documents or jobs change.
func (s *Service) Watch(ctx context.Context, bus *events.Bus) {
	sub, cancel := bus.SubscribeAll()
	defer cancel()
	for {
		select {
		case <-ctx.Done():
			return
		case ev, ok := <-sub:
			if !ok {
				return
			}
			switch ev.Type {
			case events.DocumentUpdated, events.InboxChanged, events.JobUpdated:
				s.MarkDirty(ev.WorkspaceID)
			}
		}
	}
}

// Warn tells a workspace's administrators its storage is nearly full or
// full; it reports whether they were told (no mail server: not yet).
type Warn func(ctx context.Context, ws int64, full bool, used, limit int64) (bool, error)

// Warning levels: nearly full at 90%, full at 100%; a warning is given
// again only after usage fell below 85%.
const (
	levelNearly = 90
	levelFull   = 100
	levelReset  = 85
)

// CheckWarnings tells the administrators of each workspace with a quota
// once when it passes 90% and once when it is full (v1.7.4).
func (s *Service) CheckWarnings(ctx context.Context, warn Warn) error {
	rows, err := s.db.QueryContext(ctx, `SELECT id, quota_notified FROM workspaces WHERE deleted_at IS NULL`)
	if err != nil {
		return err
	}
	type wsRow struct{ id, notified int64 }
	var list []wsRow
	for rows.Next() {
		var r wsRow
		if err := rows.Scan(&r.id, &r.notified); err != nil {
			rows.Close()
			return err
		}
		list = append(list, r)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	for _, r := range list {
		l, err := s.Limit(ctx, r.id)
		if err != nil {
			return err
		}
		var used int64
		level := int64(0)
		if l.Bytes > 0 {
			u, err := s.Usage(ctx, r.id)
			if err != nil {
				return err
			}
			used = u.Total
			switch pct := used * 100 / l.Bytes; {
			case pct >= levelFull:
				level = levelFull
			case pct >= levelNearly:
				level = levelNearly
			case pct >= levelReset && r.notified > 0:
				level = r.notified // between 85% and 90%: no change
			}
		}
		set := r.notified
		switch {
		case level > r.notified:
			told, err := warn(ctx, r.id, level == levelFull, used, l.Bytes)
			if err != nil {
				s.opt.Log.Warn("tell the administrators the storage is filling up", "workspace", r.id, "err", err)
				continue
			}
			if told {
				set = level
			}
		case level < r.notified:
			set = level
		}
		if set != r.notified {
			if _, err := s.db.ExecContext(ctx, `UPDATE workspaces SET quota_notified = ? WHERE id = ?`, set, r.id); err != nil {
				return err
			}
		}
	}
	return nil
}

// RunWarnings checks the warnings every interval until ctx ends.
func (s *Service) RunWarnings(ctx context.Context, every time.Duration, warn Warn) {
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		if err := s.CheckWarnings(ctx, warn); err != nil && ctx.Err() == nil {
			s.opt.Log.Warn("check the quota warnings", "err", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}
