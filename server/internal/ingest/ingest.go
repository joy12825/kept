// Package ingest brings files into the archive (design doc part 4): it
// receives an upload, then a background job parses it, runs OCR, makes a
// thumbnail, finds the document date and commits the document.
package ingest

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"kept/server/internal/i18n"
	"kept/server/internal/quota"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"kept/server/internal/apperr"
	"kept/server/internal/audit"
	"kept/server/internal/barcode"
	"kept/server/internal/books"
	"kept/server/internal/classify"
	"kept/server/internal/document"
	"kept/server/internal/events"
	"kept/server/internal/exif"
	"kept/server/internal/files"
	"kept/server/internal/jobs"
	"kept/server/internal/parser"
	"kept/server/internal/pinyin"
	"kept/server/internal/privacy"
	"kept/server/internal/store"
	"kept/server/internal/store/sqlc"
	"kept/server/internal/tenant"
)

// Job kinds.
const (
	KindDocument  = "ingest.document"
	KindReprocess = "ingest.reprocess"
)

// KindPrefix selects both kinds when listing jobs.
const KindPrefix = "ingest."

// Classifier suggests a new document's type, correspondent and tags
// (classify.Service).
type Classifier interface {
	Suggest(ctx context.Context, ws int64, title, content string) (classify.Suggestion, error)
}

// Quota refuses files that would take a workspace over its limit (v1.0).
type Quota interface {
	Check(ctx context.Context, ws, add int64) error
	// Reserve checks and holds room until release (v1.7.4).
	Reserve(ctx context.Context, ws, need int64) (release func(), err error)
}

type Options struct {
	Parser        parser.Config
	Quota         Quota           // nil: no limits
	Classifier    Classifier      // nil: no automatic classification
	Placer        document.Placer // nil: new documents go into no drawer
	MaxUploadSize int64
	Now           func() time.Time
	Log           *slog.Logger
	// AfterReprocess runs once a document's files were rebuilt from its
	// original (pages.Service.Reapply puts page edits back).
	AfterReprocess func(ctx context.Context, ws, id int64) error
	// Barcodes: separator sheets split a PDF into several documents, ASN
	// labels fill in the archive serial number (KEPT_BARCODES).
	Barcodes       bool
	BarcodeOptions barcode.Options
	// Workflows fills in “on consumption” workflows' values before
	// classification and hears about every new document.
	Workflows Workflows
	// OnAdded runs after a new document is committed (the intake agent).
	OnAdded func(ctx context.Context, ws, docID int64)
	// SetFields writes the custom fields given at upload (v1.5.8), after
	// the document is committed and before workflows hear of it.
	SetFields func(ctx context.Context, ws, docID int64, fields []FieldPreset) error
}

// Workflows is what ingesting needs from package workflow.
type Workflows interface {
	// Presets returns what the matching consumption workflows set.
	Presets(ctx context.Context, ws int64, in ConsumeInfo) (WorkflowPresets, error)
	// Added runs after a new document is committed: ran are the
	// consumption workflows that applied to it.
	Added(ctx context.Context, ws, docID int64, ran []int64)
}

// LateWorkflows forwards to workflows made after ingesting is (v1.13: the
// paid module, wired last); until then, or without them, nothing is preset.
type LateWorkflows struct{ W Workflows }

func (l *LateWorkflows) Presets(ctx context.Context, ws int64, in ConsumeInfo) (WorkflowPresets, error) {
	if l.W == nil {
		return WorkflowPresets{}, nil
	}
	return l.W.Presets(ctx, ws, in)
}

func (l *LateWorkflows) Added(ctx context.Context, ws, docID int64, ran []int64) {
	if l.W != nil {
		l.W.Added(ctx, ws, docID, ran)
	}
}

// ConsumeInfo is what is known about a document before it is written.
type ConsumeInfo struct {
	Filename, Source, Title, Content string
}

// WorkflowPresets are consumption workflows' values. Names maps a field
// (title, type, correspondent, person, drawer, or the tag ID) to the workflow
// that set it, recorded as the field's source.
type WorkflowPresets struct {
	Title           string
	DocumentTypeID  int64
	CorrespondentID int64
	PersonID        int64 // v1.6.3
	TagIDs          []int64
	DrawerID        int64
	Names           map[string]string
	Ran             []int64
}

type Service struct {
	db    *sql.DB
	q     *sqlc.Queries
	queue *jobs.Queue
	files *files.Store
	bus   *events.Bus
	opt   Options

	failMu    sync.Mutex
	failHooks []func(FailedUpload)
}

func NewService(db *sql.DB, queue *jobs.Queue, fs *files.Store, bus *events.Bus, opt Options) *Service {
	if opt.Now == nil {
		opt.Now = time.Now
	}
	if opt.Log == nil {
		opt.Log = slog.New(slog.DiscardHandler)
	}
	if opt.MaxUploadSize <= 0 {
		opt.MaxUploadSize = 200 << 20
	}
	return &Service{db: db, q: sqlc.New(db), queue: queue, files: fs, bus: bus, opt: opt}
}

// ChecklistItem names an item of a checklist (v1.6.7).
type ChecklistItem struct {
	ID     int64  `json:"id"`
	ItemID string `json:"item"`
}

// Overrides are what the uploader chose in advance; IDs that do not belong
// to the workspace are ignored.
type Overrides struct {
	DocumentTypeID  int64   `json:"documentTypeId,omitempty"`
	CorrespondentID int64   `json:"correspondentId,omitempty"`
	PersonID        int64   `json:"personId,omitempty"` // v1.6.3
	Title           string  `json:"title,omitempty"`
	DrawerID        int64   `json:"drawerId,omitempty"` // put in by hand, like choosing it on the web
	TagIDs          []int64 `json:"tagIds,omitempty"`
	CreatedDate     string  `json:"createdDate,omitempty"` // YYYY-MM-DD; "" finds the date in the text
	ASN             *int64  `json:"asn,omitempty"`         // set when free (an ASN barcode)
	// Checklist puts the new document into an item of a checklist of the
	// workspace by hand (v1.6.7: uploaded from the checklist page).
	Checklist *ChecklistItem `json:"checklist,omitempty"`
	// CustomFields are set right after the document is written (v1.5.8,
	// given at upload by the app or a script); invalid ones are logged.
	CustomFields []FieldPreset `json:"customFields,omitempty"`
	// Private keeps the document private to PersonID (v1.8.0): the parts
	// of a private document split apart.
	Private bool `json:"private,omitempty"`
}

// FieldPreset is a custom field's value given at upload.
type FieldPreset struct {
	FieldID int64           `json:"fieldId"`
	Value   json.RawMessage `json:"value"`
}

// Upload describes the upload itself rather than the document.
type Upload struct {
	// Source is recorded on the document: web, api or mobile ("" means web).
	Source string
	// ClientUploadID makes the upload idempotent within the workspace: an
	// ID seen before returns the earlier job (design doc 9.4).
	ClientUploadID string
	// Origin is where the file came from, e.g. its path in the consume
	// directory; failure hooks receive it.
	Origin string
	// Path is the file's path in its workspace's consume folder, kept on
	// the document so the same path again can become a new version.
	Path string
	// Password opens a password-protected PDF. It is used while receiving
	// the file and never stored.
	Password string
}

// FailedUpload is an upload whose processing failed for good. Path is the
// received file, still in place while the hooks run.
type FailedUpload struct {
	WorkspaceID int64
	Filename    string
	Source      string
	Origin      string
	Path        string
	Reason      string
}

// OnFailed registers fn to run when an upload fails for good, before its
// received file is removed or kept for a retry (the consume directory keeps
// such files).
func (s *Service) OnFailed(fn func(FailedUpload)) {
	s.failMu.Lock()
	s.failHooks = append(s.failHooks, fn)
	s.failMu.Unlock()
}

// Document sources (documents.source).
const (
	SourceWeb    = "web"
	SourceAPI    = "api"
	SourceMobile = "mobile"
	SourceWatch  = "watch"  // the consume directory
	SourceMail   = "mail"   // a mailbox (IMAP)
	SourceLink   = "link"   // an upload link (v1.6.2)
	SourceRemote = "remote" // a remote inbox (v1.7.7)
)

// payload is stored with the job. The actor is kept so the audit entry
// written by the background job names the person who uploaded.
type payload struct {
	release    func()      // gives back the quota room held for the upload (not stored)
	Dir        string      `json:"dir"`
	Filename   string      `json:"filename"`
	Checksum   string      `json:"checksum"`
	Size       int64       `json:"size"`
	Overrides  Overrides   `json:"overrides"`
	Source     string      `json:"source,omitempty"`
	Origin     string      `json:"origin,omitempty"`
	Path       string      `json:"path,omitempty"`      // in the consume folder (v1.7.13)
	Decrypted  bool        `json:"decrypted,omitempty"` // the original is encrypted; parse decrypted.pdf
	Actor      audit.Actor `json:"actor"`
	DocumentID int64       `json:"documentId,omitempty"` // reprocess
}

// Progress is a job's state as shown in the upload tray.
type Progress struct {
	Stage      string   `json:"stage"` // queued, inspect, ocr, thumbnail, saving, done
	Progress   int      `json:"progress"`
	DocumentID int64    `json:"documentId,omitempty"`
	Warnings   []string `json:"warnings,omitempty"`
}

var (
	errEmpty            = apperr.New(apperr.KindInvalid, "文件是空的")
	errUploadGone       = apperr.New(apperr.KindInvalid, "上传的文件已不存在，请重新上传")
	errDocumentGone     = apperr.New(apperr.KindNotFound, "文档已被删除")
	errOriginalGone     = apperr.New(apperr.KindInvalid, "原件已丢失，无法重新处理")
	errDuplicate        = apperr.WithCode(apperr.KindConflict, "duplicate", "这份文件已经存在")
	errDuplicateInTrash = apperr.WithCode(apperr.KindConflict, "duplicate", "这份文件在回收站中，可以从回收站恢复")
	errNeedsPassword    = apperr.WithCode(apperr.KindInvalid, "pdf_encrypted", "这个 PDF 有密码，请输入密码后重新上传")
	errWrongPassword    = apperr.WithCode(apperr.KindInvalid, "wrong_password", "PDF 的密码不正确")
)

func errTooLarge(max int64) error {
	return apperr.New(apperr.KindInvalid, fmt.Sprintf("文件超过 %d MB 的上限", max>>20))
}

// Submit receives an upload: it streams the file to a working directory,
// hashing it as it goes, rejects duplicates, and queues the ingest job.
func (s *Service) Submit(ctx context.Context, scope tenant.Scope, filename string, r io.Reader, ov Overrides, up Upload) (int64, error) {
	ws := scope.WorkspaceID()
	if up.ClientUploadID != "" {
		if len(up.ClientUploadID) > 64 {
			return 0, apperr.New(apperr.KindInvalid, "clientUploadId 不能超过 64 个字符")
		}
		if id, ok, err := s.jobForClientUpload(ctx, ws, up.ClientUploadID); err != nil {
			return 0, err
		} else if ok {
			// The phone's "重试" after the processing failed sends the same
			// upload again: a failed job gives up its ID and the upload starts
			// over (v1.9.9); otherwise the same upload is the same job.
			j, err := s.q.GetJob(ctx, id)
			if err != nil {
				return 0, err
			}
			if j.Status != "failed" {
				return id, nil
			}
			if err := s.q.ReleaseClientUploadID(ctx, id); err != nil {
				return 0, err
			}
			// the phone sends the file again: the copy kept for a retry can go
			if dir := s.workDir(j.Payload); dir != "" {
				os.RemoveAll(dir)
			}
		}
	}
	pl, dir, err := s.receive(ctx, scope, filename, r, ov, up)
	if err != nil {
		return 0, err
	}
	pl.Source, pl.Origin, pl.Path = up.Source, up.Origin, up.Path
	id, err := s.queue.Enqueue(ctx, KindDocument, pl, jobs.EnqueueOptions{WorkspaceID: &ws, ClientUploadID: up.ClientUploadID})
	pl.done() // queued: the job counts as waiting now
	if err != nil {
		os.RemoveAll(dir)
		if up.ClientUploadID != "" && isUniqueViolation(err) {
			// The same upload arrived twice at once; the other one won.
			if id, ok, lerr := s.jobForClientUpload(ctx, ws, up.ClientUploadID); lerr == nil && ok {
				return id, nil
			}
		}
		return 0, err
	}
	s.publishJob(ws, id, "queued", Progress{Stage: "queued", Progress: 0}, "")
	return id, nil
}

func (s *Service) jobForClientUpload(ctx context.Context, ws int64, clientID string) (int64, bool, error) {
	j, err := s.q.GetJobByClientUploadID(ctx, sqlc.GetJobByClientUploadIDParams{
		WorkspaceID: sql.NullInt64{Int64: ws, Valid: true}, ClientUploadID: sql.NullString{String: clientID, Valid: true},
	})
	if errors.Is(err, sql.ErrNoRows) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, err
	}
	return j.ID, true, nil
}

// UploadStatus is a phone upload's progress, looked up by its client ID.
type UploadStatus struct {
	ClientUploadID string
	Status         string // queued, running, succeeded, failed, unknown
	DocumentID     int64
	Error          string
}

// Statuses reports on uploads by their client IDs, in the order given.
func (s *Service) Statuses(ctx context.Context, scope tenant.Scope, clientIDs []string) ([]UploadStatus, error) {
	if len(clientIDs) > 200 {
		return nil, apperr.New(apperr.KindInvalid, "一次最多查询 200 个上传")
	}
	out := make([]UploadStatus, len(clientIDs))
	for i, cid := range clientIDs {
		out[i] = UploadStatus{ClientUploadID: cid, Status: "unknown"}
		if cid == "" {
			continue
		}
		j, err := s.q.GetJobByClientUploadID(ctx, sqlc.GetJobByClientUploadIDParams{
			WorkspaceID: sql.NullInt64{Int64: scope.WorkspaceID(), Valid: true}, ClientUploadID: sql.NullString{String: cid, Valid: true},
		})
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return nil, err
		}
		out[i].Status = j.Status
		if j.Status == "failed" {
			out[i].Error = JobErrorMessage(j.Error.String)
		}
		if j.Result.Valid {
			var p Progress
			if json.Unmarshal([]byte(j.Result.String), &p) == nil {
				out[i].DocumentID = p.DocumentID
			}
		}
	}
	return out, nil
}

// AcceptedTypes lists the MIME types uploads may have (Office documents
// and e-mails also without a conversion service, v1.9.1).
func (s *Service) AcceptedTypes() []string {
	return parser.AcceptedTypes()
}

// MaxUploadSize is the largest file an upload may be.
func (s *Service) MaxUploadSize() int64 { return s.opt.MaxUploadSize }

// Import ingests a file right away, without the job queue, and returns the
// new document's ID (used by `kept demo load`).
func (s *Service) Import(ctx context.Context, scope tenant.Scope, filename string, r io.Reader, ov Overrides, up Upload) (int64, error) {
	pl, dir, err := s.receive(ctx, scope, filename, r, ov, up)
	if err != nil {
		return 0, err
	}
	pl.Source, pl.Origin, pl.Path = up.Source, up.Origin, up.Path
	defer pl.done()
	defer os.RemoveAll(dir)
	return s.process(ctx, scope.WorkspaceID(), pl, dir, func(string, int) {})
}

// receive stores an upload in a fresh working directory and checks it.
func (s *Service) receive(ctx context.Context, scope tenant.Scope, filename string, r io.Reader, ov Overrides, up Upload) (payload, string, error) {
	filename = cleanFilename(filename)
	dir, err := s.files.NewTmpDir("upload-")
	if err != nil {
		return payload{}, "", err
	}
	release := func() {}
	fail := func(err error) (payload, string, error) {
		release()
		os.RemoveAll(dir)
		return payload{}, "", err
	}
	f, err := os.Create(filepath.Join(dir, "original"))
	if err != nil {
		return fail(err)
	}
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(f, h), io.LimitReader(r, s.opt.MaxUploadSize+1))
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return fail(err)
	}
	if n > s.opt.MaxUploadSize {
		return fail(errTooLarge(s.opt.MaxUploadSize))
	}
	if n == 0 {
		return fail(errEmpty)
	}
	sum := hex.EncodeToString(h.Sum(nil))
	if dup, err := s.q.FindDocumentByChecksum(ctx, sqlc.FindDocumentByChecksumParams{WorkspaceID: scope.WorkspaceID(), Checksum: sum}); err == nil {
		if _, err := s.q.GetDocumentAnyState(ctx, sqlc.GetDocumentAnyStateParams{WorkspaceID: scope.WorkspaceID(), ID: dup.ID, Viewer: scope.Viewer()}); errors.Is(err, sql.ErrNoRows) {
			// a family member's private document: refused, without saying which (v1.8.0)
			return fail(errDuplicate)
		} else if err != nil {
			return fail(err)
		}
		if dup.DeletedAt.Valid {
			return fail(errDuplicateInTrash.With(map[string]any{"documentId": dup.ID, "inTrash": true}))
		}
		return fail(errDuplicate.With(map[string]any{"documentId": dup.ID}))
	} else if !errors.Is(err, sql.ErrNoRows) {
		return fail(err)
	}
	if s.opt.Quota != nil {
		// Room for the original and the searchable PDF, held until queued;
		// after the duplicate check: a duplicate takes no room.
		if release, err = s.opt.Quota.Reserve(ctx, scope.WorkspaceID(), quota.Estimate(n)); err != nil {
			return fail(err)
		}
	}
	pl := payload{Dir: filepath.Base(dir), Filename: filename, Checksum: sum, Size: n, Overrides: ov, Actor: audit.ActorFrom(ctx)}
	// A password-protected PDF is opened now, while the password is at
	// hand; the original stays as uploaded.
	if parser.IsEncryptedPDF(ctx, filepath.Join(dir, "original")) {
		if up.Password == "" {
			return fail(errNeedsPassword)
		}
		if err := parser.DecryptPDF(ctx, filepath.Join(dir, "original"), filepath.Join(dir, "decrypted.pdf"), up.Password); err != nil {
			if errors.Is(err, parser.ErrWrongPassword) {
				return fail(errWrongPassword)
			}
			return fail(err)
		}
		pl.Decrypted = true
	}
	pl.release = release
	return pl, dir, nil
}

// Reprocess queues a re-run of parsing, OCR and the thumbnail for a
// document; its metadata stays as it is.
func (s *Service) Reprocess(ctx context.Context, scope tenant.Scope, documentID int64) (int64, error) {
	if _, err := s.q.GetDocument(ctx, sqlc.GetDocumentParams{WorkspaceID: scope.WorkspaceID(), ID: documentID, Viewer: scope.Viewer()}); errors.Is(err, sql.ErrNoRows) {
		return 0, apperr.New(apperr.KindNotFound, "找不到这份文档，它可能已被删除")
	} else if err != nil {
		return 0, err
	}
	ws := scope.WorkspaceID()
	id, err := s.queue.Enqueue(ctx, KindReprocess, payload{DocumentID: documentID, Actor: audit.ActorFrom(ctx)}, jobs.EnqueueOptions{WorkspaceID: &ws})
	if err != nil {
		return 0, err
	}
	s.publishJob(ws, id, "queued", Progress{Stage: "queued"}, "")
	return id, nil
}

// Register adds the job handlers and the hook that reports their outcome.
func (s *Service) Register(pool *jobs.Pool) {
	pool.Handle(KindDocument, s.handleDocument)
	pool.Handle(KindReprocess, s.handleReprocess)
	pool.Handle(KindConvertPending, s.handleConvertPending)
	pool.OnFinish(func(job jobs.Job, o jobs.Outcome) {
		if job.Kind != KindDocument && job.Kind != KindReprocess || job.WorkspaceID == nil {
			return
		}
		var p Progress
		if r, ok := o.Result.(Progress); ok {
			p = r
		}
		errMsg := ""
		if o.Err != nil {
			errMsg = userMessage(o.Err)
		}
		s.publishJob(*job.WorkspaceID, job.ID, o.Status, p, errMsg)
		if o.Status == "succeeded" && job.Kind == KindDocument {
			s.bus.Publish(events.Event{Type: events.InboxChanged, WorkspaceID: *job.WorkspaceID})
		}
		if o.Status != "retrying" {
			var pl payload
			if json.Unmarshal(job.Payload, &pl) == nil && pl.Dir != "" {
				dir := filepath.Join(s.files.TmpRoot(), filepath.Base(pl.Dir))
				if o.Status == "failed" && job.Kind == KindDocument {
					s.failMu.Lock()
					hooks := slices.Clone(s.failHooks)
					s.failMu.Unlock()
					for _, h := range hooks {
						h(FailedUpload{WorkspaceID: *job.WorkspaceID, Filename: pl.Filename, Source: pl.Source, Origin: pl.Origin,
							Path: filepath.Join(dir, "original"), Reason: errMsg})
					}
					if keepFailed(o.Err) {
						return // kept for a retry (v1.9.12); PurgeFailed removes it later
					}
				}
				os.RemoveAll(dir)
			}
		}
	})
}

// CleanTmp removes working directories that no unfinished job needs, nor
// a failed upload kept for a retry; run at start-up (design doc 4.3).
func (s *Service) CleanTmp(ctx context.Context) (int, error) {
	payloads, err := s.queue.UnfinishedPayloads(ctx)
	if err != nil {
		return 0, err
	}
	// failed uploads kept for a retry (v1.9.12)
	failed, err := s.q.ListFailedPayloadsSince(ctx, sqlc.ListFailedPayloadsSinceParams{Kind: KindDocument, Since: sql.NullInt64{Int64: time.Now().Add(-FailedRetention).UnixMilli(), Valid: true}})
	if err != nil {
		return 0, err
	}
	payloads = append(payloads, failed...)
	keep := map[string]bool{}
	for _, raw := range payloads {
		var pl payload
		if json.Unmarshal([]byte(raw), &pl) == nil && pl.Dir != "" {
			keep[filepath.Base(pl.Dir)] = true
		}
	}
	return s.files.CleanTmp(func(name string) bool { return keep[name] })
}

func (s *Service) handleDocument(ctx context.Context, job jobs.Job) (any, error) {
	var pl payload
	if err := json.Unmarshal(job.Payload, &pl); err != nil {
		return nil, jobs.Permanent(err)
	}
	ws := *job.WorkspaceID
	ctx = audit.WithActor(ctx, pl.Actor)
	dir := filepath.Join(s.files.TmpRoot(), filepath.Base(pl.Dir))
	if _, err := os.Stat(filepath.Join(dir, "original")); err != nil {
		return nil, jobs.Permanent(errUploadGone)
	}
	var warnings []string
	id, err := s.processWithWarnings(ctx, ws, pl, dir, s.progressFunc(ctx, ws, job.ID), &warnings)
	if errors.Is(err, errDuplicate) {
		if existing, ferr := s.q.FindDocumentByChecksum(ctx, sqlc.FindDocumentByChecksumParams{WorkspaceID: ws, Checksum: pl.Checksum}); ferr == nil && !s.isPrivate(ctx, ws, existing.ID) {
			_ = s.queue.SetDocument(ctx, job.ID, existing.ID)
		}
		return nil, jobs.Permanent(err)
	}
	if err != nil {
		return nil, err
	}
	if err := s.queue.SetDocument(ctx, job.ID, id); err != nil {
		s.opt.Log.Warn("link job to document", "job", job.ID, "err", err)
	}
	return Progress{Stage: "done", Progress: 100, DocumentID: id, Warnings: warnings}, nil
}

// process parses the received file in dir and commits the document.
func (s *Service) process(ctx context.Context, ws int64, pl payload, dir string, progress parser.Progress) (int64, error) {
	return s.processWithWarnings(ctx, ws, pl, dir, progress, nil)
}

func (s *Service) processWithWarnings(ctx context.Context, ws int64, pl payload, dir string, progress parser.Progress, warnings *[]string) (int64, error) {
	if warnings == nil {
		warnings = new([]string)
	}
	src := filepath.Join(dir, "original")
	progress("inspect", 10)
	parseSrc := src
	if pl.Decrypted {
		parseSrc = filepath.Join(dir, "decrypted.pdf")
	}
	if s.opt.Barcodes {
		plan, err := s.barcodes(ctx, parseSrc, pl.Filename, dir)
		if err != nil {
			*warnings = append(*warnings, "条码识别失败："+err.Error())
		} else if plan.Split() {
			return s.processParts(ctx, ws, pl, parseSrc, dir, plan, progress, warnings)
		} else if len(plan.Parts) == 1 && plan.Parts[0].ASN != nil {
			pl.Overrides.ASN = plan.Parts[0].ASN
		}
	}
	res, err := parser.Parse(ctx, s.opt.Parser, parseSrc, pl.Filename, dir, progress)
	if err != nil {
		return 0, wrapParseError(err)
	}
	if pl.Decrypted && res.ArchivePath == "" {
		res.ArchivePath = parseSrc // previews and search read the decrypted copy
	}
	*warnings = append(*warnings, res.Warnings...)
	progress("saving", 95)
	id, err := s.commit(ctx, ws, pl, src, res, warnings)
	if err == nil && res.Email != nil {
		s.submitEmailFiles(ctx, ws, pl, res.Email, warnings)
	}
	return id, err
}

// submitEmailFiles makes documents of an e-mail's files (v1.5.1), with the
// e-mail's presets but their own titles and dates. A file that cannot be
// taken (an unsupported type, a duplicate) is noted, not an error.
func (s *Service) submitEmailFiles(ctx context.Context, ws int64, pl payload, e *parser.EmailMeta, warnings *[]string) {
	ov := pl.Overrides
	ov.Title, ov.CreatedDate, ov.ASN, ov.CustomFields = "", "", nil, nil
	scope := tenant.New(ws, tenant.RoleOwner)
	for _, f := range e.Attachments {
		_, err := s.Submit(ctx, scope, f.Name, bytes.NewReader(f.Data), ov, Upload{Source: pl.Source, Origin: "eml:" + pl.Filename + "/" + f.Name})
		if err != nil {
			msg := err.Error()
			var ae *apperr.Error
			if errors.As(err, &ae) {
				msg = ae.Message
			}
			*warnings = append(*warnings, fmt.Sprintf("邮件的附件“%s”没有入库：%s", f.Name, msg))
		}
	}
}

// barcodes reads the codes of a PDF upload (other files: an empty plan).
func (s *Service) barcodes(ctx context.Context, src, filename, dir string) (barcode.Plan, error) {
	if mime, _, err := parser.Detect(src, filename); err != nil || mime != "application/pdf" {
		return barcode.Plan{}, nil
	}
	pages, err := parser.PDFPages(ctx, src)
	if err != nil {
		return barcode.Plan{}, nil // the parser reports a damaged PDF
	}
	codes, err := barcode.Scan(ctx, src, pages, dir, s.opt.BarcodeOptions)
	if err != nil {
		return barcode.Plan{}, err
	}
	return barcode.Analyze(codes, pages, s.opt.BarcodeOptions), nil
}

// processParts stores each run of pages between separator sheets as its
// own document; the upload itself is not kept. It returns the first one.
func (s *Service) processParts(ctx context.Context, ws int64, pl payload, src, dir string, plan barcode.Plan, progress parser.Progress, warnings *[]string) (int64, error) {
	base := titleFrom(pl.Filename)
	n := len(plan.Parts)
	var first int64
	made := 0
	for i, part := range plan.Parts {
		partDir := filepath.Join(dir, fmt.Sprintf("part-%d", i+1))
		if err := os.MkdirAll(partDir, 0o755); err != nil {
			return 0, err
		}
		out := filepath.Join(partDir, "part.pdf")
		if err := splitPages(ctx, src, out, part.From, part.To); err != nil {
			return 0, err
		}
		sum, err := fileSHA256(out)
		if err != nil {
			return 0, err
		}
		ppl := pl
		ppl.Filename = fmt.Sprintf("%s（%d/%d）.pdf", base, i+1, n)
		ppl.Checksum, ppl.Decrypted = sum, false
		ppl.Overrides.ASN = part.ASN
		if ppl.Overrides.Title != "" {
			ppl.Overrides.Title = fmt.Sprintf("%s（%d/%d）", ppl.Overrides.Title, i+1, n)
		}
		if _, err := s.q.FindDocumentByChecksum(ctx, sqlc.FindDocumentByChecksumParams{WorkspaceID: ws, Checksum: sum}); err == nil {
			*warnings = append(*warnings, fmt.Sprintf("第 %d–%d 页已经存在，没有重复保存", part.From, part.To))
			continue
		}
		res, err := parser.Parse(ctx, s.opt.Parser, out, ppl.Filename, partDir, func(string, int) {})
		if err != nil {
			return 0, wrapParseError(err)
		}
		*warnings = append(*warnings, res.Warnings...)
		id, err := s.commit(ctx, ws, ppl, out, res, warnings)
		if err != nil {
			return 0, err
		}
		if first == 0 {
			first = id
		}
		made++
		progress("saving", 30+60*(i+1)/n)
	}
	*warnings = append(*warnings, fmt.Sprintf("按分隔页拆成 %d 份", made))
	if first == 0 {
		return 0, errDuplicate
	}
	return first, nil
}

// splitPages writes pages from..to of src to dst.
func splitPages(ctx context.Context, src, dst string, from, to int) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	out, err := exec.CommandContext(ctx, "qpdf", "--empty", "--pages", src, fmt.Sprintf("%d-%d", from, to), "--", dst).CombinedOutput()
	var ee *exec.ExitError
	if err != nil && !(errors.As(err, &ee) && ee.ExitCode() == 3) {
		return fmt.Errorf("qpdf: %v: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// commit moves the files into place and writes the document in one
// transaction. The files are renamed inside the transaction, once the ID
// is known; renames within the data directory are instant, so the write
// lock is not held for long. On failure the moved files are removed.
func (s *Service) commit(ctx context.Context, ws int64, pl payload, src string, res parser.Result, warnings *[]string) (int64, error) {
	now := s.opt.Now()
	title := titleFrom(pl.Filename)
	if res.Book != nil && res.Book.Title != "" { // an e-book names itself
		title = truncate(res.Book.Title, 200)
	}
	if res.Email != nil && strings.TrimSpace(res.Email.Subject) != "" { // so does an e-mail (v1.5.1)
		title = truncate(res.Email.Subject, 200)
	}
	if t := strings.TrimSpace(pl.Overrides.Title); t != "" {
		if r := []rune(t); len(r) > 200 {
			t = string(r[:200])
		}
		title = t
	}
	source := pl.Source
	if source == "" {
		source = SourceWeb
	}
	// Consumption workflows come before classification, so what they set
	// wins over it; what the uploader chose still wins over them.
	var wf WorkflowPresets
	if s.opt.Workflows != nil {
		got, err := s.opt.Workflows.Presets(ctx, ws, ConsumeInfo{Filename: pl.Filename, Source: source, Title: title, Content: res.Content})
		if err != nil {
			s.opt.Log.Warn("consumption workflows", "workspace", ws, "err", err)
		} else {
			wf = got
		}
		if t := strings.TrimSpace(wf.Title); t != "" && strings.TrimSpace(pl.Overrides.Title) == "" {
			if r := []rune(t); len(r) > 200 {
				t = string(r[:200])
			}
			title = t
		}
	}
	byWorkflow := func(field string) *document.Source {
		return &document.Source{Source: document.SourceWorkflow, Basis: wf.Names[field]}
	}
	// Classification reads the rules and the model; it runs before the
	// transaction so the write lock is not held meanwhile.
	sug := classify.Suggestion{Sources: document.FieldSources{Tags: map[int64]document.Source{}}}
	if s.opt.Classifier != nil {
		got, err := s.opt.Classifier.Suggest(ctx, ws, title, res.Content)
		if err != nil {
			s.opt.Log.Warn("classify a new document", "workspace", ws, "err", err)
		} else {
			sug = got
		}
	}
	sources := sug.Sources
	date, dateSource := documentDate(pl, src, res, now)
	sources.Date = dateSource
	// New documents go to the workspace's storage (v1.7.0). Files bound
	// for another machine are uploaded now, before the transaction, so it
	// does not wait on the network (v1.7.1).
	storage, err := s.q.GetWorkspaceStorage(ctx, ws)
	if err != nil {
		return 0, err
	}
	if _, err := s.files.Backend(storage); err != nil {
		return 0, fmt.Errorf("空间的存储“%s”没有配置：%w", storage, err)
	}
	// A family member's private document keeps its files on this server
	// (v1.8.1); remote storages get a sealed copy only. Decided before the
	// upload as the transaction decides it (v1.9.10).
	if storage != files.Local && s.willBePrivate(ctx, ws, pl.Overrides, wf, sug) {
		storage = files.Local
	}
	// The sizes go on the document, so the quota adds them up without
	// asking the storage (v1.7.4).
	sizes := [2]sql.NullInt64{fileSizeOf(src), {Int64: 0, Valid: true}}
	if res.ArchivePath != "" {
		sizes[1] = fileSizeOf(res.ArchivePath)
	}
	original, err := s.files.Stage(ctx, storage, ws, src, true)
	if err != nil {
		return 0, err
	}
	defer original.Discard(ctx, s.files)
	var archive *files.Staged
	if res.ArchivePath != "" {
		if archive, err = s.files.Stage(ctx, storage, ws, res.ArchivePath, true); err != nil {
			return 0, err
		}
		defer archive.Discard(ctx, s.files)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	q := s.q.WithTx(tx)
	// What the uploader chose wins over classification. Suggested items
	// deleted meanwhile are dropped.
	preset := &document.Source{Source: document.SourcePreset}
	typeID := s.ownID(ctx, q, ws, pl.Overrides.DocumentTypeID, true)
	if typeID != 0 {
		sources.Type = preset
	} else if typeID = s.ownID(ctx, q, ws, wf.DocumentTypeID, true); typeID != 0 {
		sources.Type = byWorkflow("type")
	} else if typeID = s.ownID(ctx, q, ws, sug.TypeID, true); typeID == 0 {
		sources.Type = nil
	}
	corrID := s.ownID(ctx, q, ws, pl.Overrides.CorrespondentID, false)
	if corrID != 0 {
		sources.Correspondent = preset
	} else if corrID = s.ownID(ctx, q, ws, wf.CorrespondentID, false); corrID != 0 {
		sources.Correspondent = byWorkflow("correspondent")
	} else if corrID = s.ownID(ctx, q, ws, sug.CorrespondentID, false); corrID == 0 {
		sources.Correspondent = nil
	}
	personID := s.ownPerson(ctx, q, ws, pl.Overrides.PersonID)
	if personID != 0 {
		sources.Person = preset
	} else if personID = s.ownPerson(ctx, q, ws, wf.PersonID); personID != 0 {
		sources.Person = byWorkflow("person")
	} else if personID = s.ownPerson(ctx, q, ws, sug.PersonID); personID == 0 {
		sources.Person = nil
	}
	var tagIDs []int64
	for _, id := range pl.Overrides.TagIDs {
		if slices.Contains(tagIDs, id) {
			continue
		}
		if _, err := q.GetTag(ctx, sqlc.GetTagParams{WorkspaceID: ws, ID: id}); err == nil {
			tagIDs = append(tagIDs, id)
			sources.Tags[id] = *preset
		}
	}
	for _, id := range wf.TagIDs {
		if slices.Contains(tagIDs, id) {
			continue
		}
		if _, err := q.GetTag(ctx, sqlc.GetTagParams{WorkspaceID: ws, ID: id}); err == nil {
			tagIDs = append(tagIDs, id)
			sources.Tags[id] = *byWorkflow(strconv.FormatInt(id, 10))
		}
	}
	for _, id := range sug.TagIDs {
		if slices.Contains(tagIDs, id) {
			continue
		}
		if _, err := q.GetTag(ctx, sqlc.GetTagParams{WorkspaceID: ws, ID: id}); err == nil {
			tagIDs = append(tagIDs, id)
		} else {
			delete(sources.Tags, id)
		}
	}
	var archiveSum sql.NullString
	if res.ArchivePath != "" {
		sum, err := fileSHA256(res.ArchivePath)
		if err != nil {
			return 0, err
		}
		archiveSum = sql.NullString{String: sum, Valid: true}
	}
	id, err := q.InsertDocument(ctx, sqlc.InsertDocumentParams{
		WorkspaceID: ws, Title: title, TitleSort: pinyin.TitleKey(title), Content: res.Content, CreatedDate: date, AddedAt: now.UnixMilli(), ModifiedAt: now.UnixMilli(),
		CorrespondentID: nullID(corrID), DocumentTypeID: nullID(typeID), MimeType: res.MimeType,
		OriginalFilename: pl.Filename, OriginalExt: res.Ext, PageCount: int64(res.PageCount), Checksum: pl.Checksum,
		ArchiveChecksum: archiveSum, HasArchive: boolInt(res.ArchivePath != ""), HasThumbnail: boolInt(res.ThumbnailPath != ""), Inbox: 1,
		FieldSources: sources.String(), Source: source, OriginalEncrypted: boolInt(pl.Decrypted), PersonID: nullID(personID),
		Storage: storage, OriginalSize: sizes[0], DerivedSize: sizes[1],
		AddedBy: sql.NullInt64{Int64: pl.Actor.UserID, Valid: pl.Actor.UserID != 0}, // a member quota counts it (v1.7.11)
	})
	if err != nil {
		if isUniqueViolation(err) {
			return 0, errDuplicate // a concurrent upload of the same file won
		}
		return 0, err
	}
	for _, t := range tagIDs {
		if err := q.AddDocumentTag(ctx, sqlc.AddDocumentTagParams{DocumentID: id, TagID: t}); err != nil {
			return 0, err
		}
	}
	if res.Pending != "" { // waits for Gotenberg (v1.9.1)
		if err := notePending(ctx, tx, ws, id, res.Pending, now); err != nil {
			return 0, err
		}
	}
	// Private from the start (v1.8.0): asked for, or a type whose documents
	// are private, with a family member someone looks after.
	private := pl.Overrides.Private && personID != 0
	if !private {
		if private, err = privacy.New(s.db).AutoPrivate(ctx, q, ws, nullID(typeID), nullID(personID)); err != nil {
			return 0, err
		}
	}
	if private {
		if err := q.SetDocumentPrivate(ctx, sqlc.SetDocumentPrivateParams{Private: 1, WorkspaceID: ws, ID: id}); err != nil {
			return 0, err
		}
	}
	if pl.Path != "" {
		if _, err := tx.ExecContext(ctx, `UPDATE documents SET origin_path = ? WHERE id = ?`, pl.Path, id); err != nil {
			return 0, err
		}
	}
	if c := pl.Overrides.Checklist; c != nil {
		// only a checklist of this workspace; one deleted meanwhile is skipped
		if _, err := tx.ExecContext(ctx, `INSERT INTO checklist_documents (checklist_id, item_id, document_id, added_at)
			SELECT id, ?, ?, ? FROM checklists WHERE id = ? AND workspace_id = ? ON CONFLICT DO NOTHING`, c.ItemID, id, now.UnixMilli(), c.ID, ws); err != nil {
			return 0, err
		}
	}
	if a := pl.Overrides.ASN; a != nil && *a >= 1 && *a <= 999999 {
		if n, err := q.SetDocumentASNIfFree(ctx, sqlc.SetDocumentASNIfFreeParams{Asn: sql.NullInt64{Int64: *a, Valid: true}, WorkspaceID: ws, ID: id}); err != nil {
			return 0, err
		} else if n == 0 && warnings != nil {
			*warnings = append(*warnings, fmt.Sprintf("原件编号 #%06d 已被其他文档使用，没有填入", *a))
		}
	}
	// An e-book is a book from the start (v0.4), before filing so the
	// library's rule sees it.
	if res.Book != nil {
		if err := s.addBook(ctx, q, ws, id, res.Book, warnings); err != nil {
			return 0, err
		}
	}
	// Into the first drawer whose rule matches (design doc 5.7), after
	// classification so rules on type, correspondent and tags see them.
	if s.opt.Placer != nil {
		if err := s.opt.Placer.PlaceDocument(ctx, tx, ws, id); err != nil {
			return 0, err
		}
		// A drawer chosen at upload is a hand placement; one deleted
		// meanwhile is dropped.
		d := pl.Overrides.DrawerID
		if d == 0 {
			d = wf.DrawerID
		}
		if d != 0 {
			if _, err := q.GetDrawer(ctx, sqlc.GetDrawerParams{WorkspaceID: ws, ID: d}); err == nil {
				if err := s.opt.Placer.SetManual(ctx, tx, ws, id, d, false); err != nil {
					return 0, err
				}
			}
		}
	}
	undo, err := s.placeFiles(ctx, storage, ws, id, original, archive, res)
	if err != nil {
		undo()
		return 0, err
	}
	if res.Book != nil && res.CoverPath != "" {
		if err := q.SetBookCover(ctx, sqlc.SetBookCoverParams{Cover: 1, Now: now.UnixMilli(), WorkspaceID: ws, DocumentID: id}); err != nil {
			undo()
			return 0, err
		}
	}
	if err := audit.Record(ctx, q, audit.Event{
		WorkspaceID: ws, Action: "document.create", TargetType: "document", TargetID: strconv.FormatInt(id, 10),
		Summary: fmt.Sprintf(i18n.N("%s 上传了“%s”"), audit.ActorFrom(ctx).Name, pl.Filename), At: now,
	}); err != nil {
		undo()
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		undo()
		return 0, err
	}
	if len(pl.Overrides.CustomFields) > 0 && s.opt.SetFields != nil {
		if err := s.opt.SetFields(ctx, ws, id, pl.Overrides.CustomFields); err != nil {
			s.opt.Log.Warn("set the custom fields given at upload", "document", id, "err", err)
		}
	}
	if s.opt.Workflows != nil {
		s.opt.Workflows.Added(ctx, ws, id, wf.Ran)
	}
	if s.opt.OnAdded != nil {
		s.opt.OnAdded(ctx, ws, id)
	}
	return id, nil
}

// addBook writes the catalog entry an e-book carries. An ISBN that is
// malformed or already another book's is left out, with a warning.
func (s *Service) addBook(ctx context.Context, q *sqlc.Queries, ws, id int64, b *parser.BookMeta, warnings *[]string) error {
	warn := func(msg string) {
		if warnings != nil {
			*warnings = append(*warnings, msg)
		}
	}
	isbn, err := books.NormalizeISBN(b.ISBN)
	if err != nil {
		warn("书中的 ISBN（" + b.ISBN + "）校验不通过，没有填入")
		isbn = ""
	}
	if isbn != "" {
		if other, err := q.FindBookByISBN(ctx, sqlc.FindBookByISBNParams{WorkspaceID: ws, Isbn: isbn}); err == nil {
			warn("ISBN " + isbn + " 已经是“" + other.Title + "”的书目，没有填入")
			isbn = ""
		} else if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
	}
	authors := b.Authors
	if authors == nil {
		authors = []string{}
	}
	if len(authors) > 20 {
		authors = authors[:20]
	}
	for i, a := range authors {
		authors[i] = truncate(a, 100)
	}
	aj, _ := json.Marshal(authors)
	return q.UpsertBook(ctx, sqlc.UpsertBookParams{
		DocumentID: id, WorkspaceID: ws, Isbn: isbn, Authors: string(aj), Publisher: truncate(b.Publisher, 200), Series: truncate(b.Series, 200),
		Published: truncate(b.Published, 200), Language: truncate(b.Language, 200), Description: truncate(b.Description, 5000), Now: s.opt.Now().UnixMilli(),
	})
}

func truncate(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n])
	}
	return s
}

// placeFiles places the staged original and archive in the document's
// storage and moves the thumbnail and cover to the data directory. The returned
// undo removes what it placed (also on error).
func (s *Service) placeFiles(ctx context.Context, storage string, ws, id int64, original, archive *files.Staged, res parser.Result) (func(), error) {
	var stored []files.Ref
	var local []string
	undo := func() {
		ctx := context.WithoutCancel(ctx)
		for _, r := range stored {
			_ = s.files.Remove(ctx, storage, r)
		}
		for _, p := range local {
			os.Remove(p)
		}
	}
	put := []struct {
		from *files.Staged
		to   files.Ref
	}{{original, files.Original(ws, id, res.Ext)}}
	if archive != nil {
		put = append(put, struct {
			from *files.Staged
			to   files.Ref
		}{archive, files.Archive(ws, id)})
	}
	for _, p := range put {
		if err := p.from.Place(ctx, s.files, p.to); err != nil {
			undo()
			return func() {}, err
		}
		stored = append(stored, p.to)
	}
	var moves [][2]string
	if res.ThumbnailPath != "" {
		moves = append(moves, [2]string{res.ThumbnailPath, s.files.ThumbnailPath(ws, id)})
	}
	if res.Book != nil && res.CoverPath != "" {
		moves = append(moves, [2]string{res.CoverPath, s.files.CoverPath(ws, id)})
	}
	for _, m := range moves {
		if err := files.Move(m[0], m[1]); err != nil {
			undo()
			return func() {}, err
		}
		local = append(local, m[1])
	}
	return undo, nil
}

func (s *Service) handleReprocess(ctx context.Context, job jobs.Job) (any, error) {
	var pl payload
	if err := json.Unmarshal(job.Payload, &pl); err != nil {
		return nil, jobs.Permanent(err)
	}
	ws := *job.WorkspaceID
	ctx = audit.WithActor(ctx, pl.Actor)
	row, err := s.q.GetDocument(ctx, sqlc.GetDocumentParams{WorkspaceID: ws, ID: pl.DocumentID, Viewer: tenant.ViewerSystem})
	if errors.Is(err, sql.ErrNoRows) {
		return nil, jobs.Permanent(errDocumentGone)
	}
	if err != nil {
		return nil, err
	}
	dir, err := s.files.NewTmpDir("reprocess-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)
	src := filepath.Join(dir, "original")
	from := files.Original(ws, row.ID, row.OriginalExt)
	if row.OriginalEncrypted == 1 {
		// The original needs its password; the decrypted copy kept as the
		// archive (unedited: the base file while pages are edited) is read.
		from = files.Archive(ws, row.ID)
		if s.files.Exists(ctx, row.Storage, files.Base(ws, row.ID)) {
			from = files.Base(ws, row.ID)
		}
	}
	if err := s.files.Fetch(ctx, row.Storage, from, src); err != nil {
		return nil, jobs.Permanent(errors.Join(errOriginalGone, err))
	}
	progress := s.progressFunc(ctx, ws, job.ID)
	res, err := parser.Parse(ctx, s.opt.Parser, src, row.OriginalFilename, dir, progress)
	if err != nil {
		return nil, wrapParseError(err)
	}
	if row.OriginalEncrypted == 1 && res.ArchivePath == "" {
		res.ArchivePath = src
	}
	progress("saving", 95)
	// Replace the derived files first; the row only changes if that worked.
	var archiveSum sql.NullString
	if res.ArchivePath != "" {
		if sum, err := fileSHA256(res.ArchivePath); err == nil {
			archiveSum = sql.NullString{String: sum, Valid: true}
		}
		if err := s.files.Put(ctx, row.Storage, files.Archive(ws, row.ID), res.ArchivePath, true); err != nil {
			return nil, err
		}
	} else if err := s.files.Remove(ctx, row.Storage, files.Archive(ws, row.ID)); err != nil {
		return nil, err
	}
	thumb := s.files.ThumbnailPath(ws, row.ID)
	if res.ThumbnailPath == "" {
		os.Remove(thumb)
	} else if err := files.Move(res.ThumbnailPath, thumb); err != nil {
		return nil, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	q := s.q.WithTx(tx)
	now := s.opt.Now()
	if err := q.UpdateDocumentFiles(ctx, sqlc.UpdateDocumentFilesParams{
		Content: res.Content, PageCount: int64(res.PageCount), HasArchive: boolInt(res.ArchivePath != ""), ArchiveChecksum: archiveSum,
		HasThumbnail: boolInt(res.ThumbnailPath != ""), ModifiedAt: now.UnixMilli(), WorkspaceID: ws, ID: row.ID,
	}); err != nil {
		return nil, err
	}
	if err := notePending(ctx, tx, ws, row.ID, res.Pending, now); err != nil {
		return nil, err
	}
	if err := audit.Record(ctx, q, audit.Event{
		WorkspaceID: ws, Action: "document.reprocess", TargetType: "document", TargetID: strconv.FormatInt(row.ID, 10),
		Summary: fmt.Sprintf(i18n.N("%s 重新处理了“%s”"), audit.ActorFrom(ctx).Name, row.Title), At: now,
	}); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	_ = s.queue.SetDocument(ctx, job.ID, row.ID)
	if s.opt.AfterReprocess != nil {
		if err := s.opt.AfterReprocess(ctx, ws, row.ID); err != nil {
			res.Warnings = append(res.Warnings, "页面编辑没能重新应用："+err.Error())
		}
	}
	s.bus.Publish(events.Event{Type: events.DocumentUpdated, WorkspaceID: ws, Data: map[string]any{"documentId": row.ID}})
	return Progress{Stage: "done", Progress: 100, DocumentID: row.ID, Warnings: res.Warnings}, nil
}

// progressFunc records and announces progress of a running job.
func (s *Service) progressFunc(ctx context.Context, ws, jobID int64) parser.Progress {
	return func(stage string, pct int) {
		p := Progress{Stage: stage, Progress: pct}
		if err := s.queue.SetProgress(ctx, jobID, p); err != nil {
			s.opt.Log.Warn("record job progress", "job", jobID, "err", err)
		}
		s.publishJob(ws, jobID, "running", p, "")
	}
}

// JobEvent is the data of a job.updated event.
type JobEvent struct {
	ID         int64  `json:"id"`
	Status     string `json:"status"` // queued, running, retrying, succeeded, failed
	Stage      string `json:"stage"`
	Progress   int    `json:"progress"`
	DocumentID int64  `json:"documentId,omitempty"`
	Error      string `json:"error,omitempty"`
}

func (s *Service) publishJob(ws, id int64, status string, p Progress, errMsg string) {
	s.bus.Publish(events.Event{Type: events.JobUpdated, WorkspaceID: ws, Data: JobEvent{
		ID: id, Status: status, Stage: p.Stage, Progress: p.Progress, DocumentID: p.DocumentID, Error: errMsg,
	}})
}

// ownID returns id if it is a document type (or correspondent) of the
// workspace, otherwise 0.
func (s *Service) ownID(ctx context.Context, q *sqlc.Queries, ws, id int64, docType bool) int64 {
	if id == 0 {
		return 0
	}
	var err error
	if docType {
		_, err = q.GetDocumentType(ctx, sqlc.GetDocumentTypeParams{WorkspaceID: ws, ID: id})
	} else {
		_, err = q.GetCorrespondent(ctx, sqlc.GetCorrespondentParams{WorkspaceID: ws, ID: id})
	}
	if err != nil {
		return 0
	}
	return id
}

// ownPerson returns id if it is a person of the workspace, otherwise 0.
func (s *Service) ownPerson(ctx context.Context, q *sqlc.Queries, ws, id int64) int64 {
	if id == 0 {
		return 0
	}
	if _, err := q.GetPerson(ctx, sqlc.GetPersonParams{WorkspaceID: ws, ID: id}); err != nil {
		return 0
	}
	return id
}

// wrapParseError marks problems with the file itself as permanent.
func wrapParseError(err error) error {
	if parser.IsPermanent(err) {
		return jobs.Permanent(err)
	}
	return err
}

// JobErrorMessage turns a stored job error into what the tray shows: the
// messages ingest produces itself are Chinese and kept; anything else
// (tool output, I/O errors) is replaced by a generic message.
func JobErrorMessage(stored string) string {
	for _, e := range []string{
		parser.ErrUnsupported.Message, parser.ErrEncrypted.Message, parser.ErrCorrupt.Message, parser.ErrEmpty.Message,
		errDuplicate.Message, errDuplicateInTrash.Message, errUploadGone.Message, errDocumentGone.Message, errOriginalGone.Message,
	} {
		if strings.HasPrefix(stored, e) {
			return e
		}
	}
	if strings.Contains(stored, "超时") || strings.Contains(stored, "timed out") {
		return i18n.N("处理超时，请稍后重试")
	}
	return i18n.N("处理失败，请稍后重试")
}

// userMessage is the part of a job error the tray shows: parser and
// application errors carry Chinese messages; anything else is generic.
func userMessage(err error) string {
	var pe *parser.Error
	var ae *apperr.Error
	switch {
	case errors.As(err, &pe):
		return pe.Message
	case errors.As(err, &ae):
		return ae.Message
	}
	if msg := err.Error(); strings.Contains(msg, "超时") || strings.Contains(msg, "timed out") {
		return i18n.N("处理超时，请稍后重试")
	}
	return i18n.N("处理失败，请稍后重试")
}

func validDate(s string) bool {
	_, err := time.Parse("2006-01-02", s)
	return err == nil
}

func titleFrom(filename string) string {
	t := strings.TrimSpace(strings.TrimSuffix(filename, filepath.Ext(filename)))
	if t == "" {
		t = "未命名文档"
	}
	if utf8.RuneCountInString(t) > 200 {
		t = string([]rune(t)[:200])
	}
	return t
}

// cleanFilename keeps only the base name and drops control characters.
func cleanFilename(name string) string {
	name = filepath.Base(strings.ReplaceAll(name, "\\", "/"))
	name = strings.Map(func(r rune) rune {
		if r < 32 || r == 127 {
			return -1
		}
		return r
	}, name)
	if name == "." || name == "/" || name == "" {
		return "未命名文件"
	}
	return name
}

func fileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func isUniqueViolation(err error) bool { return store.IsUniqueViolation(err) }

func nullID(id int64) sql.NullInt64 { return sql.NullInt64{Int64: id, Valid: id != 0} }

func boolInt(b bool) int64 {
	if b {
		return 1
	}
	return 0
}

// documentDate picks the document's date (v1.4.4): the one chosen when
// uploading; else a date the text tells plainly (开票日期 …, or one with
// nothing against it); else the file's name or a photo's capture time;
// else a weaker date of the text (a period's start); else today. The other
// dates found are kept as alternatives for the details page.
func documentDate(pl payload, src string, res parser.Result, now time.Time) (string, *document.Source) {
	type option struct {
		date, basis, source string
		score               int
	}
	var opts []option
	for _, c := range parser.DateCandidates(res.Content, now) {
		opts = append(opts, option{c.Date, c.Matched, document.SourceText, c.Score})
	}
	var file *option
	if d, ok := parser.FileNameDate(pl.Filename, now); ok {
		file = &option{d, fmt.Sprintf(i18n.N("文件名“%s”"), pl.Filename), document.SourceFile, 0}
	} else if strings.HasPrefix(res.MimeType, "image/") {
		if x := exif.ReadFile(src); !x.Taken.IsZero() && x.Taken.Year() >= 1990 && x.Taken.Year() <= now.Year()+1 {
			file = &option{x.Taken.Format("2006-01-02"), fmt.Sprintf(i18n.N("拍摄时间 %s"), x.Taken.Format("2006-01-02 15:04")), document.SourceFile, 0}
		}
	}
	var chosen *option
	switch {
	case validDate(pl.Overrides.CreatedDate):
		chosen = &option{date: pl.Overrides.CreatedDate, source: document.SourcePreset}
	case res.Email != nil && !res.Email.Date.IsZero(): // an e-mail is dated when it was sent
		chosen = &option{res.Email.Date.Local().Format("2006-01-02"), fmt.Sprintf(i18n.N("邮件发送时间 %s"), res.Email.Date.Local().Format("2006-01-02 15:04")), document.SourceFile, 0}
	case len(opts) > 0 && opts[0].score >= 0:
		chosen = &opts[0]
	case file != nil:
		chosen = file
	case len(opts) > 0:
		chosen = &opts[0]
	}
	if file != nil {
		opts = append(opts, *file)
	}
	if chosen == nil {
		return now.Format("2006-01-02"), nil
	}
	src0 := &document.Source{Source: chosen.source, Basis: chosen.basis}
	seen := map[string]bool{chosen.date: true}
	for _, o := range opts {
		if !seen[o.date] && len(src0.Alternatives) < 5 {
			seen[o.date] = true
			src0.Alternatives = append(src0.Alternatives, document.DateOption{Date: o.date, Basis: o.basis})
		}
	}
	return chosen.date, src0
}

// fileSizeOf is a local file's size, not known (NULL) when it cannot be read.
func fileSizeOf(p string) sql.NullInt64 {
	st, err := os.Stat(p)
	if err != nil {
		return sql.NullInt64{}
	}
	return sql.NullInt64{Int64: st.Size(), Valid: true}
}

func (p payload) done() {
	if p.release != nil {
		p.release()
	}
}

// isPrivate reports whether a document is a family member's private one
// (v1.8.0): a duplicate upload is then not linked to it, which would show
// it in the uploader's task list.
func (s *Service) isPrivate(ctx context.Context, ws, id int64) bool {
	d, err := s.q.GetDocumentAnyState(ctx, sqlc.GetDocumentAnyStateParams{WorkspaceID: ws, ID: id, Viewer: tenant.ViewerSystem})
	return err == nil && d.Private == 1
}

// duplicateOf is errDuplicate naming the document already there, unless it
// is private: that one is not named.
func (s *Service) duplicateOf(ctx context.Context, ws, id int64) error {
	if s.isPrivate(ctx, ws, id) {
		return errDuplicate
	}
	return errDuplicate.With(map[string]any{"documentId": id})
}

// willBePrivate tells, before a new document's files are written, whether
// it will be private — decided as commit decides it (v1.9.10: before, a
// guess that left out the workflows let a private document's files reach
// remote storage until the storage job took them back): the type and
// person the uploader chose, else a workflow's, else the suggestion's, of
// those still there; then asked for, or private by its type.
func (s *Service) willBePrivate(ctx context.Context, ws int64, ov Overrides, wf WorkflowPresets, sug classify.Suggestion) bool {
	typeID := s.ownID(ctx, s.q, ws, ov.DocumentTypeID, true)
	if typeID == 0 {
		typeID = s.ownID(ctx, s.q, ws, wf.DocumentTypeID, true)
	}
	if typeID == 0 {
		typeID = s.ownID(ctx, s.q, ws, sug.TypeID, true)
	}
	person := s.ownPerson(ctx, s.q, ws, ov.PersonID)
	if person == 0 {
		person = s.ownPerson(ctx, s.q, ws, wf.PersonID)
	}
	if person == 0 {
		person = s.ownPerson(ctx, s.q, ws, sug.PersonID)
	}
	if ov.Private && person != 0 {
		return true
	}
	auto, err := privacy.New(s.db).AutoPrivate(ctx, s.q, ws, nullID(typeID), nullID(person))
	return err != nil || auto // in doubt, the files stay here
}
