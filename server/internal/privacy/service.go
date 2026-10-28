package privacy

import (
	"context"
	"database/sql"
	"errors"
	"slices"
	"time"

	"kept/server/internal/apperr"
	"kept/server/internal/store/sqlc"
	"kept/server/internal/tenant"
)

// Levels of person_access.
const (
	LevelSelf     = "self"     // the person's own account
	LevelGuardian = "guardian" // looks after a person without an account
	LevelManage   = "manage"   // granted: sees and edits
	LevelRead     = "read"     // granted: only sees
)

// ViaEmergency marks access granted by emergency access.
const ViaEmergency = "emergency"

var (
	ErrPersonNotFound = apperr.New(apperr.KindNotFound, "找不到这位家人")
	errNotMember      = apperr.New(apperr.KindInvalid, "只能选择这个空间的成员")
	errLevel          = apperr.New(apperr.KindInvalid, "权限只能是本人、照看人、代管或只读")
	errNotHolder      = apperr.New(apperr.KindForbidden, "只有这位家人本人（没有本人时是照看人）可以管理谁能看")
	errHasSelf        = apperr.New(apperr.KindConflict, "这位家人已经关联了账号，先解除再换")
	errSelfTaken      = apperr.New(apperr.KindConflict, "这个账号已经关联了空间里的另一位家人")
	errSelfRow        = apperr.New(apperr.KindInvalid, "本人的权限不能改，只能由本人解除关联")
	errSelfOnly       = apperr.New(apperr.KindForbidden, "只有本人可以解除自己和这位家人的关联")
	errLastHolder     = apperr.New(apperr.KindConflict, "这位家人还有私密文档，先指定照看人或取消这些文档的私密，再解除")
	errUnclaimedOwner = apperr.New(apperr.KindForbidden, "这位家人有私密文档但没有人负责，只有空间拥有者可以指定本人或照看人")
	errReadOnly       = apperr.New(apperr.KindForbidden, "你的角色只能查看")
	errNoHolder       = apperr.New(apperr.KindInvalid, "这位家人还没有关联账号或照看人，先在家人页指定，再设为私密")
	errNeedPerson     = apperr.New(apperr.KindInvalid, "私密文档要属于一位家人，请先选择家人")
	errMergePrivate   = apperr.New(apperr.KindForbidden, "要合并的家人有私密文档：只有同时负责这几位家人的人可以合并")
	errNotYours       = apperr.New(apperr.KindForbidden, "只有这位家人本人（没有本人时是照看人）可以设置或取消私密")
	// ErrPrivateEdit refuses changes to a private document the viewer may
	// only look at.
	ErrPrivateEdit = apperr.WithCode(apperr.KindForbidden, "private_read_only", "这是家人的私密文档，你只能查看")
	// ErrPersonHasPrivate refuses deleting a person who still has private
	// documents: they would show to everyone.
	ErrPersonHasPrivate = apperr.New(apperr.KindConflict, "这位家人还有私密文档，先取消私密或把文档换给别的家人，再删除")
)

// Service manages who may see private documents.
type Service struct {
	db   *sql.DB
	q    *sqlc.Queries
	now  func() time.Time
	mail Mailer
}

func New(db *sql.DB) *Service {
	return &Service{db: db, q: sqlc.New(db), now: time.Now}
}

// SetClock replaces the clock (tests).
func (s *Service) SetClock(now func() time.Time) { s.now = now }

// Access is one member's access to a person's private documents.
type Access struct {
	UserID      int64
	DisplayName string
	Username    string
	Level       string
	Via         string
	Reviewed    bool
	CreatedAt   time.Time
}

// Person is a family member's privacy as the viewer sees it.
type Person struct {
	ID int64
	// Self is the linked account, nil if none (everyone sees who it is:
	// it is the family member's own name).
	Self *Access
	// MyLevel is the viewer's level, "" none.
	MyLevel string
	// Holder: the viewer manages who may see (self, or a guardian when
	// there is no self).
	Holder bool
	// Claimable: nobody looks after the person yet and the viewer may link
	// an account or name a guardian.
	Claimable bool
	// Access lists everyone with access; only holders and the people
	// listed see it, others get nil.
	Access []Access
	// Private counts the person's private documents (trash included); for
	// people who may not see them only as "has some".
	Private int64
	// NeedsReview: the viewer is the person and should decide whether to
	// keep the guardians named before the account was linked.
	NeedsReview bool
	// Emergency lists the emergency contacts: all of them for holders,
	// only their own for a contact, nil for others.
	Emergency []Emergency
	// Key is the person's key (v1.8.1), nil without one; CloudAI: they let
	// cloud models read their private documents.
	Key     *KeyInfo
	CloudAI bool
}

type state struct {
	rows    []Access // current members only
	private int64
}

func (st state) self() *Access {
	for i := range st.rows {
		if st.rows[i].Level == LevelSelf {
			return &st.rows[i]
		}
	}
	return nil
}

func (st state) level(user int64) string {
	for _, a := range st.rows {
		if a.UserID == user {
			return a.Level
		}
	}
	return ""
}

func (st state) holders() []int64 {
	if self := st.self(); self != nil {
		return []int64{self.UserID}
	}
	var out []int64
	for _, a := range st.rows {
		if a.Level == LevelGuardian {
			out = append(out, a.UserID)
		}
	}
	return out
}

func (st state) isHolder(user int64) bool { return user > 0 && slices.Contains(st.holders(), user) }

func (st state) claimed() bool { return len(st.holders()) > 0 }

func (s *Service) load(ctx context.Context, q *sqlc.Queries, ws int64) (map[int64]*state, error) {
	rows, err := q.ListPersonAccess(ctx, ws)
	if err != nil {
		return nil, err
	}
	out := map[int64]*state{}
	for _, r := range rows {
		st := out[r.PersonID]
		if st == nil {
			st = &state{}
			out[r.PersonID] = st
		}
		st.rows = append(st.rows, Access{
			UserID: r.UserID, DisplayName: r.DisplayName, Username: r.Username, Level: r.Level, Via: r.Via,
			Reviewed: r.Reviewed == 1, CreatedAt: time.UnixMilli(r.CreatedAt),
		})
	}
	return out, nil
}

func (s *Service) person(ctx context.Context, q *sqlc.Queries, scope tenant.Scope, id int64) (state, error) {
	if _, err := q.GetPerson(ctx, sqlc.GetPersonParams{WorkspaceID: scope.WorkspaceID(), ID: id}); errors.Is(err, sql.ErrNoRows) {
		return state{}, ErrPersonNotFound
	} else if err != nil {
		return state{}, err
	}
	all, err := s.load(ctx, q, scope.WorkspaceID())
	if err != nil {
		return state{}, err
	}
	st := state{}
	if p := all[id]; p != nil {
		st = *p
	}
	st.private, err = q.CountPrivateDocuments(ctx, sql.NullInt64{Int64: id, Valid: true})
	return st, err
}

// People returns the privacy of every family member in the workspace, as
// the scope's viewer sees it.
func (s *Service) People(ctx context.Context, scope tenant.Scope) (map[int64]Person, error) {
	persons, err := s.q.ListPersons(ctx, scope.WorkspaceID())
	if err != nil {
		return nil, err
	}
	if err := s.GrantDue(ctx); err != nil {
		return nil, err
	}
	all, err := s.load(ctx, s.q, scope.WorkspaceID())
	if err != nil {
		return nil, err
	}
	em, err := s.emergencies(ctx, s.q, scope.WorkspaceID())
	if err != nil {
		return nil, err
	}
	counts, err := s.privateCounts(ctx, scope.WorkspaceID())
	if err != nil {
		return nil, err
	}
	keys, err := s.Keys(ctx, scope)
	if err != nil {
		return nil, err
	}
	me := scope.User()
	out := make(map[int64]Person, len(persons))
	for _, p := range persons {
		st := state{private: counts[p.ID]}
		if a := all[p.ID]; a != nil {
			st.rows = a.rows
		}
		v := s.view(scope, me, p.ID, st)
		for _, e := range em[p.ID] {
			if v.Holder || e.UserID == me {
				v.Emergency = append(v.Emergency, e)
			}
		}
		if k, ok := keys[p.ID]; ok {
			v.CloudAI = k.CloudAI
			if k.Has {
				kk := k
				v.Key = &kk
			}
		}
		out[p.ID] = v
	}
	return out, nil
}

func (s *Service) view(scope tenant.Scope, me, id int64, st state) Person {
	p := Person{ID: id, MyLevel: st.level(me), Holder: st.isHolder(me), Private: st.private}
	if self := st.self(); self != nil {
		c := *self
		p.Self = &c
		p.NeedsReview = self.UserID == me && !self.Reviewed && len(st.rows) > 1
	}
	if p.MyLevel != "" || scope.Viewer() == tenant.ViewerSystem {
		p.Access = slices.Clone(st.rows)
	}
	p.Claimable = s.claimable(scope, st) == nil
	return p
}

// claimable reports whether the viewer may link an account or name a
// guardian to a person nobody looks after: anyone who may write while the
// person has no private documents, only an owner once they have.
func (s *Service) claimable(scope tenant.Scope, st state) error {
	if st.claimed() {
		return errNotHolder
	}
	if !scope.CanWrite() {
		return errReadOnly
	}
	if st.private > 0 && scope.Role() != tenant.RoleOwner {
		return errUnclaimedOwner
	}
	return nil
}

func (s *Service) privateCounts(ctx context.Context, ws int64) (map[int64]int64, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT person_id, COUNT(*) FROM documents
		WHERE workspace_id = ? AND private = 1 AND person_id IS NOT NULL GROUP BY person_id`, ws)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]int64{}
	for rows.Next() {
		var id, n int64
		if err := rows.Scan(&id, &n); err != nil {
			return nil, err
		}
		out[id] = n
	}
	return out, rows.Err()
}

// RemoveAccess takes away userID's access. Holders remove anyone but the
// person themself; everyone may remove their own access, the person only
// their own link. The last holder of a person with private documents
// stays, or the documents would belong to nobody.
func (s *Service) RemoveAccess(ctx context.Context, scope tenant.Scope, personID, userID int64) error {
	return s.inTx(ctx, func(q *sqlc.Queries) error {
		st, err := s.person(ctx, q, scope, personID)
		if err != nil {
			return err
		}
		me := scope.User()
		level := st.level(userID)
		if level == "" {
			return nil
		}
		switch {
		case level == LevelSelf && userID != me:
			return errSelfOnly
		case userID == me, st.isHolder(me):
		default:
			return errNotHolder
		}
		if st.private > 0 && slices.Contains(st.holders(), userID) && len(st.holders()) == 1 {
			// a self leaving with guardians around hands over to them
			hasGuardian := false
			for _, a := range st.rows {
				hasGuardian = hasGuardian || (a.Level == LevelGuardian && a.UserID != userID)
			}
			if !hasGuardian {
				return errLastHolder
			}
		}
		if _, err := q.DeletePersonAccess(ctx, sqlc.DeletePersonAccessParams{PersonID: personID, UserID: userID}); err != nil {
			return err
		}
		if level == LevelSelf {
			return nil
		}
		// an emergency grant ends with the access
		_, err = q.DeletePersonEmergency(ctx, sqlc.DeletePersonEmergencyParams{PersonID: personID, UserID: userID})
		return err
	})
}

// Reviewed records that the person decided about the guardians named
// before their account was linked.
func (s *Service) Reviewed(ctx context.Context, scope tenant.Scope, personID int64) error {
	st, err := s.person(ctx, s.q, scope, personID)
	if err != nil {
		return err
	}
	if self := st.self(); self == nil || self.UserID != scope.User() {
		return errSelfOnly
	}
	return s.q.SetPersonAccessReviewed(ctx, sqlc.SetPersonAccessReviewedParams{Reviewed: 1, PersonID: personID})
}

// Uploader is a member who uploaded someone's private documents and still
// sees them.
type Uploader struct {
	PersonID    int64
	UserID      int64
	DisplayName string
	Documents   int64
}

// Uploaders lists, for the persons the viewer holds, who uploaded their
// private documents and may still see them.
func (s *Service) Uploaders(ctx context.Context, scope tenant.Scope) ([]Uploader, error) {
	people, err := s.People(ctx, scope)
	if err != nil {
		return nil, err
	}
	rows, err := s.q.ListUploaderAccess(ctx, scope.WorkspaceID())
	if err != nil {
		return nil, err
	}
	out := []Uploader{}
	for _, r := range rows {
		if !people[r.PersonID.Int64].Holder {
			continue
		}
		out = append(out, Uploader{PersonID: r.PersonID.Int64, UserID: r.AddedBy.Int64, DisplayName: r.DisplayName, Documents: r.N})
	}
	return out, nil
}

// RevokeUploader takes back an uploader's access to all of a person's
// private documents they uploaded.
func (s *Service) RevokeUploader(ctx context.Context, scope tenant.Scope, personID, userID int64) (int64, error) {
	st, err := s.person(ctx, s.q, scope, personID)
	if err != nil {
		return 0, err
	}
	if !st.isHolder(scope.User()) {
		return 0, errNotHolder
	}
	return s.q.RevokeUploaderAccess(ctx, sqlc.RevokeUploaderAccessParams{
		WorkspaceID: scope.WorkspaceID(), PersonID: sql.NullInt64{Int64: personID, Valid: true},
		AddedBy: sql.NullInt64{Int64: userID, Valid: true},
	})
}

// Level returns the viewer's level for a person ("" none).
func (s *Service) Level(ctx context.Context, q *sqlc.Queries, personID, userID int64) (string, error) {
	if q == nil {
		q = s.q
	}
	if userID <= 0 {
		return "", nil
	}
	l, err := q.GetPersonAccess(ctx, sqlc.GetPersonAccessParams{PersonID: personID, UserID: userID})
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return l, err
}

// CanEdit reports whether the scope may change a document with these
// privacy columns: anyone who may write, for a document that is not
// private; for a private one only the person, guardians and managers.
func (s *Service) CanEdit(ctx context.Context, q *sqlc.Queries, scope tenant.Scope, private bool, personID sql.NullInt64) (bool, error) {
	if q == nil {
		q = s.q
	}
	if !scope.CanWrite() {
		return false, nil
	}
	if !private || scope.Viewer() == tenant.ViewerSystem {
		return true, nil
	}
	if !personID.Valid {
		return false, nil
	}
	l, err := s.Level(ctx, q, personID.Int64, scope.User())
	return l == LevelSelf || l == LevelGuardian || l == LevelManage, err
}

// CheckEdit is CanEdit as an error.
func (s *Service) CheckEdit(ctx context.Context, q *sqlc.Queries, scope tenant.Scope, private bool, personID sql.NullInt64) error {
	if q == nil {
		q = s.q
	}
	ok, err := s.CanEdit(ctx, q, scope, private, personID)
	if err == nil && !ok && private {
		return ErrPrivateEdit
	}
	return err
}

// IsHolder reports whether the viewer manages who may see a person's
// private documents.
func (s *Service) IsHolder(ctx context.Context, q *sqlc.Queries, scope tenant.Scope, personID int64) (bool, error) {
	if q == nil {
		q = s.q
	}
	if scope.Viewer() == tenant.ViewerSystem {
		return true, nil
	}
	all, err := s.load(ctx, q, scope.WorkspaceID())
	if err != nil {
		return false, err
	}
	st := all[personID]
	return st != nil && st.isHolder(scope.User()), nil
}

// Claimed reports whether someone looks after a person (a linked account
// or a guardian who is still a member).
func (s *Service) Claimed(ctx context.Context, q *sqlc.Queries, ws, personID int64) (bool, error) {
	if q == nil {
		q = s.q
	}
	all, err := s.load(ctx, q, ws)
	if err != nil {
		return false, err
	}
	st := all[personID]
	return st != nil && st.claimed(), nil
}

// CheckPrivacyChange checks that the viewer may make a document private,
// make it public again, or move a private document to another person:
// only holders of the persons involved may.
func (s *Service) CheckPrivacyChange(ctx context.Context, q *sqlc.Queries, scope tenant.Scope, wasPrivate bool, oldPerson sql.NullInt64, private bool, newPerson sql.NullInt64) error {
	if q == nil {
		q = s.q
	}
	if private && !newPerson.Valid {
		return errNeedPerson
	}
	if scope.Viewer() == tenant.ViewerSystem {
		return nil
	}
	check := func(p sql.NullInt64) error {
		ok, err := s.IsHolder(ctx, q, scope, p.Int64)
		if err != nil {
			return err
		}
		if !ok {
			claimed, err := s.Claimed(ctx, q, scope.WorkspaceID(), p.Int64)
			if err != nil {
				return err
			}
			if !claimed {
				return errNoHolder
			}
			return errNotYours
		}
		return nil
	}
	if wasPrivate && oldPerson.Valid && (!private || oldPerson != newPerson) {
		if err := check(oldPerson); err != nil {
			return err
		}
	}
	if private && (!wasPrivate || oldPerson != newPerson) {
		return check(newPerson)
	}
	return nil
}

// AutoPrivate reports whether a document of this type and person becomes
// private by itself: the type says so and someone looks after the person.
func (s *Service) AutoPrivate(ctx context.Context, q *sqlc.Queries, ws int64, typeID, personID sql.NullInt64) (bool, error) {
	if q == nil {
		q = s.q
	}
	if !typeID.Valid || !personID.Valid {
		return false, nil
	}
	n, err := q.IsPrivateType(ctx, typeID.Int64)
	if err != nil || n == 0 {
		return false, err
	}
	return s.Claimed(ctx, q, ws, personID.Int64)
}

// CheckDeletePerson refuses deleting a person with private documents.
func (s *Service) CheckDeletePerson(ctx context.Context, q *sqlc.Queries, personID int64) error {
	if q == nil {
		q = s.q
	}
	n, err := q.CountPrivateDocuments(ctx, sql.NullInt64{Int64: personID, Valid: true})
	if err == nil && n > 0 {
		return ErrPersonHasPrivate
	}
	return err
}

// CheckMergePersons allows merging persons whose private documents move to
// keep only for someone who holds all of them; the access granted on the
// merged persons goes away with them.
func (s *Service) CheckMergePersons(ctx context.Context, q *sqlc.Queries, scope tenant.Scope, keep int64, merge []int64) error {
	if q == nil {
		q = s.q
	}
	moving := false
	for _, id := range merge {
		n, err := q.CountPrivateDocuments(ctx, sql.NullInt64{Int64: id, Valid: true})
		if err != nil {
			return err
		}
		if n == 0 {
			continue
		}
		moving = true
		if ok, err := s.IsHolder(ctx, q, scope, id); err != nil {
			return err
		} else if !ok {
			return errMergePrivate
		}
	}
	if !moving {
		return nil
	}
	if ok, err := s.IsHolder(ctx, q, scope, keep); err != nil {
		return err
	} else if !ok {
		return errMergePrivate
	}
	return nil
}

// Usage is how many private documents a workspace keeps and their size,
// for owners who may not see them.
func (s *Service) Usage(ctx context.Context, ws int64) (int64, int64, error) {
	r, err := s.q.PrivateUsage(ctx, ws)
	return r.N, r.Bytes, err
}

// PrivateTypes lists the document types that become private by themselves.
func (s *Service) PrivateTypes(ctx context.Context, ws int64) (map[int64]bool, error) {
	ids, err := s.q.ListPrivateTypes(ctx, ws)
	out := make(map[int64]bool, len(ids))
	for _, id := range ids {
		out[id] = true
	}
	return out, err
}

// SetPrivateType says whether documents of a type become private.
func (s *Service) SetPrivateType(ctx context.Context, q *sqlc.Queries, ws, typeID int64, on bool) error {
	if q == nil {
		q = s.q
	}
	if on {
		return q.SetPrivateType(ctx, sqlc.SetPrivateTypeParams{DocumentTypeID: typeID, WorkspaceID: ws})
	}
	return q.UnsetPrivateType(ctx, typeID)
}

// MemberLeft removes a member's access in a workspace they left.
func MemberLeft(ctx context.Context, q *sqlc.Queries, ws, userID int64) error {
	if err := q.DeleteMemberPersonAccess(ctx, sqlc.DeleteMemberPersonAccessParams{WorkspaceID: ws, UserID: userID}); err != nil {
		return err
	}
	return q.DeleteMemberPersonEmergency(ctx, sqlc.DeleteMemberPersonEmergencyParams{WorkspaceID: ws, UserID: userID})
}

func (s *Service) member(ctx context.Context, q *sqlc.Queries, ws, userID int64) error {
	_, err := q.GetWorkspaceMember(ctx, sqlc.GetWorkspaceMemberParams{WorkspaceID: ws, UserID: userID})
	if errors.Is(err, sql.ErrNoRows) {
		return errNotMember
	}
	return err
}

func (s *Service) selfOf(ctx context.Context, q *sqlc.Queries, ws, userID int64) (int64, error) {
	id, err := q.GetSelfPerson(ctx, sqlc.GetSelfPersonParams{WorkspaceID: ws, UserID: userID})
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	return id, err
}

func (s *Service) inTx(ctx context.Context, fn func(*sqlc.Queries) error) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := fn(s.q.WithTx(tx)); err != nil {
		return err
	}
	return tx.Commit()
}
