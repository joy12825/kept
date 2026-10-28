// Package demo loads the web prototype's sample data into the database, so
// the real pages can be demonstrated with the same content. It is only run
// on request (`kept demo load`), never automatically.
package demo

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"kept/server/internal/apperr"
	"kept/server/internal/audit"
	"kept/server/internal/auth"
	"kept/server/internal/classify"
	"kept/server/internal/document"
	"kept/server/internal/drawer"
	"kept/server/internal/files"
	"kept/server/internal/ingest"
	"kept/server/internal/parser"
	"kept/server/internal/pinyin"
	"kept/server/internal/store/sqlc"
	"kept/server/internal/taxonomy"
	"kept/server/internal/tenant"
	"kept/server/internal/views"
	"kept/server/internal/workspace"
)

type Deps struct {
	DB         *sql.DB
	Auth       *auth.Service
	Workspaces *workspace.Service
	Taxonomy   *taxonomy.Service
	Ingest     *ingest.Service
	Classifier *classify.Service
	Drawers    *drawer.Service
	Files      *files.Store // book covers
	// EE are the paid modules (v1.13); the open-source build has none
	EE EEDeps
}

// SpaceReport is what Load added to one workspace.
type SpaceReport struct {
	Name           string
	Created        bool // the workspace itself was created
	Correspondents int
	Types          int
	Tags           int
	Documents      int
	Views          int
	Learned        int // items the trained classifier learned
	Drawers        int
	Devices        int // paired phones (家庭空间)
	Workflows      int // sample workflows (家庭空间)
	Shares         int // share links (家庭空间)
	Versions       int // documents given a second version (家庭空间)
	Duplicates     int // near-duplicate pairs found (家庭空间)
	AgentRuns      int // sample AI activity (家庭空间)
	Books          int // sample books in the 书房 (家庭空间)
	// DocumentsSkipped explains why no documents were loaded ("" when they were).
	DocumentsSkipped string
	// RecoveryCode is the recovery code of the demo key made for 爸爸
	// (v1.8.1), shown once; "" when there was one already.
	RecoveryCode string
	// StorageRecoveryCode is the instance's recovery code for encrypted
	// storages (v1.11.0), made when there was none, shown once.
	StorageRecoveryCode string
	// LabReports counts the lab reports added (v1.8.2).
	LabReports int
	// Findings counts the examination reports, records and receipts added
	// (v1.8.3).
	Findings int
	// Scenes counts the values and medicines of the scenes (v1.9.0).
	Scenes int
	// Previewed counts the note, recording and photo added (v1.9.1), and
	// the e-invoice, workbook and zip (v1.9.3).
	Previewed int
}

// maxDocuments limits the documents per workspace (tests); 0 loads all.
var maxDocuments = 0

// Load gives the account (username or email) the prototype's two sample
// workspaces and fills in their taxonomy. Running it again adds only what
// is missing.
func Load(ctx context.Context, d Deps, login string) ([]SpaceReport, error) {
	u, err := d.Auth.FindUser(ctx, login)
	if err != nil {
		return nil, err
	}
	existing, err := d.Workspaces.List(ctx, u.ID)
	if err != nil {
		return nil, err
	}
	var reports []SpaceReport
	for _, sd := range spaces {
		rep := SpaceReport{Name: sd.name}
		var ws workspace.Workspace
		for _, w := range existing {
			if w.Name == sd.name && w.Kind == sd.kind && w.Role == tenant.RoleOwner {
				ws = w
			}
		}
		if ws.ID == 0 {
			if ws, err = d.Workspaces.Create(ctx, u.ID, sd.name, sd.kind, sd.color); err != nil {
				return reports, err
			}
			rep.Created = true
		}
		scope := tenant.New(ws.ID, tenant.RoleOwner)

		for _, name := range sd.correspondents {
			added, err := skipExisting(d.Taxonomy.Create(ctx, scope, taxonomy.Correspondent, taxonomy.Input{Name: name, Rule: ruleFor(name)}))
			if err != nil {
				return reports, err
			}
			rep.Correspondents += added
		}
		for _, name := range sd.types {
			added, err := skipExisting(d.Taxonomy.Create(ctx, scope, taxonomy.DocumentType, taxonomy.Input{Name: name}))
			if err != nil {
				return reports, err
			}
			rep.Types += added
		}
		for _, tg := range sd.tags {
			_, err := d.Taxonomy.CreateTag(ctx, scope, taxonomy.TagInput{Name: tg.name, Color: tg.color})
			added, err := skipExisting(nil, err)
			if err != nil {
				return reports, err
			}
			rep.Tags += added
		}
		if missing := parser.MissingTools(); len(missing) > 0 {
			rep.DocumentsSkipped = "缺少 " + strings.Join(missing, "、") + "，没有写入示例文档（macOS 可执行 brew install ocrmypdf poppler tesseract-lang）"
		} else if rep.Documents, err = loadDocuments(ctx, d, scope, sd.documents()); err != nil {
			return append(reports, rep), err
		} else if rep.Learned, err = classifyDemo(ctx, d, scope); err != nil {
			return append(reports, rep), err
		} else if rep.Drawers, err = loadDrawers(audit.WithActor(ctx, audit.Actor{UserID: u.ID, Name: u.DisplayName, Via: audit.ViaSystem}), d, scope, sd.drawers); err != nil {
			return append(reports, rep), err
		}
		actor := audit.WithActor(ctx, audit.Actor{UserID: u.ID, Name: u.DisplayName, Via: audit.ViaSystem})
		if rep.Views, err = loadViews(actor, d, scope); err != nil {
			return append(reports, rep), err
		}
		if sd.kind == "family" {
			if rep.Devices, err = loadDevices(ctx, d, u.ID, scope); err != nil {
				return append(reports, rep), err
			}
			// Reminder emails on (they go out once KEPT_SMTP_* is set).
			if _, err := sqlc.New(d.DB).SetMemberNotify(ctx, sqlc.SetMemberNotifyParams{NotifyEmail: 1, WorkspaceID: scope.WorkspaceID(), UserID: u.ID}); err != nil {
				return append(reports, rep), err
			}
			if rep.Workflows, err = loadWorkflows(ctx, d, scope, u.Email); err != nil {
				return append(reports, rep), err
			}
			x, err := loadExtras(ctx, d, u.ID, scope)
			if err != nil {
				return append(reports, rep), err
			}
			rep.Shares, rep.Versions, rep.Duplicates, rep.AgentRuns = x.shares, x.versions, x.duplicates, x.agentRuns
			rep.RecoveryCode = x.recoveryCode
			if rep.StorageRecoveryCode, err = demoStorageRecovery(actor, d); err != nil {
				return append(reports, rep), err
			}
			if rep.DocumentsSkipped == "" {
				if rep.Books, err = loadBooks(actor, d, scope, u.ID); err != nil {
					return append(reports, rep), err
				}
				if rep.LabReports, err = demoLab(actor, d, scope); err != nil {
					return append(reports, rep), err
				}
				if rep.Findings, err = demoFindings(actor, d, scope); err != nil {
					return append(reports, rep), err
				}
				if rep.Scenes, err = demoScenes(actor, d, scope); err != nil {
					return append(reports, rep), err
				}
				if rep.Previewed, err = demoPreviewer(actor, d, scope); err != nil {
					return append(reports, rep), err
				}
				if _, err = loadQADemo(actor, d, scope, u.ID); err != nil {
					return append(reports, rep), err
				}
				if _, err = loadOrganizeDemo(actor, d, scope); err != nil {
					return append(reports, rep), err
				}
				if _, err = loadStewardDemo(actor, d, scope); err != nil {
					return append(reports, rep), err
				}
				if _, err = loadLibraryDemo(actor, d, scope, u.ID); err != nil {
					return append(reports, rep), err
				}
			}
		}
		reports = append(reports, rep)
	}
	return reports, shareDemo(ctx, d, u.ID, login)
}

// shareDemo shares the family drawers with the studio, read-only.
func shareDemo(ctx context.Context, d Deps, userID int64, login string) error {
	if d.Drawers == nil {
		return nil
	}
	list, err := d.Workspaces.List(ctx, userID)
	if err != nil {
		return err
	}
	var family, studio int64
	for _, w := range list {
		if w.Role != tenant.RoleOwner {
			continue
		}
		switch w.Name {
		case spaces[0].name:
			family = w.ID
		case spaces[1].name:
			studio = w.ID
		}
	}
	if family == 0 || studio == 0 {
		return nil
	}
	ctx = audit.WithActor(ctx, audit.Actor{UserID: userID, Name: login, Via: audit.ViaSystem})
	return shareAcross(ctx, d, tenant.New(family, tenant.RoleOwner), userID, studio)
}

// loadViews adds the prototype's saved views (常用筛选) when their tags exist.
func loadViews(ctx context.Context, d Deps, scope tenant.Scope) (int, error) {
	tags, err := d.Taxonomy.ListTags(ctx, scope)
	if err != nil {
		return 0, err
	}
	ids := map[string]int64{}
	for _, t := range tags {
		ids[t.Name] = t.ID
	}
	svc := views.NewService(d.DB)
	added := 0
	for _, v := range []struct {
		name, tag, filter string
	}{
		{"待报销发票", "待报销", `{"field":"tag","op":"has_any","value":[%d],"descendants":true}`},
		{"2024 税务", "税务", `{"all":[{"field":"tag","op":"has_any","value":[%d],"descendants":true},{"field":"created","op":"between","value":["2024-01-01","2024-12-31"]}]}`},
	} {
		id, ok := ids[v.tag]
		if !ok {
			continue
		}
		name, raw := v.name, json.RawMessage(fmt.Sprintf(v.filter, id))
		n, err := skipExisting(svc.Create(ctx, scope, views.Input{Name: &name, Filter: &raw}))
		if err != nil {
			return added, err
		}
		added += n
	}
	return added, nil
}

// skipExisting turns "already exists" into "added nothing".
func skipExisting(_ any, err error) (int, error) {
	var ae *apperr.Error
	if errors.As(err, &ae) && ae.Kind == apperr.KindConflict {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	return 1, nil
}

// loadDocuments renders each sample as a PDF, ingests it through the real
// pipeline and then sets the metadata the prototype shows (type,
// correspondent, tags, date, amount, inbox). Titles already present are
// skipped, so loading twice adds nothing.
func loadDocuments(ctx context.Context, d Deps, scope tenant.Scope, docs []docData) (int, error) {
	q := sqlc.New(d.DB)
	ws := scope.WorkspaceID()
	names := func(kind taxonomy.Kind) (map[string]int64, error) {
		items, err := d.Taxonomy.List(ctx, scope, kind)
		m := map[string]int64{}
		for _, it := range items {
			m[it.Name] = it.ID
		}
		return m, err
	}
	corrs, err := names(taxonomy.Correspondent)
	if err != nil {
		return 0, err
	}
	types, err := names(taxonomy.DocumentType)
	if err != nil {
		return 0, err
	}
	tagList, err := d.Taxonomy.ListTags(ctx, scope)
	if err != nil {
		return 0, err
	}
	tags := map[string]int64{}
	for _, t := range tagList {
		tags[t.Name] = t.ID
	}
	amountField, err := q.GetBuiltInField(ctx, sqlc.GetBuiltInFieldParams{WorkspaceID: ws, Key: sql.NullString{String: "amount", Valid: true}})
	if err != nil {
		return 0, err
	}

	added := 0
	for i, doc := range docs {
		if maxDocuments > 0 && i >= maxDocuments {
			break
		}
		if _, err := q.FindDocumentByTitle(ctx, sqlc.FindDocumentByTitleParams{WorkspaceID: ws, Title: doc.title}); err == nil {
			continue
		} else if !errors.Is(err, sql.ErrNoRows) {
			return added, err
		}
		pdf, err := samplePDF(doc)
		if err != nil {
			return added, err
		}
		id, err := d.Ingest.Import(ctx, scope, doc.title+".pdf", bytes.NewReader(pdf), ingest.Overrides{}, ingest.Upload{})
		if err != nil {
			return added, fmt.Errorf("%s: %w", doc.title, err)
		}
		inbox := int64(0)
		if doc.inbox {
			inbox = 1
		}
		if err := q.SetDocumentMeta(ctx, sqlc.SetDocumentMetaParams{
			Title: doc.title, TitleSort: pinyin.TitleKey(doc.title), CreatedDate: doc.date, CorrespondentID: nullID(corrs[doc.correspondent]),
			DocumentTypeID: nullID(types[doc.docType]), Asn: nullID(doc.asn), Inbox: inbox, ModifiedAt: time.Now().UnixMilli(), WorkspaceID: ws, ID: id,
		}); err != nil {
			return added, err
		}
		if doc.deletedDaysAgo > 0 {
			at := time.Now().AddDate(0, 0, -doc.deletedDaysAgo).UnixMilli()
			if err := q.SetDocumentDeleted(ctx, sqlc.SetDocumentDeletedParams{DeletedAt: sql.NullInt64{Int64: at, Valid: true}, ModifiedAt: at, WorkspaceID: ws, ID: id}); err != nil {
				return added, err
			}
		}
		for _, t := range doc.tags {
			if tid, ok := tags[t]; ok {
				if err := q.AddDocumentTag(ctx, sqlc.AddDocumentTagParams{DocumentID: id, TagID: tid}); err != nil {
					return added, err
				}
			}
		}
		if doc.amount > 0 {
			fen := int64(doc.amount*100 + 0.5)
			if err := q.SetMonetaryValue(ctx, sqlc.SetMonetaryValueParams{DocumentID: id, FieldID: amountField, ValueMonetary: sql.NullInt64{Int64: fen, Valid: true}}); err != nil {
				return added, err
			}
		}
		// Tags and the amount were added after the row (a running server may index meanwhile): index the final state.
		if err := q.BumpIndexRev(ctx, sqlc.BumpIndexRevParams{WorkspaceID: ws, ID: id}); err != nil {
			return added, err
		}
		added++
	}
	return added, nil
}

// classifyDemo trains the workspace's classifier on the filed samples and
// records, for the samples waiting in the inbox, where their values would
// have come from: the rule or the model where it agrees with the sample,
// and the date recognised in the text. The inbox then shows the ticks and
// the basis as after a real upload.
func classifyDemo(ctx context.Context, d Deps, scope tenant.Scope) (int, error) {
	if d.Classifier == nil {
		return 0, nil
	}
	ws := scope.WorkspaceID()
	res, err := d.Classifier.Train(ctx, ws, true)
	if err != nil {
		return 0, err
	}
	learned := 0
	for _, it := range res.Summary.Items {
		if it.Learned {
			learned++
		}
	}
	suggest, err := d.Classifier.Suggester(ctx, ws)
	if err != nil {
		return learned, err
	}
	q := sqlc.New(d.DB)
	rows, err := q.ListInboxDocuments(ctx, ws)
	if err != nil {
		return learned, err
	}
	for _, r := range rows {
		sug := suggest(r.Title, r.Content)
		fs := document.FieldSources{Drawer: document.ParseSources(r.FieldSources).Drawer}
		if sug.TypeID != 0 && sug.TypeID == r.DocumentTypeID.Int64 {
			fs.Type = sug.Sources.Type
		}
		if sug.CorrespondentID != 0 && sug.CorrespondentID == r.CorrespondentID.Int64 {
			fs.Correspondent = sug.Sources.Correspondent
		}
		if date, matched, ok := parser.FindDateText(r.Content, time.Now()); ok && date == r.CreatedDate {
			fs.Date = &document.Source{Source: document.SourceText, Basis: matched}
		}
		have, err := q.ListDocumentTagIDs(ctx, r.ID)
		if err != nil {
			return learned, err
		}
		for _, id := range have {
			if src, ok := sug.Sources.Tags[id]; ok {
				if fs.Tags == nil {
					fs.Tags = map[int64]document.Source{}
				}
				fs.Tags[id] = src
			}
		}
		if err := q.SetDocumentFieldSources(ctx, sqlc.SetDocumentFieldSourcesParams{FieldSources: fs.String(), WorkspaceID: ws, ID: r.ID}); err != nil {
			return learned, err
		}
	}
	return learned, nil
}

func nullID(id int64) sql.NullInt64 { return sql.NullInt64{Int64: id, Valid: id != 0} }
