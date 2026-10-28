package document

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"kept/server/internal/i18n"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"kept/server/internal/apperr"
	"kept/server/internal/audit"
	"kept/server/internal/events"
	"kept/server/internal/filter"
	"kept/server/internal/pinyin"
	"kept/server/internal/privacy"
	"kept/server/internal/store/sqlc"
	"kept/server/internal/tenant"
)

var (
	errTitle      = apperr.New(apperr.KindInvalid, "标题不能为空，最多 200 个字")
	errDate       = apperr.New(apperr.KindInvalid, "日期的格式应为 2024-03-05")
	errASN        = apperr.New(apperr.KindInvalid, "原件编号应为 1 到 999999 之间的整数")
	errType       = apperr.New(apperr.KindInvalid, "这个类型不存在")
	errCorr       = apperr.New(apperr.KindInvalid, "这个往来方不存在")
	errPerson     = apperr.New(apperr.KindInvalid, "这位家人不存在")
	errLocation   = apperr.New(apperr.KindInvalid, "这个存放位置不存在")
	errTag        = apperr.New(apperr.KindInvalid, "这个标签不存在")
	errField      = apperr.New(apperr.KindInvalid, "这个自定义字段不存在")
	errLinkSelf   = apperr.New(apperr.KindInvalid, "文档不能链接到自己")
	errLinkTarget = apperr.New(apperr.KindInvalid, "链接的文档不存在")
	errContent    = apperr.New(apperr.KindInvalid, "识别文字太长了，最多 100 万字")
	errModified   = apperr.WithCode(apperr.KindConflict, "modified", "这份文档刚被其他人修改过，请刷新后再改")
)

const (
	maxTitleLen = 200
	maxASN      = 999999
	maxTextLen  = 2000 // a text or URL custom field
	maxContent  = 1_000_000
)

// Patch changes the given fields of a document; nil leaves a field alone.
// TypeID, CorrespondentID, PersonID, LocationID and ASN clear the field when set to 0.
type Patch struct {
	Title           *string
	CreatedDate     *string
	TypeID          *int64
	CorrespondentID *int64
	PersonID        *int64 // v1.6.3: 家人
	LocationID      *int64 // v1.6.3: where the paper original is kept
	TagIDs          *[]int64
	ASN             *int64
	CustomFields    []FieldInput
	Inbox           *bool
	// ExpectedModifiedAt, when set, must equal the stored modification time
	// (the details page read the document at that version).
	ExpectedModifiedAt *time.Time
	// DrawerID puts the document into a drawer by hand (0: into none);
	// DrawerAuto hands it back to the filing rules.
	DrawerID   *int64
	DrawerAuto bool
	// Milestone marks the document for the yearbook's 大事记.
	Milestone *bool
	// Sensitive marks the document sensitive (v1.6.4).
	Sensitive *bool
	// Private makes the document private to its person (v1.8.0); only the
	// person (or a guardian) may change it.
	Private *bool
	// Content replaces the recognised text, corrected by hand (v1.3.7).
	Content *string
	// Origin is where an automatic change comes from (classification); it
	// is recorded in field_sources for the fields that change. nil means a
	// person made the change: those fields become user-set.
	Origin *FieldSources
}

// FieldInput sets a custom field; a JSON null Value removes it. Values are
// typed as in FieldValue.
type FieldInput struct {
	FieldID int64
	Value   json.RawMessage
}

// Update applies p and records what changed. A patch that changes nothing
// writes nothing.
func (s *Service) Update(ctx context.Context, scope tenant.Scope, id int64, p Patch) (Detail, error) {
	var inboxChanged, changed, privacyChanged bool
	err := s.inTxDB(ctx, func(tx *sql.Tx, q *sqlc.Queries) error {
		row, err := q.GetDocument(ctx, sqlc.GetDocumentParams{WorkspaceID: scope.WorkspaceID(), ID: id, Viewer: scope.Viewer()})
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		if p.ExpectedModifiedAt != nil && p.ExpectedModifiedAt.UnixMilli() != row.ModifiedAt {
			return errModified
		}
		res, err := s.apply(ctx, q, scope, newNamesTx(tx, q, scope.WorkspaceID()), row, p, "")
		inboxChanged, changed, privacyChanged = res.inbox, res.changed, res.privacy
		return err
	})
	if err != nil {
		return Detail{}, err
	}
	if privacyChanged {
		s.privacyChanged(scope.WorkspaceID())
	}
	if changed {
		s.notify(scope.WorkspaceID(), []int64{id}, inboxChanged)
		if p.Origin == nil && s.opt.OnUserUpdate != nil {
			s.opt.OnUserUpdate(scope.WorkspaceID(), []int64{id})
		}
	}
	d, err := s.GetDetail(ctx, scope, id)
	if errors.Is(err, ErrNotFound) {
		// the change made it someone else's private document
		return Detail{}, ErrBecamePrivate
	}
	return d, err
}

// ErrBecamePrivate: the change was saved, and made the document private to
// a family member the editor may not look at.
var ErrBecamePrivate = apperr.WithCode(apperr.KindConflict, "became_private", "已保存。这份文档成了家人的私密文档，你不能再查看它")

// ApplyAutomatic writes an automatic change (p.Origin set, e.g. from
// reclassification) to one document and reports whether anything changed.
// A document modified after p.ExpectedModifiedAt is left alone (false).
func (s *Service) ApplyAutomatic(ctx context.Context, scope tenant.Scope, id int64, p Patch) (bool, error) {
	return s.ApplyAutof(ctx, scope, id, p, i18n.N("%s 自动分类了“%s”"))
}

// ApplyAutof is ApplyAuto with the activity summary as a format filled
// with the actor's name and the document's title, so it can be
// translated (v1.10.1).
func (s *Service) ApplyAutof(ctx context.Context, scope tenant.Scope, id int64, p Patch, format string) (bool, error) {
	return s.applyAuto(ctx, scope, id, p, func(title string) string { return fmt.Sprintf(format, audit.ActorFrom(ctx).Name, title) })
}

// ApplyAuto is ApplyAutomatic with the activity summary's wording after the
// actor's name (summary gets the document's title). It does not call
// OnUserUpdate.
func (s *Service) ApplyAuto(ctx context.Context, scope tenant.Scope, id int64, p Patch, summary func(title string) string) (bool, error) {
	return s.applyAuto(ctx, scope, id, p, func(title string) string { return audit.ActorFrom(ctx).Name + summary(title) })
}

// applyAuto is ApplyAuto with the whole summary (the actor's name included).
func (s *Service) applyAuto(ctx context.Context, scope tenant.Scope, id int64, p Patch, summary func(title string) string) (bool, error) {
	var changed, privacyChanged bool
	err := s.inTxDB(ctx, func(tx *sql.Tx, q *sqlc.Queries) error {
		row, err := q.GetDocument(ctx, sqlc.GetDocumentParams{WorkspaceID: scope.WorkspaceID(), ID: id, Viewer: scope.Viewer()})
		if errors.Is(err, sql.ErrNoRows) || (err == nil && row.DeletedAt.Valid) {
			return nil
		}
		if err != nil {
			return err
		}
		if p.ExpectedModifiedAt != nil && p.ExpectedModifiedAt.UnixMilli() != row.ModifiedAt {
			return nil
		}
		res, err := s.apply(ctx, q, scope, newNamesTx(tx, q, scope.WorkspaceID()), row, p, summary(row.Title))
		changed, privacyChanged = res.changed, res.privacy
		return err
	})
	if err == nil && privacyChanged {
		s.privacyChanged(scope.WorkspaceID())
	}
	if err == nil && changed {
		s.notify(scope.WorkspaceID(), []int64{id}, false)
	}
	return changed, err
}

func (s *Service) notify(ws int64, ids []int64, inboxChanged bool) {
	for _, id := range ids {
		s.publish(events.Event{Type: events.DocumentUpdated, WorkspaceID: ws, Data: map[string]any{"documentId": id}})
	}
	if inboxChanged {
		s.publish(events.Event{Type: events.InboxChanged, WorkspaceID: ws})
	}
}

type applied struct{ changed, inbox, privacy bool }

// apply validates p against row, writes it with q and records the audit
// event; summary, when given, is the whole summary (bulk edits, automatic
// changes).
func (s *Service) apply(ctx context.Context, q *sqlc.Queries, scope tenant.Scope, nm *names, row sqlc.Document, p Patch, summary string) (applied, error) {
	ws := row.WorkspaceID
	nm.viewer = scope.Viewer()
	pv := privacy.New(s.db)
	if err := pv.CheckEdit(ctx, q, scope, row.Private == 1, row.PersonID); err != nil {
		return applied{}, err
	}
	changes := map[string][2]any{}
	sources := ParseSources(row.FieldSources)
	before := sources.String()
	upd := sqlc.UpdateDocumentFieldsParams{
		Title: row.Title, TitleSort: row.TitleSort, CreatedDate: row.CreatedDate, DocumentTypeID: row.DocumentTypeID,
		CorrespondentID: row.CorrespondentID, PersonID: row.PersonID, LocationID: row.LocationID, Asn: row.Asn, Inbox: row.Inbox, ModifiedAt: s.opt.Now().UnixMilli(), WorkspaceID: ws, ID: row.ID,
	}
	if p.Title != nil {
		t := strings.TrimSpace(*p.Title)
		if n := utf8.RuneCountInString(t); n < 1 || n > maxTitleLen {
			return applied{}, errTitle
		}
		if t != row.Title {
			changes[i18n.N("标题")] = [2]any{row.Title, t}
			upd.Title, upd.TitleSort = t, pinyin.TitleKey(t)
		}
	}
	if p.CreatedDate != nil {
		if _, err := time.Parse("2006-01-02", *p.CreatedDate); err != nil {
			return applied{}, errDate
		}
		if *p.CreatedDate != row.CreatedDate {
			changes[i18n.N("日期")] = [2]any{row.CreatedDate, *p.CreatedDate}
			upd.CreatedDate = *p.CreatedDate
			sources.Date = keepAlternatives(sources.Date, row.CreatedDate, pick(p.Origin, func(f *FieldSources) *Source { return f.Date }), *p.CreatedDate)
		}
	}
	if p.TypeID != nil {
		next, err := nm.ref(ctx, *p.TypeID, nm.typeName, errType)
		if err != nil {
			return applied{}, err
		}
		if next != row.DocumentTypeID {
			changes[i18n.N("类型")] = [2]any{nm.must(ctx, row.DocumentTypeID, nm.typeName), nm.must(ctx, next, nm.typeName)}
			upd.DocumentTypeID = next
			sources.Type = pick(p.Origin, func(f *FieldSources) *Source { return f.Type })
		}
	}
	if p.CorrespondentID != nil {
		next, err := nm.ref(ctx, *p.CorrespondentID, nm.corrName, errCorr)
		if err != nil {
			return applied{}, err
		}
		if next != row.CorrespondentID {
			changes[i18n.N("往来方")] = [2]any{nm.must(ctx, row.CorrespondentID, nm.corrName), nm.must(ctx, next, nm.corrName)}
			upd.CorrespondentID = next
			sources.Correspondent = pick(p.Origin, func(f *FieldSources) *Source { return f.Correspondent })
		}
	}
	if p.PersonID != nil {
		next, err := nm.ref(ctx, *p.PersonID, nm.personName, errPerson)
		if err != nil {
			return applied{}, err
		}
		if next != row.PersonID {
			changes[i18n.N("家人")] = [2]any{nm.must(ctx, row.PersonID, nm.personName), nm.must(ctx, next, nm.personName)}
			upd.PersonID = next
			sources.Person = pick(p.Origin, func(f *FieldSources) *Source { return f.Person })
		}
	}
	if p.LocationID != nil {
		next, err := nm.ref(ctx, *p.LocationID, nm.locationName, errLocation)
		if err != nil {
			return applied{}, err
		}
		if next != row.LocationID {
			changes[i18n.N("存放位置")] = [2]any{nm.must(ctx, row.LocationID, nm.locationName), nm.must(ctx, next, nm.locationName)}
			upd.LocationID = next
		}
	}
	if p.ASN != nil {
		next := sql.NullInt64{Int64: *p.ASN, Valid: *p.ASN != 0}
		if *p.ASN < 0 || *p.ASN > maxASN {
			return applied{}, errASN
		}
		if next != row.Asn {
			if next.Valid {
				other, err := q.FindDocumentByASN(ctx, sqlc.FindDocumentByASNParams{WorkspaceID: ws, Asn: next})
				if err == nil && other.ID != row.ID {
					if _, verr := q.GetDocumentAnyState(ctx, sqlc.GetDocumentAnyStateParams{WorkspaceID: ws, ID: other.ID, Viewer: scope.Viewer()}); verr != nil {
						// a family member's private document: not named (v1.8.0)
						return applied{}, apperr.WithCode(apperr.KindConflict, "asn_taken", fmt.Sprintf("原件编号 #%06d 已被另一份文档使用", next.Int64))
					}
					if other.DeletedAt.Valid {
						return applied{}, apperr.WithCode(apperr.KindConflict, "asn_taken", fmt.Sprintf("原件编号 #%06d 已被《%s》（在回收站中）使用", next.Int64, other.Title))
					}
					return applied{}, apperr.WithCode(apperr.KindConflict, "asn_taken", fmt.Sprintf("原件编号 #%06d 已被《%s》使用", next.Int64, other.Title))
				} else if err != nil && !errors.Is(err, sql.ErrNoRows) {
					return applied{}, err
				}
			}
			changes[i18n.N("原件编号")] = [2]any{formatASN(row.Asn), formatASN(next)}
			upd.Asn = next
		}
	}
	inboxChanged := false
	if p.Inbox != nil && boolInt(*p.Inbox) != row.Inbox {
		changes[i18n.N("待整理")] = [2]any{row.Inbox == 1, *p.Inbox}
		upd.Inbox, inboxChanged = boolInt(*p.Inbox), true
	}
	if p.TagIDs != nil {
		added, removed, err := s.setTags(ctx, q, nm, row.ID, *p.TagIDs, changes)
		if err != nil {
			return applied{}, err
		}
		if len(added)+len(removed) > 0 {
			sources = sources.clone()
			if sources.Tags == nil {
				sources.Tags = map[int64]Source{}
			}
			for _, id := range added {
				if src := pick(p.Origin, func(f *FieldSources) *Source { return tagSource(f, id) }); src != nil {
					sources.Tags[id] = *src
				} else {
					delete(sources.Tags, id)
				}
			}
			for _, id := range removed {
				if p.Origin == nil {
					sources.Tags[id] = userSource
				} else {
					delete(sources.Tags, id)
				}
			}
		}
	}
	for _, f := range p.CustomFields {
		if err := s.setField(ctx, q, nm, row.ID, f, changes); err != nil {
			return applied{}, err
		}
	}
	if p.Milestone != nil && boolInt(*p.Milestone) != row.IsMilestone {
		changes[i18n.N("大事")] = [2]any{row.IsMilestone == 1, *p.Milestone}
		if err := q.SetDocumentMilestone(ctx, sqlc.SetDocumentMilestoneParams{IsMilestone: boolInt(*p.Milestone), WorkspaceID: ws, ID: row.ID}); err != nil {
			return applied{}, err
		}
	}
	if p.Sensitive != nil && boolInt(*p.Sensitive) != row.Sensitive {
		changes[i18n.N("敏感")] = [2]any{row.Sensitive == 1, *p.Sensitive}
		if err := q.SetDocumentSensitive(ctx, sqlc.SetDocumentSensitiveParams{Sensitive: boolInt(*p.Sensitive), WorkspaceID: ws, ID: row.ID}); err != nil {
			return applied{}, err
		}
	}
	if err := s.applyPrivacy(ctx, q, pv, scope, row, upd, p, changes); err != nil {
		return applied{}, err
	}
	if p.Content != nil && *p.Content != row.Content {
		if utf8.RuneCountInString(*p.Content) > maxContent {
			return applied{}, errContent
		}
		// the history keeps sizes, not the text itself
		was, now := textSize(row.Content), textSize(*p.Content)
		if was == now {
			now += "（改了内容）"
		}
		changes[i18n.N("识别文字")] = [2]any{was, now}
		if err := q.SetDocumentContent(ctx, sqlc.SetDocumentContentParams{Content: *p.Content,
			ContentEditedAt: sql.NullInt64{Int64: upd.ModifiedAt, Valid: true}, WorkspaceID: ws, ID: row.ID}); err != nil {
			return applied{}, err
		}
	}
	metadata := len(changes) > 0 && !(len(changes) == 1 && (inboxChanged || changes["大事"] != [2]any{}))
	if len(changes) > 0 {
		if err := q.UpdateDocumentFields(ctx, upd); err != nil {
			return applied{}, err
		}
		if after := sources.String(); after != before {
			if err := q.SetDocumentFieldSources(ctx, sqlc.SetDocumentFieldSourcesParams{FieldSources: after, WorkspaceID: ws, ID: row.ID}); err != nil {
				return applied{}, err
			}
		}
	}
	written := len(changes) > 0
	if err := s.placeDrawer(ctx, nm, row, p, metadata, changes); err != nil {
		return applied{}, err
	}
	if len(changes) == 0 {
		return applied{}, nil
	}
	if !written { // only the drawer changed: still a modification
		if err := q.UpdateDocumentFields(ctx, upd); err != nil {
			return applied{}, err
		}
	}
	if summary == "" {
		summary = updateSummary(audit.ActorFrom(ctx).Name, upd.Title, changes, inboxChanged, upd.Inbox == 0)
	}
	err := audit.Record(ctx, q, audit.Event{
		WorkspaceID: ws, Action: "document.update", TargetType: "document", TargetID: strconv.FormatInt(row.ID, 10),
		Summary: summary, Changes: changes, At: s.opt.Now(),
	})
	moved := changes["私密"] != [2]any{} || (row.Private == 1 && changes["家人"] != [2]any{})
	return applied{changed: true, inbox: inboxChanged, privacy: moved}, err
}

// updateSummary is the activity summary of an edit by hand. Up to three
// changed fields each get a value of the format, so their names can be
// translated (v1.10.1).
func updateSummary(actor, title string, changes map[string][2]any, inboxChanged, filed bool) string {
	if inboxChanged && len(changes) == 1 {
		if filed {
			return fmt.Sprintf(i18n.N("%s 整理了“%s”"), actor, title)
		}
		return fmt.Sprintf(i18n.N("%s 把“%s”放回了待整理"), actor, title)
	}
	keys := make([]string, 0, len(changes))
	for k := range changes {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	switch len(keys) {
	case 2:
		return fmt.Sprintf(i18n.N("%s 修改了“%s”的%s、%s"), actor, title, keys[0], keys[1])
	case 3:
		return fmt.Sprintf(i18n.N("%s 修改了“%s”的%s、%s、%s"), actor, title, keys[0], keys[1], keys[2])
	}
	return fmt.Sprintf(i18n.N("%s 修改了“%s”的%s"), actor, title, strings.Join(keys, "、"))
}

// applyPrivacy turns privacy on or off: by hand (p.Private, the person's
// holders only), or by itself when the type or person changes to a type
// whose documents are private (v1.8.0).
func (s *Service) applyPrivacy(ctx context.Context, q *sqlc.Queries, pv *privacy.Service, scope tenant.Scope, row sqlc.Document, upd sqlc.UpdateDocumentFieldsParams, p Patch, changes map[string][2]any) error {
	was := row.Private == 1
	private, manual := was, row.PrivateManual == 1
	moved := upd.PersonID != row.PersonID || upd.DocumentTypeID != row.DocumentTypeID
	switch {
	case p.Private != nil && *p.Private != was:
		private, manual = *p.Private, !*p.Private
	case moved:
		manual = false
		if !was {
			auto, err := pv.AutoPrivate(ctx, q, row.WorkspaceID, upd.DocumentTypeID, upd.PersonID)
			if err != nil {
				return err
			}
			if auto {
				// by itself: whoever files it may not hold the person
				return s.setPrivate(ctx, q, row, true, false, changes)
			}
		}
	}
	if err := pv.CheckPrivacyChange(ctx, q, scope, was, row.PersonID, private, upd.PersonID); err != nil {
		return err
	}
	if private == was && manual == (row.PrivateManual == 1) {
		return nil
	}
	return s.setPrivate(ctx, q, row, private, manual, changes)
}

func (s *Service) setPrivate(ctx context.Context, q *sqlc.Queries, row sqlc.Document, private, manual bool, changes map[string][2]any) error {
	if private != (row.Private == 1) {
		changes[i18n.N("私密")] = [2]any{row.Private == 1, private}
	}
	return q.SetDocumentPrivate(ctx, sqlc.SetDocumentPrivateParams{Private: boolInt(private), PrivateManual: boolInt(manual), WorkspaceID: row.WorkspaceID, ID: row.ID})
}

// textSize describes recognised text for the history: “约 1,200 字”.
func textSize(s string) string {
	n := utf8.RuneCountInString(strings.TrimSpace(s))
	if n == 0 {
		return "（空）"
	}
	return "约 " + groupDigits(n) + " 字"
}

func groupDigits(n int) string {
	s := strconv.Itoa(n)
	for i := len(s) - 3; i > 0; i -= 3 {
		s = s[:i] + "," + s[i:]
	}
	return s
}

// setTags makes the document's tags want and returns the tags added and
// removed.
// placeDrawer applies a placement by hand, or refiles a document whose
// metadata changed by the rules, and records a change of drawer.
func (s *Service) placeDrawer(ctx context.Context, nm *names, row sqlc.Document, p Patch, metadata bool, changes map[string][2]any) error {
	pl := s.opt.Placer
	if pl == nil || nm.tx == nil {
		return nil
	}
	ws := row.WorkspaceID
	var err error
	switch {
	case p.DrawerAuto:
		err = pl.SetManual(ctx, nm.tx, ws, row.ID, 0, true)
	case p.DrawerID != nil:
		if *p.DrawerID == row.DrawerID.Int64 && row.DrawerManual == 1 {
			return nil
		}
		err = pl.SetManual(ctx, nm.tx, ws, row.ID, *p.DrawerID, false)
	case metadata && row.DrawerManual == 0:
		err = pl.PlaceDocument(ctx, nm.tx, ws, row.ID)
	default:
		return nil
	}
	if err != nil {
		return err
	}
	after, err := nm.q.GetDocumentDrawerState(ctx, sqlc.GetDocumentDrawerStateParams{WorkspaceID: ws, ID: row.ID})
	if err != nil {
		return err
	}
	name := func(id sql.NullInt64) string {
		if !id.Valid {
			return "未入抽屉"
		}
		return pl.Name(ctx, nm.tx, ws, id.Int64)
	}
	if after.DrawerID != row.DrawerID {
		changes[i18n.N("抽屉")] = [2]any{name(row.DrawerID), name(after.DrawerID)}
	} else if after.DrawerManual != row.DrawerManual && (p.DrawerAuto || p.DrawerID != nil) {
		changes[i18n.N("抽屉")] = [2]any{name(row.DrawerID), name(after.DrawerID) + map[bool]string{true: i18n.N("（手动）"), false: i18n.N("（按规则）")}[after.DrawerManual == 1]}
	}
	return nil
}

func (s *Service) setTags(ctx context.Context, q *sqlc.Queries, nm *names, doc int64, want []int64, changes map[string][2]any) (added, removed []int64, err error) {
	have, err := q.ListDocumentTagIDs(ctx, doc)
	if err != nil {
		return nil, nil, err
	}
	want = slices.Compact(slices.Sorted(slices.Values(want)))
	if slices.Equal(have, want) {
		return nil, nil, nil
	}
	for _, id := range want {
		if _, err := nm.tagName(ctx, id); err != nil {
			return nil, nil, errTag
		}
		if !slices.Contains(have, id) {
			if err := q.AddDocumentTag(ctx, sqlc.AddDocumentTagParams{DocumentID: doc, TagID: id}); err != nil {
				return nil, nil, err
			}
			added = append(added, id)
		}
	}
	for _, id := range have {
		if !slices.Contains(want, id) {
			if err := q.RemoveDocumentTag(ctx, sqlc.RemoveDocumentTagParams{DocumentID: doc, TagID: id}); err != nil {
				return nil, nil, err
			}
			removed = append(removed, id)
		}
	}
	changes[i18n.N("标签")] = [2]any{nm.tagList(ctx, have), nm.tagList(ctx, want)}
	return added, removed, nil
}

func tagSource(f *FieldSources, id int64) *Source {
	if s, ok := f.Tags[id]; ok {
		return &s
	}
	return nil
}

// setField writes one custom field value; changes are keyed by the field's
// name and show values as people read them.
func (s *Service) setField(ctx context.Context, q *sqlc.Queries, nm *names, doc int64, in FieldInput, changes map[string][2]any) error {
	f, err := nm.field(ctx, in.FieldID)
	if err != nil {
		return err
	}
	vals, err := q.ListDocumentFieldValues(ctx, doc)
	if err != nil {
		return err
	}
	var before sqlc.CustomFieldValue
	for _, v := range vals {
		if v.FieldID == f.ID {
			before = v
		}
	}
	beforeShown := nm.showValue(ctx, f, before)
	if isNull(in.Value) {
		if beforeShown == "" {
			return nil
		}
		if err := q.DeleteFieldValue(ctx, sqlc.DeleteFieldValueParams{DocumentID: doc, FieldID: f.ID}); err != nil {
			return err
		}
		changes[f.Name] = [2]any{beforeShown, ""}
		return nil
	}
	v := sqlc.UpsertFieldValueParams{DocumentID: doc, FieldID: f.ID}
	bad := apperr.New(apperr.KindInvalid, fmt.Sprintf("“%s”的值不正确", f.Name))
	switch f.DataType {
	case "text", "url":
		var t string
		if json.Unmarshal(in.Value, &t) != nil || utf8.RuneCountInString(t) > maxTextLen {
			return bad
		}
		t = strings.TrimSpace(t)
		if f.DataType == "url" {
			if u, err := url.Parse(t); err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
				return apperr.New(apperr.KindInvalid, fmt.Sprintf("“%s”应为 http 或 https 开头的网址", f.Name))
			}
		}
		if t == "" {
			return s.setField(ctx, q, nm, doc, FieldInput{FieldID: f.ID, Value: json.RawMessage("null")}, changes)
		}
		v.ValueText = sql.NullString{String: t, Valid: true}
	case "integer":
		var n int64
		if json.Unmarshal(in.Value, &n) != nil {
			return bad
		}
		v.ValueInt = sql.NullInt64{Int64: n, Valid: true}
	case "float":
		var n float64
		if json.Unmarshal(in.Value, &n) != nil {
			return bad
		}
		v.ValueFloat = sql.NullFloat64{Float64: n, Valid: true}
	case "boolean":
		var b bool
		if json.Unmarshal(in.Value, &b) != nil {
			return bad
		}
		v.ValueBool = sql.NullInt64{Int64: boolInt(b), Valid: true}
	case "date":
		var d string
		if json.Unmarshal(in.Value, &d) != nil {
			return bad
		}
		if _, err := time.Parse("2006-01-02", d); err != nil {
			return errDate
		}
		v.ValueDate = sql.NullString{String: d, Valid: true}
	case "monetary":
		var m string
		if json.Unmarshal(in.Value, &m) != nil {
			return bad
		}
		n, err := filter.ParseMoney(m)
		if err != nil {
			return apperr.New(apperr.KindInvalid, fmt.Sprintf("“%s”应为金额，最多两位小数", f.Name))
		}
		v.ValueMonetary = sql.NullInt64{Int64: n, Valid: true}
	case "select":
		var o string
		if json.Unmarshal(in.Value, &o) != nil || f.optionLabel(o) == "" {
			return bad
		}
		v.ValueSelect = sql.NullString{String: o, Valid: true}
	case "document_link":
		var target int64
		if json.Unmarshal(in.Value, &target) != nil {
			return bad
		}
		if target == doc {
			return errLinkSelf
		}
		if _, err := nm.docTitle(ctx, target); err != nil {
			return errLinkTarget
		}
		v.ValueDoclink = sql.NullInt64{Int64: target, Valid: true}
	default:
		return bad
	}
	after := nm.showValue(ctx, f, sqlc.CustomFieldValue{FieldID: f.ID, ValueText: v.ValueText, ValueInt: v.ValueInt, ValueFloat: v.ValueFloat,
		ValueBool: v.ValueBool, ValueDate: v.ValueDate, ValueMonetary: v.ValueMonetary, ValueSelect: v.ValueSelect, ValueDoclink: v.ValueDoclink})
	if after == beforeShown {
		return nil
	}
	if err := q.UpsertFieldValue(ctx, v); err != nil {
		return err
	}
	changes[f.Name] = [2]any{beforeShown, after}
	return nil
}

func isNull(raw json.RawMessage) bool {
	return len(raw) == 0 || strings.TrimSpace(string(raw)) == "null"
}

func formatASN(n sql.NullInt64) string {
	if !n.Valid {
		return ""
	}
	return fmt.Sprintf("#%06d", n.Int64)
}

func boolInt(b bool) int64 {
	if b {
		return 1
	}
	return 0
}

// names looks up and caches what audit changes and validation need within
// one transaction.
type names struct {
	tx     sqlc.DBTX // the transaction, for the Placer; nil skips drawers
	q      *sqlc.Queries
	ws     int64
	types  map[int64]string
	corrs  map[int64]string
	people map[int64]string
	places map[int64]string
	tags   map[int64]string
	fields map[int64]*fieldInfo
	docs   map[int64]string
	// viewer looks up linked documents (v1.8.0): a private one the editor
	// may not see is not there. The zero value would be an outsider, so
	// newNames sets the system.
	viewer int64
}

type fieldInfo struct {
	ID       int64
	Name     string
	DataType string
	Options  []struct {
		ID    string `json:"id"`
		Label string `json:"label"`
	} `json:"options"`
	Currency string `json:"currency"`
}

func (f *fieldInfo) optionLabel(id string) string {
	for _, o := range f.Options {
		if o.ID == id {
			return o.Label
		}
	}
	return ""
}

func newNamesTx(tx sqlc.DBTX, q *sqlc.Queries, ws int64) *names {
	nm := newNames(q, ws)
	nm.tx = tx
	return nm
}

func newNames(q *sqlc.Queries, ws int64) *names {
	return &names{q: q, ws: ws, types: map[int64]string{}, corrs: map[int64]string{}, people: map[int64]string{}, places: map[int64]string{}, tags: map[int64]string{}, fields: map[int64]*fieldInfo{}, docs: map[int64]string{}, viewer: tenant.ViewerSystem}
}

func cached(ctx context.Context, m map[int64]string, id int64, load func() (string, error)) (string, error) {
	if n, ok := m[id]; ok {
		return n, nil
	}
	n, err := load()
	if err != nil {
		return "", err
	}
	m[id] = n
	return n, nil
}

func (nm *names) typeName(ctx context.Context, id int64) (string, error) {
	return cached(ctx, nm.types, id, func() (string, error) {
		r, err := nm.q.GetDocumentType(ctx, sqlc.GetDocumentTypeParams{WorkspaceID: nm.ws, ID: id})
		return r.Name, err
	})
}

func (nm *names) corrName(ctx context.Context, id int64) (string, error) {
	return cached(ctx, nm.corrs, id, func() (string, error) {
		r, err := nm.q.GetCorrespondent(ctx, sqlc.GetCorrespondentParams{WorkspaceID: nm.ws, ID: id})
		return r.Name, err
	})
}

func (nm *names) personName(ctx context.Context, id int64) (string, error) {
	return cached(ctx, nm.people, id, func() (string, error) {
		r, err := nm.q.GetPerson(ctx, sqlc.GetPersonParams{WorkspaceID: nm.ws, ID: id})
		return r.Name, err
	})
}

func (nm *names) locationName(ctx context.Context, id int64) (string, error) {
	return cached(ctx, nm.places, id, func() (string, error) {
		r, err := nm.q.GetLocation(ctx, sqlc.GetLocationParams{WorkspaceID: nm.ws, ID: id})
		return r.Name, err
	})
}

func (nm *names) tagName(ctx context.Context, id int64) (string, error) {
	return cached(ctx, nm.tags, id, func() (string, error) {
		r, err := nm.q.GetTag(ctx, sqlc.GetTagParams{WorkspaceID: nm.ws, ID: id})
		return r.Name, err
	})
}

func (nm *names) docTitle(ctx context.Context, id int64) (string, error) {
	return cached(ctx, nm.docs, id, func() (string, error) {
		r, err := nm.q.GetDocument(ctx, sqlc.GetDocumentParams{WorkspaceID: nm.ws, ID: id, Viewer: nm.viewer})
		return r.Title, err
	})
}

func (nm *names) field(ctx context.Context, id int64) (*fieldInfo, error) {
	if f, ok := nm.fields[id]; ok {
		return f, nil
	}
	r, err := nm.q.GetCustomField(ctx, sqlc.GetCustomFieldParams{WorkspaceID: nm.ws, ID: id})
	if errors.Is(err, sql.ErrNoRows) {
		return nil, errField
	}
	if err != nil {
		return nil, err
	}
	f := &fieldInfo{}
	_ = json.Unmarshal([]byte(r.Extra), f)
	f.ID, f.Name, f.DataType = r.ID, r.Name, r.DataType
	nm.fields[id] = f
	return f, nil
}

// ref validates a classification ID (0 clears it).
func (nm *names) ref(ctx context.Context, id int64, lookup func(context.Context, int64) (string, error), missing error) (sql.NullInt64, error) {
	if id == 0 {
		return sql.NullInt64{}, nil
	}
	if _, err := lookup(ctx, id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return sql.NullInt64{}, missing
		}
		return sql.NullInt64{}, err
	}
	return sql.NullInt64{Int64: id, Valid: true}, nil
}

// must names a set classification ("" when unset or gone).
func (nm *names) must(ctx context.Context, id sql.NullInt64, lookup func(context.Context, int64) (string, error)) string {
	if !id.Valid {
		return ""
	}
	n, _ := lookup(ctx, id.Int64)
	return n
}

func (nm *names) tagList(ctx context.Context, ids []int64) string {
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		if n, err := nm.tagName(ctx, id); err == nil {
			out = append(out, n)
		}
	}
	slices.Sort(out)
	return strings.Join(out, "、")
}

// showValue renders a stored value for the history ("" when unset).
func (nm *names) showValue(ctx context.Context, f *fieldInfo, v sqlc.CustomFieldValue) string {
	switch f.DataType {
	case "text", "url":
		return v.ValueText.String
	case "integer":
		if v.ValueInt.Valid {
			return strconv.FormatInt(v.ValueInt.Int64, 10)
		}
	case "float":
		if v.ValueFloat.Valid {
			return strconv.FormatFloat(v.ValueFloat.Float64, 'f', -1, 64)
		}
	case "boolean":
		if v.ValueBool.Valid {
			return map[bool]string{true: "是", false: "否"}[v.ValueBool.Int64 == 1]
		}
	case "date":
		return v.ValueDate.String
	case "monetary":
		if v.ValueMonetary.Valid {
			return filter.FormatMoney(v.ValueMonetary.Int64)
		}
	case "select":
		if v.ValueSelect.Valid {
			return f.optionLabel(v.ValueSelect.String)
		}
	case "document_link":
		if v.ValueDoclink.Valid {
			t, err := nm.docTitle(ctx, v.ValueDoclink.Int64)
			if err != nil {
				return "（已删除的文档）"
			}
			return "《" + t + "》"
		}
	}
	return ""
}

// keepAlternatives carries the dates found in the document over to its new
// date's source (v1.4.4), with the old date among them, so the details
// page can still offer them after a change.
func keepAlternatives(old *Source, oldDate string, next *Source, nextDate string) *Source {
	if old == nil || (len(old.Alternatives) == 0 && old.Source != SourceText && old.Source != SourceFile) {
		return next
	}
	var alts []DateOption
	seen := map[string]bool{nextDate: true}
	add := func(o DateOption) {
		if !seen[o.Date] && len(alts) < 5 {
			seen[o.Date] = true
			alts = append(alts, o)
		}
	}
	if old.Source == SourceText || old.Source == SourceFile {
		add(DateOption{Date: oldDate, Basis: old.Basis})
	}
	for _, o := range old.Alternatives {
		add(o)
	}
	if len(alts) == 0 {
		return next
	}
	out := Source{Source: SourceUser}
	if next != nil {
		out = *next
	}
	out.Alternatives = alts
	return &out
}

func (s *Service) privacyChanged(ws int64) {
	if s.opt.OnPrivacyChange != nil {
		s.opt.OnPrivacyChange(ws)
	}
}
