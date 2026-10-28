// Package privacy is private documents (v1.8.0): a document private to a
// family member shows only to the people that member lets see it.
//
// Who may see a private document:
//   - the person themself (person_access level self), their guardians
//     (guardian), and members they granted access (manage, read);
//   - the member who uploaded it (documents.added_by), until the person
//     takes that back (documents.uploader_access = 0).
//
// Everyone else in the workspace, the owner included, does not see it and
// does not count it. Every query that shows documents to a person carries
// the visibility condition: Visible for SQL written in Go, and the same
// condition written out in the sqlc queries (the viewer argument).
package privacy

import "kept/server/internal/tenant"

// Visible is the condition over the documents table aliased alias that
// keeps only what scope's viewer may see, with its arguments. System
// scopes see everything; outside scopes no private document.
func Visible(alias string, scope tenant.Scope) (string, []any) {
	return VisibleTo(alias, scope.Viewer())
}

// VisibleTo is Visible for a viewer value (tenant.ViewerSystem,
// tenant.ViewerOutside or a user).
func VisibleTo(alias string, viewer int64) (string, []any) {
	switch {
	case viewer == tenant.ViewerSystem:
		return "1 = 1", nil
	case viewer <= 0:
		return alias + ".private = 0", nil
	}
	return "(" + alias + ".private = 0" +
		" OR " + alias + ".person_id IN (SELECT pa.person_id FROM person_access AS pa WHERE pa.user_id = ?)" +
		" OR (" + alias + ".added_by = ? AND " + alias + ".uploader_access = 1))", []any{viewer, viewer}
}

// Editable is the condition that keeps the documents scope's viewer may
// change: those that are not private, and private ones of persons the
// viewer is, looks after or manages. Uploaders and read-only grants only
// look. It does not check the viewer's role.
func Editable(alias string, scope tenant.Scope) (string, []any) {
	switch viewer := scope.Viewer(); {
	case viewer == tenant.ViewerSystem:
		return "1 = 1", nil
	case viewer <= 0:
		return alias + ".private = 0", nil
	default:
		return "(" + alias + ".private = 0 OR " + alias + ".person_id IN (SELECT pa.person_id FROM person_access AS pa WHERE pa.user_id = ?" +
			" AND pa.level IN ('self', 'guardian', 'manage')))", []any{viewer}
	}
}
