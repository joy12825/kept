// Package document reads and writes documents: the list with filters and
// facet counts, details, editing, bulk operations and the trash (milestone
// 1.5). Ingest creates them.
package document

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"kept/server/internal/apperr"
	"kept/server/internal/events"
	"kept/server/internal/files"
	"kept/server/internal/search"
	"kept/server/internal/store/sqlc"
	"kept/server/internal/tenant"
)

// ErrNotFound covers a missing document, a deleted one and one in another
// workspace alike.
var ErrNotFound = apperr.New(apperr.KindNotFound, "找不到这份文档，它可能已被删除")

type Document struct {
	ID                int64
	WorkspaceID       int64
	Title             string
	Content           string
	CreatedDate       string // YYYY-MM-DD
	AddedAt           time.Time
	ModifiedAt        time.Time
	CorrespondentID   int64 // 0 when none
	DocumentTypeID    int64 // 0 when none
	PersonID          int64 // 0 when none (v1.6.3: 家人)
	LocationID        int64 // 0 when unknown: where the paper original is kept (v1.6.3)
	MimeType          string
	OriginalFilename  string
	OriginalExt       string
	PageCount         int
	ASN               *int64
	Checksum          string
	HasArchive        bool
	HasThumbnail      bool
	Inbox             bool
	DrawerID          int64  // 0 when in no drawer
	DrawerManual      bool   // a person chose the drawer (or chose none)
	IsMilestone       bool   // shown in the yearbook's 大事记
	SensitiveOwn      bool   // marked sensitive itself (v1.6.4); see Summary.Sensitive
	Storage           string // where its files are (v1.7.0); "local" is the data directory
	FieldSources      FieldSources
	Source            string     // how it was added: web, api, mobile, watch
	PageEdits         string     // pages.Edits as JSON; "" when not edited
	OriginalEncrypted bool       // the original is a password-protected PDF
	ContentEditedAt   *time.Time // the recognised text was corrected by hand (v1.3.7)
	ArchiveChecksum   string     // SHA-256 of the searchable version; "" when none
	// Private to its person (v1.8.0); AddedBy uploaded it, and still sees
	// it while UploaderAccess.
	Private        bool
	AddedBy        int64
	UploaderAccess bool
}

type Service struct {
	db  *sql.DB
	q   *sqlc.Queries
	opt Options
}

// Options are the service's collaborators; all are optional.
type Options struct {
	Bus   *events.Bus  // receives document.updated and inbox.changed
	Files *files.Store // purging removes the files
	// OnPurge is called with the IDs of documents deleted for good (the
	// search index drops them).
	OnPurge func(ids []int64)
	// Search answers keyword conditions; without it they fail with
	// search_unavailable. SearchReady, when set, may refuse while the index
	// is being rebuilt. SearchLimit caps the documents one keyword recalls.
	Search      *search.Index
	SearchReady func() error
	SearchLimit int
	// Placer files documents into drawers (package drawer): after a change
	// to a document not placed by hand, and when a person places it.
	Placer Placer
	// OnUserUpdate is called after a person changed documents' metadata
	// (not after automatic changes): workflows triggered by updates.
	OnUserUpdate func(ws int64, ids []int64)
	// OnPrivacyChange is called after documents became private, stopped
	// being so, or moved to another person (v1.8.1: their files move and
	// their sealed copies follow).
	OnPrivacyChange func(ws int64)
	Now             func() time.Time
}

// Placer is what the document service needs from drawers. db is the
// transaction the change is made in.
type Placer interface {
	PlaceDocument(ctx context.Context, db sqlc.DBTX, ws, docID int64) error
	SetManual(ctx context.Context, db sqlc.DBTX, ws, docID, drawerID int64, auto bool) error
	Name(ctx context.Context, db sqlc.DBTX, ws, id int64) string
}

func NewService(db *sql.DB, opt Options) *Service {
	if opt.Now == nil {
		opt.Now = time.Now
	}
	return &Service{db: db, q: sqlc.New(db), opt: opt}
}

func (s *Service) publish(e events.Event) {
	if s.opt.Bus != nil {
		s.opt.Bus.Publish(e)
	}
}

// inTxDB is inTx that also hands over the transaction (for the Placer).
func (s *Service) inTxDB(ctx context.Context, fn func(*sql.Tx, *sqlc.Queries) error) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := fn(tx, s.q.WithTx(tx)); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Service) inTx(ctx context.Context, fn func(*sqlc.Queries) error) error {
	tx, err := s.db.BeginTx(ctx, nil) // BEGIN IMMEDIATE (see store.Open)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := fn(s.q.WithTx(tx)); err != nil {
		return err
	}
	return tx.Commit()
}

func From(r sqlc.Document) Document {
	return Document{
		ID: r.ID, WorkspaceID: r.WorkspaceID, Title: r.Title, Content: r.Content, CreatedDate: r.CreatedDate,
		AddedAt: time.UnixMilli(r.AddedAt), ModifiedAt: time.UnixMilli(r.ModifiedAt),
		CorrespondentID: r.CorrespondentID.Int64, DocumentTypeID: r.DocumentTypeID.Int64, PersonID: r.PersonID.Int64, LocationID: r.LocationID.Int64,
		ASN: nullInt(r.Asn), MimeType: r.MimeType, OriginalFilename: r.OriginalFilename, OriginalExt: r.OriginalExt, PageCount: int(r.PageCount),
		Checksum: r.Checksum, HasArchive: r.HasArchive == 1, HasThumbnail: r.HasThumbnail == 1, Inbox: r.Inbox == 1,
		DrawerID: r.DrawerID.Int64, DrawerManual: r.DrawerManual == 1, IsMilestone: r.IsMilestone == 1, SensitiveOwn: r.Sensitive == 1, Storage: r.Storage,
		FieldSources: ParseSources(r.FieldSources), Source: r.Source, PageEdits: r.PageEdits, OriginalEncrypted: r.OriginalEncrypted == 1,
		ContentEditedAt: nullTime(r.ContentEditedAt), ArchiveChecksum: r.ArchiveChecksum.String,
		Private: r.Private == 1, AddedBy: r.AddedBy.Int64, UploaderAccess: r.UploaderAccess == 1,
	}
}

func nullTime(v sql.NullInt64) *time.Time {
	if !v.Valid {
		return nil
	}
	t := time.UnixMilli(v.Int64)
	return &t
}

func (s *Service) Get(ctx context.Context, scope tenant.Scope, id int64) (Document, error) {
	r, err := s.q.GetDocument(ctx, sqlc.GetDocumentParams{WorkspaceID: scope.WorkspaceID(), ID: id, Viewer: scope.Viewer()})
	if errors.Is(err, sql.ErrNoRows) {
		return Document{}, ErrNotFound
	}
	if err != nil {
		return Document{}, err
	}
	return From(r), nil
}

// ListInbox returns the documents waiting in the inbox, oldest first.
func (s *Service) ListInbox(ctx context.Context, scope tenant.Scope) ([]Document, error) {
	rows, err := s.q.ListInboxDocuments(ctx, scope.WorkspaceID())
	if err != nil {
		return nil, err
	}
	out := make([]Document, len(rows))
	for i, r := range rows {
		out[i] = From(r)
	}
	return out, nil
}

func nullInt(n sql.NullInt64) *int64 {
	if !n.Valid {
		return nil
	}
	return &n.Int64
}
