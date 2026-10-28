// Package tenant carries the current workspace through a request.
//
// It is the only way workspace-scoped code learns which workspace it acts
// for: the HTTP auth middleware resolves the caller's workspace and stores a
// Scope in the context; store functions that touch workspace data take a
// Scope (never a bare workspace ID) so that forgetting the filter is a
// compile error rather than a data leak.
package tenant

import "context"

type Role string

const (
	RoleOwner  Role = "owner"
	RoleEditor Role = "editor"
	RoleViewer Role = "viewer"
)

// rank orders roles by what they may do; unknown roles rank nowhere.
var rank = map[Role]int{RoleViewer: 1, RoleEditor: 2, RoleOwner: 3}

// AtLeast reports whether r grants everything min does.
func (r Role) AtLeast(min Role) bool {
	have, need := rank[r], rank[min]
	return have > 0 && need > 0 && have >= need
}

// Scope identifies the workspace a request acts in, the caller's role
// there, and who is looking (v1.8.0, private documents).
type Scope struct {
	workspaceID int64
	role        Role
	viewer      int64
}

// Viewer values that are not a user.
const (
	// ViewerSystem sees every document: background work (reading files,
	// rules, the index) that shows nothing to a person by itself.
	ViewerSystem int64 = -1
	// ViewerOutside sees no private document: another workspace looking
	// in through a share, and work whose result anyone may see.
	ViewerOutside int64 = 0
)

// New builds a system Scope, which sees private documents too. Call it only
// where the membership has been verified (background jobs that load a
// job's workspace, tests); a person's request uses ForUser.
func New(workspaceID int64, role Role) Scope {
	return Scope{workspaceID: workspaceID, role: role, viewer: ViewerSystem}
}

// ForUser builds the Scope of a member's request: private documents show
// only to the people they belong to (see the privacy package).
func ForUser(workspaceID int64, role Role, userID int64) Scope {
	return Scope{workspaceID: workspaceID, role: role, viewer: userID}
}

// Outside is the same workspace seen without its private documents.
func (s Scope) Outside() Scope { s.viewer = ViewerOutside; return s }

func (s Scope) WorkspaceID() int64 { return s.workspaceID }
func (s Scope) Role() Role         { return s.role }

// Viewer is the user looking, ViewerSystem or ViewerOutside. It is passed
// to queries as the viewer argument of the visibility condition.
func (s Scope) Viewer() int64 { return s.viewer }

// User is the member looking, 0 for system and outside scopes.
func (s Scope) User() int64 {
	if s.viewer > 0 {
		return s.viewer
	}
	return 0
}

// CanWrite reports whether the role may modify workspace data.
func (s Scope) CanWrite() bool { return s.role == RoleOwner || s.role == RoleEditor }

type ctxKey struct{}

func With(ctx context.Context, s Scope) context.Context {
	return context.WithValue(ctx, ctxKey{}, s)
}

// From returns the request's workspace scope, if any.
func From(ctx context.Context) (Scope, bool) {
	s, ok := ctx.Value(ctxKey{}).(Scope)
	if !ok || s.workspaceID <= 0 {
		return Scope{}, false
	}
	return s, true
}

// MustFrom is for handlers behind the auth middleware, where a missing
// scope is a programming error.
func MustFrom(ctx context.Context) Scope {
	s, ok := From(ctx)
	if !ok {
		panic("tenant: no workspace scope in context")
	}
	return s
}
