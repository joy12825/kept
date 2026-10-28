package pages

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"

	"kept/server/internal/apperr"
	"kept/server/internal/audit"
	"kept/server/internal/document"
	"kept/server/internal/ingest"
	"kept/server/internal/jobs"
	"kept/server/internal/store/sqlc"
	"kept/server/internal/tenant"
)

// Job kinds: both make new documents through the ingest pipeline.
const (
	KindSplit = "pages.split"
	KindMerge = "pages.merge"
)

// Deps are what split and merge need besides the page editor's.
type Deps struct {
	Queue     *jobs.Queue
	Ingest    *ingest.Service
	Documents *document.Service
}

type splitPayload struct {
	DocumentID int64       `json:"documentId"`
	After      []int       `json:"after"`
	Trash      bool        `json:"trash"`
	Source     string      `json:"source"`
	Actor      audit.Actor `json:"actor"`
}

type mergePayload struct {
	IDs    []int64     `json:"ids"`
	Trash  bool        `json:"trash"`
	Source string      `json:"source"`
	Actor  audit.Actor `json:"actor"`
}

// Result lists the documents a split or merge made.
type Result struct {
	Created []int64 `json:"created"`
}

// Register handles the split and merge jobs.
func (s *Service) Register(p *jobs.Pool, d Deps) {
	s.deps = d
	p.Handle(KindSplit, s.handleSplit)
	p.Handle(KindMerge, s.handleMerge)
}

// Split queues splitting a document after the given pages (as it looks
// now); each part becomes a new document. With trash the original goes to
// the trash once every part is in.
func (s *Service) Split(ctx context.Context, scope tenant.Scope, id int64, after []int, trash bool, source string) (int64, error) {
	d, err := s.editable(ctx, scope, id)
	if err != nil {
		return 0, err
	}
	if _, err := s.Current(d); err != nil {
		return 0, err
	}
	after = sorted(after)
	if len(after) == 0 {
		return 0, apperr.New(apperr.KindInvalid, "请选择在哪里拆开")
	}
	if after[0] < 1 || after[len(after)-1] >= int(d.PageCount) {
		return 0, apperr.New(apperr.KindInvalid, fmt.Sprintf("拆分位置要在第 1 页到第 %d 页之后", d.PageCount-1))
	}
	ws := scope.WorkspaceID()
	return s.deps.Queue.Enqueue(ctx, KindSplit, splitPayload{DocumentID: id, After: after, Trash: trash, Source: source, Actor: audit.ActorFrom(ctx)},
		jobs.EnqueueOptions{WorkspaceID: &ws, MaxAttempts: 1})
}

// Merge queues merging documents, in the order given, into a new one.
func (s *Service) Merge(ctx context.Context, scope tenant.Scope, ids []int64, trash bool, source string) (int64, error) {
	seen := map[int64]bool{}
	for _, id := range ids {
		if seen[id] {
			return 0, apperr.New(apperr.KindInvalid, "同一份文档只能出现一次")
		}
		seen[id] = true
	}
	if len(ids) < 2 {
		return 0, apperr.New(apperr.KindInvalid, "至少选择两份文档")
	}
	var private sql.NullInt64 // the person of the first, if private
	for i, id := range ids {
		d, err := s.editable(ctx, scope, id)
		if err != nil {
			return 0, err
		}
		// private documents merge only with those of the same person (v1.8.0)
		p := sql.NullInt64{Int64: d.PersonID.Int64, Valid: d.Private == 1}
		if i == 0 {
			private = p
		} else if p != private {
			return 0, apperr.New(apperr.KindInvalid, "家人的私密文档只能和同一位家人的私密文档合并")
		}
		if _, err := s.Current(d); err != nil {
			return 0, apperr.New(apperr.KindInvalid, fmt.Sprintf("“%s”不是 PDF，不能合并", d.Title))
		}
	}
	ws := scope.WorkspaceID()
	return s.deps.Queue.Enqueue(ctx, KindMerge, mergePayload{IDs: ids, Trash: trash, Source: source, Actor: audit.ActorFrom(ctx)},
		jobs.EnqueueOptions{WorkspaceID: &ws, MaxAttempts: 1})
}

// overrides carry a document's metadata over to the documents made from it.
func (s *Service) overrides(ctx context.Context, d sqlc.Document) (ingest.Overrides, error) {
	tags, err := s.q.ListDocumentTagIDs(ctx, d.ID)
	if err != nil {
		return ingest.Overrides{}, err
	}
	return ingest.Overrides{DocumentTypeID: d.DocumentTypeID.Int64, CorrespondentID: d.CorrespondentID.Int64, TagIDs: tags, CreatedDate: d.CreatedDate,
		PersonID: d.PersonID.Int64, Private: d.Private == 1}, nil
}

func (s *Service) handleSplit(ctx context.Context, job jobs.Job) (any, error) {
	var pl splitPayload
	if err := json.Unmarshal(job.Payload, &pl); err != nil {
		return nil, jobs.Permanent(err)
	}
	ws := *job.WorkspaceID
	scope := tenant.New(ws, tenant.RoleOwner)
	ctx = audit.WithActor(ctx, pl.Actor)
	d, err := s.get(ctx, tenant.New(ws, tenant.RoleOwner), pl.DocumentID)
	if err != nil {
		return nil, jobs.Permanent(err)
	}
	ref, err := s.Current(d)
	if err != nil {
		return nil, jobs.Permanent(err)
	}
	ov, err := s.overrides(ctx, d)
	if err != nil {
		return nil, err
	}
	tmp, err := s.files.NewTmpDir("split-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmp)
	src, release, err := s.files.Local(ctx, d.Storage, ref, tmp)
	if err != nil {
		return nil, err
	}
	defer release()
	bounds := append(slices.Clone(pl.After), int(d.PageCount))
	var res Result
	from := 1
	for i, to := range bounds {
		part := filepath.Join(tmp, "part-"+strconv.Itoa(i+1)+".pdf")
		if err := qpdf(ctx, "--empty", "--pages", src, fmt.Sprintf("%d-%d", from, to), "--", part); err != nil {
			return res, jobs.Permanent(err)
		}
		from = to + 1
		pov := ov
		pov.Title = fmt.Sprintf("%s（%d/%d）", d.Title, i+1, len(bounds))
		id, err := s.importFile(ctx, scope, part, pov, pl.Source)
		if err != nil {
			return res, jobs.Permanent(err)
		}
		res.Created = append(res.Created, id)
	}
	if pl.Trash {
		if _, err := s.deps.Documents.Delete(ctx, scope, document.Selection{IDs: []int64{d.ID}}); err != nil {
			return res, err
		}
	}
	return res, nil
}

func (s *Service) handleMerge(ctx context.Context, job jobs.Job) (any, error) {
	var pl mergePayload
	if err := json.Unmarshal(job.Payload, &pl); err != nil {
		return nil, jobs.Permanent(err)
	}
	ws := *job.WorkspaceID
	scope := tenant.New(ws, tenant.RoleOwner)
	ctx = audit.WithActor(ctx, pl.Actor)
	tmp, err := s.files.NewTmpDir("merge-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmp)
	args := []string{"--empty", "--pages"}
	var first sqlc.Document
	var tags []int64
	for i, id := range pl.IDs {
		d, err := s.get(ctx, tenant.New(ws, tenant.RoleOwner), id)
		if err != nil {
			return nil, jobs.Permanent(err)
		}
		ref, err := s.Current(d)
		if err != nil {
			return nil, jobs.Permanent(err)
		}
		src, release, err := s.files.Local(ctx, d.Storage, ref, tmp)
		if err != nil {
			return nil, err
		}
		defer release()
		args = append(args, src, "1-z")
		ov, err := s.overrides(ctx, d)
		if err != nil {
			return nil, err
		}
		if i == 0 {
			first = d
		}
		for _, t := range ov.TagIDs {
			if !slices.Contains(tags, t) {
				tags = append(tags, t)
			}
		}
	}
	out := filepath.Join(tmp, "merged.pdf")
	if err := qpdf(ctx, append(args, "--", out)...); err != nil {
		return nil, jobs.Permanent(err)
	}
	ov, err := s.overrides(ctx, first)
	if err != nil {
		return nil, err
	}
	ov.TagIDs, ov.Title = tags, first.Title
	id, err := s.importFile(ctx, scope, out, ov, pl.Source)
	if err != nil {
		return nil, jobs.Permanent(err)
	}
	if pl.Trash {
		if _, err := s.deps.Documents.Delete(ctx, scope, document.Selection{IDs: pl.IDs}); err != nil {
			return Result{Created: []int64{id}}, err
		}
	}
	return Result{Created: []int64{id}}, nil
}

func (s *Service) importFile(ctx context.Context, scope tenant.Scope, path string, ov ingest.Overrides, source string) (int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	return s.deps.Ingest.Import(ctx, scope, ov.Title+".pdf", f, ov, ingest.Upload{Source: source})
}

// TitleOf returns a document's title (task summaries).
func (s *Service) TitleOf(ctx context.Context, ws, id int64) string {
	d, err := s.q.GetDocument(ctx, sqlc.GetDocumentParams{WorkspaceID: ws, ID: id, Viewer: tenant.ViewerSystem})
	if err != nil {
		return ""
	}
	return d.Title
}
