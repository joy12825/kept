package api

// The edition and its license (v1.12): what this instance may do, shown to
// everyone; the license itself installed and removed by administrators.

import (
	"context"
	"time"

	openapi_types "github.com/oapi-codegen/runtime/types"

	"kept/server/internal/api/gen"
	"kept/server/internal/apperr"
	"kept/server/internal/edition"
	"kept/server/internal/i18n"
)

// editionAllows reports whether the current edition includes need; with no
// edition service (tests) everything is allowed.
func (s *Server) editionAllows(need edition.Edition) bool {
	return s.edition == nil || s.edition.Allows(need)
}

// requireEdition is nil when need is within the current edition.
func (s *Server) requireEdition(need edition.Edition) error {
	if s.editionAllows(need) {
		return nil
	}
	return edition.Required(need)
}

// errNotInBuild answers the paid modules' operations in the open-source
// build (v1.13), which does not have them.
var errNotInBuild = apperr.WithCode(apperr.KindForbidden, "edition_required", i18n.N("这是开源的免费版，不包含收费功能"))

// canAddMember and canAddSharedWorkspace check the edition's limits; one
// more is refused, nobody already there is affected.
func (s *Server) canAddMember(ctx context.Context) error {
	if s.edition == nil {
		return nil
	}
	return s.edition.CanAddMember(ctx)
}

func (s *Server) canAddSharedWorkspace(ctx context.Context) error {
	if s.edition == nil {
		return nil
	}
	return s.edition.CanAddSharedWorkspace(ctx)
}

// errExternalStorage: S3 and WebDAV are the paid edition's; a directory on
// the server is in every edition, and moving documents back to one always works.
var errExternalStorage = apperr.WithCode(apperr.KindForbidden, "edition_required", i18n.N("把外部存储（S3、WebDAV）用作文档存储是收费版的功能；服务器上的目录在免费版里也可以用"))

func (s *Server) requireStorageKind(kind string) error {
	if kind == "dir" || s.editionAllows(edition.Pro) {
		return nil
	}
	return errExternalStorage.With(map[string]any{"edition": edition.Pro.String()})
}

// requireStorageTarget checks the storage documents would move to, by name.
func (s *Server) requireStorageTarget(ctx context.Context, name string) error {
	if s.editionAllows(edition.Pro) {
		return nil
	}
	choices, err := s.storage.Choices(ctx)
	if err != nil {
		return err
	}
	for _, c := range choices {
		if c.Name == name {
			return s.requireStorageKind(c.Kind)
		}
	}
	return nil
}

func (s *Server) GetEdition(ctx context.Context, _ gen.GetEditionRequestObject) (gen.GetEditionResponseObject, error) {
	out, err := s.editionStatus(ctx)
	if err != nil {
		return nil, err
	}
	return gen.GetEdition200JSONResponse(out), nil
}

func (s *Server) AdminInstallLicense(ctx context.Context, req gen.AdminInstallLicenseRequestObject) (gen.AdminInstallLicenseResponseObject, error) {
	if _, err := s.edition.Install(ctx, req.Body.License); err != nil {
		return nil, err
	}
	out, err := s.editionStatus(ctx)
	if err != nil {
		return nil, err
	}
	return gen.AdminInstallLicense200JSONResponse(out), nil
}

func (s *Server) AdminRemoveLicense(ctx context.Context, _ gen.AdminRemoveLicenseRequestObject) (gen.AdminRemoveLicenseResponseObject, error) {
	if err := s.edition.Remove(ctx); err != nil {
		return nil, err
	}
	out, err := s.editionStatus(ctx)
	if err != nil {
		return nil, err
	}
	return gen.AdminRemoveLicense200JSONResponse(out), nil
}

// editionStatus is the edition as the caller may see it: the instance ID
// and the license's details only for administrators.
func (s *Server) editionStatus(ctx context.Context) (gen.EditionStatus, error) {
	if s.edition == nil {
		out := gen.EditionStatus{Edition: gen.EditionStatusEditionTeam, DaysLeft: -1, Features: []gen.EditionStatusFeatures{}}
		for _, f := range edition.Features(edition.Team) { // everything is allowed, so say so (App 4.10 trusts the list)
			out.Features = append(out.Features, gen.EditionStatusFeatures(f))
		}
		return out, nil
	}
	st, err := s.edition.Status(ctx)
	if err != nil {
		return gen.EditionStatus{}, err
	}
	out := gen.EditionStatus{
		Edition: gen.EditionStatusEdition(st.Edition.String()), Trial: st.Trial, DaysLeft: st.DaysLeft, Warn: st.Warn, Expired: st.Expired,
		Members:          gen.EditionUsage{Used: st.Members.Used, Limit: st.Members.Limit},
		SharedWorkspaces: gen.EditionUsage{Used: st.SharedWorkspaces.Used, Limit: st.SharedWorkspaces.Limit},
		Features:         []gen.EditionStatusFeatures{},
	}
	for _, f := range st.Features {
		out.Features = append(out.Features, gen.EditionStatusFeatures(f))
	}
	if !st.TrialEndsAt.IsZero() {
		out.TrialEndsAt = &st.TrialEndsAt
	}
	if st.OpenSource {
		out.OpenSource = &st.OpenSource
	}
	if st.Overridden {
		out.Overridden = &st.Overridden
	}
	if p, ok := principalFrom(ctx); ok && p.IsAdmin {
		out.Instance = &st.Instance
		if l := st.License; l != nil {
			info := gen.LicenseInfo{Id: l.ID, Edition: gen.LicenseInfoEdition(l.Edition), Holder: l.Holder, Issued: day(l.Issued)}
			if l.Members > 0 {
				info.Members = &l.Members
			}
			if l.Expires != "" {
				d := day(l.Expires)
				info.Expires = &d
			}
			out.License = &info
		}
	}
	return out, nil
}

func day(s string) openapi_types.Date {
	t, _ := time.Parse("2006-01-02", s)
	return openapi_types.Date{Time: t}
}
