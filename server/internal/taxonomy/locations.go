package taxonomy

import (
	"context"
	"database/sql"
	"errors"
	"kept/server/internal/i18n"
	"strings"
	"unicode/utf8"

	"kept/server/internal/apperr"
	"kept/server/internal/store/sqlc"
	"kept/server/internal/tenant"
)

// Locations (v1.6.3, after Homebox's locations): where a paper original
// is kept — 书房柜子第 2 层, 文件盒 A, the bank's safe deposit box. The
// archive serial number says which original; the location says which box
// to open. A location is not learned from the text, so it has no rule.

// Location is a place originals are kept.
type Location struct {
	ID        int64
	Name      string
	SortKey   string
	Initial   string
	Note      string
	Documents int // documents kept there (not in the trash)
}

// LocationInput creates or replaces a location.
type LocationInput struct {
	Name string
	Note string
}

const (
	locationKind  Kind = "location"
	maxLocNoteLen      = 200
	maxASNRange        = 100000
)

var (
	errLocNote  = apperr.New(apperr.KindInvalid, "说明最多 200 个字")
	errASNRange = apperr.New(apperr.KindInvalid, "原件编号的范围不正确")
)

func toLocation(r sqlc.Location, n int) Location {
	_, initial := sortFields(r.Name)
	return Location{ID: r.ID, Name: r.Name, SortKey: r.SortKey, Initial: initial, Note: r.Note, Documents: n}
}

// ListLocations returns the workspace's locations in pinyin order with
// how many documents each holds.
func (s *Service) ListLocations(ctx context.Context, scope tenant.Scope) ([]Location, error) {
	ws := scope.WorkspaceID()
	rows, err := s.q.ListLocations(ctx, ws)
	if err != nil {
		return nil, err
	}
	counts, err := s.q.CountDocumentsByLocation(ctx, sqlc.CountDocumentsByLocationParams{WorkspaceID: ws, Viewer: scope.Viewer()})
	if err != nil {
		return nil, err
	}
	n := map[int64]int{}
	for _, c := range counts {
		n[c.LocationID.Int64] = int(c.N)
	}
	out := make([]Location, len(rows))
	for i, r := range rows {
		out[i] = toLocation(r, n[r.ID])
	}
	return out, nil
}

func normalizeLocation(in LocationInput) (LocationInput, error) {
	name, err := normalizeName(in.Name)
	if err != nil {
		return in, err
	}
	in.Name, in.Note = name, strings.TrimSpace(in.Note)
	if utf8.RuneCountInString(in.Note) > maxLocNoteLen {
		return in, errLocNote
	}
	return in, nil
}

func findLocation(ctx context.Context, q *sqlc.Queries, ws int64, name string) (int64, error) {
	return q.FindLocationByName(ctx, sqlc.FindLocationByNameParams{WorkspaceID: ws, Name: name})
}

// CreateLocation adds a location.
func (s *Service) CreateLocation(ctx context.Context, scope tenant.Scope, in LocationInput) (Location, error) {
	in, err := normalizeLocation(in)
	if err != nil {
		return Location{}, err
	}
	var out Location
	err = s.inTx(ctx, func(q *sqlc.Queries) error {
		ws := scope.WorkspaceID()
		if err := ensureFree(ctx, findLocation, q, ws, in.Name, 0, locationKind); err != nil {
			return err
		}
		sortKey, _ := sortFields(in.Name)
		now := s.now().UnixMilli()
		row, err := q.InsertLocation(ctx, sqlc.InsertLocationParams{WorkspaceID: ws, Name: in.Name, SortKey: sortKey, Note: in.Note, CreatedAt: now, UpdatedAt: now})
		if err != nil {
			return err
		}
		out = toLocation(row, 0)
		return s.record(ctx, q, ws, locationKind, "create", row.ID, nil, kindSummaries[locationKind].create, in.Name)
	})
	return out, err
}

// UpdateLocation renames a location or changes its note.
func (s *Service) UpdateLocation(ctx context.Context, scope tenant.Scope, id int64, in LocationInput) (Location, error) {
	in, err := normalizeLocation(in)
	if err != nil {
		return Location{}, err
	}
	err = s.inTx(ctx, func(q *sqlc.Queries) error {
		ws := scope.WorkspaceID()
		before, err := q.GetLocation(ctx, sqlc.GetLocationParams{WorkspaceID: ws, ID: id})
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		if before.Name == in.Name && before.Note == in.Note {
			return nil
		}
		if err := ensureFree(ctx, findLocation, q, ws, in.Name, id, locationKind); err != nil {
			return err
		}
		sortKey, _ := sortFields(in.Name)
		if err := q.UpdateLocation(ctx, sqlc.UpdateLocationParams{Name: in.Name, SortKey: sortKey, Note: in.Note, UpdatedAt: s.now().UnixMilli(), WorkspaceID: ws, ID: id}); err != nil {
			return err
		}
		f, a := renameSummary(locationKind, before.Name, in.Name)
		return s.record(ctx, q, ws, locationKind, "update", id, nil, f, a...)
	})
	if err != nil {
		return Location{}, err
	}
	return s.location(ctx, scope, id)
}

func (s *Service) location(ctx context.Context, scope tenant.Scope, id int64) (Location, error) {
	all, err := s.ListLocations(ctx, scope)
	if err != nil {
		return Location{}, err
	}
	for _, l := range all {
		if l.ID == id {
			return l, nil
		}
	}
	return Location{}, ErrNotFound
}

// DeleteLocation removes a location; its documents keep existing, without one.
func (s *Service) DeleteLocation(ctx context.Context, scope tenant.Scope, id int64) error {
	return s.inTx(ctx, func(q *sqlc.Queries) error {
		ws := scope.WorkspaceID()
		row, err := q.GetLocation(ctx, sqlc.GetLocationParams{WorkspaceID: ws, ID: id})
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		if err := q.DeleteLocation(ctx, sqlc.DeleteLocationParams{WorkspaceID: ws, ID: id}); err != nil {
			return err
		}
		return s.record(ctx, q, ws, locationKind, "delete", id, nil, kindSummaries[locationKind].delete, row.Name)
	})
}

// AssignLocationByASN puts the originals numbered from..to (inclusive)
// in a location, or takes them out of any with id 0: a box of originals
// is usually a run of numbers. Documents in the trash are left alone. It
// returns how many documents changed.
func (s *Service) AssignLocationByASN(ctx context.Context, scope tenant.Scope, id, from, to int64) (int, error) {
	if from < 1 || to < from || to-from >= maxASNRange {
		return 0, errASNRange
	}
	var n int64
	err := s.inTx(ctx, func(q *sqlc.Queries) error {
		ws := scope.WorkspaceID()
		format, args := i18n.N("%s 把原件编号 %d～%d 的文档清除了存放位置（%d 份）"), []any{from, to}
		loc := sql.NullInt64{}
		if id != 0 {
			row, err := q.GetLocation(ctx, sqlc.GetLocationParams{WorkspaceID: ws, ID: id})
			if errors.Is(err, sql.ErrNoRows) {
				return ErrNotFound
			}
			if err != nil {
				return err
			}
			loc = sql.NullInt64{Int64: id, Valid: true}
			format, args = i18n.N("%s 把原件编号 %d～%d 的文档放到了“%s”（%d 份）"), append(args, row.Name)
		}
		r, err := q.AssignLocationByASN(ctx, sqlc.AssignLocationByASNParams{LocationID: loc, ModifiedAt: s.now().UnixMilli(), WorkspaceID: ws, AsnFrom: sql.NullInt64{Int64: from, Valid: true}, AsnTo: sql.NullInt64{Int64: to, Valid: true}, Viewer: scope.Viewer()})
		if err != nil {
			return err
		}
		n = r
		if n == 0 {
			return nil
		}
		return s.record(ctx, q, ws, locationKind, "assign", id, nil, format, append(args, n)...)
	})
	return int(n), err
}
