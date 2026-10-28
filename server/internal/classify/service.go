package classify

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"slices"
	"sync"
	"time"

	"kept/server/internal/document"
	"kept/server/internal/jobs"
	"kept/server/internal/store/sqlc"
	"kept/server/internal/taxonomy"
)

// KindTrain is the training job: the hourly scheduler entry (retrains only
// when the training data changed) and the settings page's “重新训练”
// (payload {"force": true}).
const KindTrain = "classify.train"

// Rule is one item's matching rule.
type Rule struct {
	Kind string // type, correspondent or tag
	ID   int64
	Name string
	Rule taxonomy.MatchRule
}

type Options struct {
	Log       *slog.Logger
	Now       func() time.Time
	Queue     *jobs.Queue       // EnqueueApply, EnqueueTrain and job progress
	Documents *document.Service // re-applying classification
}

// Service trains, stores and applies the classifiers.
type Service struct {
	db  *sql.DB
	q   *sqlc.Queries
	opt Options

	mu    sync.Mutex
	cache map[int64]cached
	// neighbors finds similar documents (nil without vectors).
	neighbors NeighborFunc
}

type cached struct {
	trainedAt int64
	hash      string
	model     *Model // nil: none stored
}

func NewService(db *sql.DB, opt Options) *Service {
	if opt.Log == nil {
		opt.Log = slog.New(slog.DiscardHandler)
	}
	if opt.Now == nil {
		opt.Now = time.Now
	}
	return &Service{db: db, q: sqlc.New(db), opt: opt, cache: map[int64]cached{}}
}

// Register adds the training job's handler.
func (s *Service) Register(p *jobs.Pool) {
	p.Handle(KindTrain, func(ctx context.Context, job jobs.Job) (any, error) {
		if job.WorkspaceID == nil {
			return nil, jobs.Permanent(errors.New("classify.train needs a workspace"))
		}
		var in struct{ Force bool }
		_ = json.Unmarshal(job.Payload, &in)
		return s.Train(ctx, *job.WorkspaceID, in.Force)
	})
	p.Handle(KindApply, s.handleApply)
}

// Rules returns the workspace's matching rules and which items learn.
func (s *Service) Rules(ctx context.Context, ws int64) ([]Rule, Learners, error) {
	rules, learn, _, err := s.items(ctx, ws)
	return rules, learn, err
}

// Names are the names of the workspace's items by kind and ID.
func (s *Service) Names(ctx context.Context, ws int64) (map[string]map[int64]string, error) {
	_, _, names, err := s.items(ctx, ws)
	return names, err
}

func (s *Service) items(ctx context.Context, ws int64) ([]Rule, Learners, map[string]map[int64]string, error) {
	names := map[string]map[int64]string{"type": {}, "correspondent": {}, "person": {}, "tag": {}}
	learn := Learners{Types: map[int64]bool{}, Correspondents: map[int64]bool{}, Persons: map[int64]bool{}, Tags: map[int64]bool{}}
	var out []Rule
	add := func(kind string, id int64, name, alg, match string, ci int64, into map[int64]bool) {
		names[kind][id] = name
		if alg == taxonomy.AlgorithmAuto {
			into[id] = true
			return
		}
		if alg != taxonomy.AlgorithmNone {
			out = append(out, Rule{Kind: kind, ID: id, Name: name, Rule: taxonomy.MatchRule{Algorithm: alg, Match: match, CaseInsensitive: ci == 1}})
		}
	}
	types, err := s.q.ListTypeRules(ctx, ws)
	if err != nil {
		return nil, learn, nil, err
	}
	for _, r := range types {
		add("type", r.ID, r.Name, r.Algorithm, r.Match, r.CaseInsensitive, learn.Types)
	}
	corrs, err := s.q.ListCorrespondentRules(ctx, ws)
	if err != nil {
		return nil, learn, nil, err
	}
	for _, r := range corrs {
		add("correspondent", r.ID, r.Name, r.Algorithm, r.Match, r.CaseInsensitive, learn.Correspondents)
	}
	persons, err := s.q.ListPersonRules(ctx, ws)
	if err != nil {
		return nil, learn, nil, err
	}
	for _, r := range persons {
		add("person", r.ID, r.Name, r.Algorithm, r.Match, r.CaseInsensitive, learn.Persons)
	}
	tags, err := s.q.ListTagRules(ctx, ws)
	if err != nil {
		return nil, learn, nil, err
	}
	for _, r := range tags {
		add("tag", r.ID, r.Name, r.Algorithm, r.Match, r.CaseInsensitive, learn.Tags)
	}
	return out, learn, names, nil
}

// samples reads the training data and its fingerprint: document IDs,
// classifications and modification times, and which items learn.
func (s *Service) samples(ctx context.Context, ws int64, learn Learners) ([]Sample, string, error) {
	docs, err := s.q.ListTrainingDocuments(ctx, ws)
	if err != nil {
		return nil, "", err
	}
	tagRows, err := s.q.ListTrainingTags(ctx, ws)
	if err != nil {
		return nil, "", err
	}
	tags := map[int64][]int64{}
	for _, r := range tagRows {
		tags[r.DocumentID] = append(tags[r.DocumentID], r.TagID)
	}
	h := sha256.New()
	fmt.Fprintf(h, "v%d\n", FormatVersion)
	for _, set := range []map[int64]bool{learn.Types, learn.Correspondents, learn.Tags, learn.Persons} {
		fmt.Fprintln(h, sortedIDs(set))
	}
	out := make([]Sample, len(docs))
	for i, d := range docs {
		out[i] = Sample{Type: d.DocumentTypeID.Int64, Correspondent: d.CorrespondentID.Int64, Person: d.PersonID.Int64, Tags: tags[d.ID]}
		fmt.Fprintln(h, d.ID, d.ModifiedAt, out[i].Type, out[i].Correspondent, out[i].Tags, out[i].Person)
	}
	for i, d := range docs {
		out[i].Features = Features(Text(d.Title, d.Content))
	}
	return out, hex.EncodeToString(h.Sum(nil)), nil
}

// TrainResult is the training job's result.
type TrainResult struct {
	Trained   bool    `json:"trained"`
	Unchanged bool    `json:"unchanged,omitempty"` // the data had not changed; nothing was done
	Summary   Summary `json:"summary"`
}

// Train retrains the workspace's model, unless the training data is the
// same as last time and force is false. Too little data stores no model
// (and removes an old one) with the reason in the summary.
func (s *Service) Train(ctx context.Context, ws int64, force bool) (TrainResult, error) {
	_, learn, err := s.Rules(ctx, ws)
	if err != nil {
		return TrainResult{}, err
	}
	samples, hash, err := s.samples(ctx, ws, learn)
	if err != nil {
		return TrainResult{}, err
	}
	if !force {
		if old, err := s.q.GetClassifierSummary(ctx, ws); err == nil && old.DataHash == hash && old.FormatVersion == FormatVersion {
			var sum Summary
			_ = json.Unmarshal([]byte(old.Summary), &sum)
			return TrainResult{Unchanged: true, Trained: sum.Skipped == "", Summary: sum}, nil
		} else if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return TrainResult{}, err
		}
	}
	m, sum := Train(samples, learn)
	var blob []byte
	if m != nil {
		if blob, err = m.Encode(); err != nil {
			return TrainResult{}, err
		}
	}
	js, _ := json.Marshal(sum)
	if err := s.q.SaveClassifierModel(ctx, sqlc.SaveClassifierModelParams{
		WorkspaceID: ws, FormatVersion: FormatVersion, TrainedAt: s.opt.Now().UnixMilli(),
		DataHash: hash, DocCount: int64(len(samples)), Summary: string(js), Blob: blob,
	}); err != nil {
		return TrainResult{}, err
	}
	s.opt.Log.Info("classifier trained", "workspace", ws, "documents", len(samples), "skipped", sum.Skipped)
	return TrainResult{Trained: m != nil, Summary: sum}, nil
}

// Status is the stored model's state, for the settings page.
type Status struct {
	TrainedAt *time.Time
	Summary   Summary
	Training  bool // a training job is queued or running
}

func (s *Service) Status(ctx context.Context, ws int64) (Status, error) {
	var st Status
	row, err := s.q.GetClassifierSummary(ctx, ws)
	switch {
	case err == nil && row.FormatVersion == FormatVersion:
		t := time.UnixMilli(row.TrainedAt)
		st.TrainedAt = &t
		_ = json.Unmarshal([]byte(row.Summary), &st.Summary)
	case err != nil && !errors.Is(err, sql.ErrNoRows):
		return st, err
	}
	if st.Summary.Items == nil {
		st.Summary.Items = []ItemSummary{}
	}
	n, err := s.q.CountUnfinishedJobs(ctx, sqlc.CountUnfinishedJobsParams{Kind: KindTrain, WorkspaceID: sql.NullInt64{Int64: ws, Valid: true}})
	if err != nil {
		return st, err
	}
	st.Training = n > 0
	return st, nil
}

// Model returns the workspace's stored model, or nil if there is none.
func (s *Service) Model(ctx context.Context, ws int64) (*Model, error) {
	row, err := s.q.GetClassifierSummary(ctx, ws)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && row.FormatVersion != FormatVersion) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	c, ok := s.cache[ws]
	s.mu.Unlock()
	if ok && c.trainedAt == row.TrainedAt && c.hash == row.DataHash {
		return c.model, nil
	}
	full, err := s.q.GetClassifierModel(ctx, ws)
	if err != nil {
		return nil, err
	}
	var m *Model
	if len(full.Blob) > 0 {
		if m, err = Decode(full.Blob); err != nil {
			s.opt.Log.Warn("a stored classifier cannot be read; it is ignored until retrained", "workspace", ws, "err", err)
			m = nil
		}
	}
	s.mu.Lock()
	s.cache[ws] = cached{trainedAt: full.TrainedAt, hash: full.DataHash, model: m}
	s.mu.Unlock()
	return m, nil
}

func sortedIDs(set map[int64]bool) []int64 {
	ids := slices.Collect(maps.Keys(set))
	slices.Sort(ids)
	return ids
}
