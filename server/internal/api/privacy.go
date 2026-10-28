package api

import (
	"context"

	"kept/server/internal/api/gen"
	"kept/server/internal/privacy"
	"kept/server/internal/taxonomy"
	"kept/server/internal/tenant"
)

// Private documents (v1.8.0): who may see a family member's private
// documents, set on the persons page and in “我的私密”.

func toAPIPersonPrivacy(p privacy.Person) gen.PersonPrivacy {
	out := gen.PersonPrivacy{PersonId: p.ID, Holder: p.Holder, Claimable: p.Claimable, NeedsReview: p.NeedsReview}
	// how many private documents a person has is for those who see them (v1.9.10)
	if p.MyLevel != "" {
		out.PrivateCount = int(p.Private)
	}
	if p.CloudAI {
		out.CloudAi = &p.CloudAI
	}
	if k := p.Key; k != nil {
		out.Key = &gen.PersonKey{CreatedAt: k.CreatedAt, RecoveryAt: k.RecoveryAt, MyPassword: k.MyPassword}
	}
	if p.Self != nil {
		a := toAPIPersonAccess(*p.Self)
		out.Self = &a
	}
	if p.MyLevel != "" {
		l := gen.PersonPrivacyMyLevel(p.MyLevel)
		out.MyLevel = &l
	}
	if p.Access != nil {
		access := make([]gen.PersonAccess, len(p.Access))
		for i, a := range p.Access {
			access[i] = toAPIPersonAccess(a)
		}
		out.Access = &access
	}
	if p.Emergency != nil {
		em := make([]gen.PersonEmergency, len(p.Emergency))
		for i, e := range p.Emergency {
			em[i] = toAPIPersonEmergency(e)
		}
		out.Emergency = &em
	}
	return out
}

func toAPIPersonAccess(a privacy.Access) gen.PersonAccess {
	return gen.PersonAccess{UserId: a.UserID, DisplayName: a.DisplayName, Username: a.Username, Level: gen.PersonAccessLevel(a.Level), Via: a.Via, CreatedAt: a.CreatedAt}
}

func toAPIPersonEmergency(e privacy.Emergency) gen.PersonEmergency {
	out := gen.PersonEmergency{UserId: e.UserID, DisplayName: e.DisplayName, WaitHours: e.WaitHours, Status: gen.PersonEmergencyStatus(e.Status),
		RequestedAt: e.RequestedAt, GrantAt: e.GrantAt, GrantedAt: e.GrantedAt}
	if e.Reason != "" {
		out.Reason = &e.Reason
	}
	return out
}

// personPrivacy is one person's privacy after a change.
func (s *Server) personPrivacy(ctx context.Context, id int64) (gen.PersonPrivacy, error) {
	people, err := s.privacy.People(ctx, tenant.MustFrom(ctx))
	if err != nil {
		return gen.PersonPrivacy{}, err
	}
	p, ok := people[id]
	if !ok {
		return gen.PersonPrivacy{}, privacy.ErrPersonNotFound
	}
	return toAPIPersonPrivacy(p), nil
}

func (s *Server) RemovePersonAccess(ctx context.Context, req gen.RemovePersonAccessRequestObject) (gen.RemovePersonAccessResponseObject, error) {
	if err := s.privacy.RemoveAccess(ctx, tenant.MustFrom(ctx), req.Id, req.UserId); err != nil {
		return nil, err
	}
	p, err := s.personPrivacy(ctx, req.Id)
	return gen.RemovePersonAccess200JSONResponse(p), err
}

func (s *Server) ReviewPersonAccess(ctx context.Context, req gen.ReviewPersonAccessRequestObject) (gen.ReviewPersonAccessResponseObject, error) {
	if err := s.privacy.Reviewed(ctx, tenant.MustFrom(ctx), req.Id); err != nil {
		return nil, err
	}
	return gen.ReviewPersonAccess204Response{}, nil
}

func (s *Server) RevokePersonUploader(ctx context.Context, req gen.RevokePersonUploaderRequestObject) (gen.RevokePersonUploaderResponseObject, error) {
	n, err := s.privacy.RevokeUploader(ctx, tenant.MustFrom(ctx), req.Id, req.UserId)
	if err != nil {
		return nil, err
	}
	return gen.RevokePersonUploader200JSONResponse{Revoked: int(n)}, nil
}

func (s *Server) RemovePersonEmergency(ctx context.Context, req gen.RemovePersonEmergencyRequestObject) (gen.RemovePersonEmergencyResponseObject, error) {
	if err := s.privacy.RemoveEmergency(ctx, tenant.MustFrom(ctx), req.Id, req.UserId); err != nil {
		return nil, err
	}
	p, err := s.personPrivacy(ctx, req.Id)
	return gen.RemovePersonEmergency200JSONResponse(p), err
}

func (s *Server) DenyPersonEmergency(ctx context.Context, req gen.DenyPersonEmergencyRequestObject) (gen.DenyPersonEmergencyResponseObject, error) {
	if err := s.privacy.DenyEmergency(ctx, tenant.MustFrom(ctx), req.Id, req.UserId); err != nil {
		return nil, err
	}
	p, err := s.personPrivacy(ctx, req.Id)
	return gen.DenyPersonEmergency200JSONResponse(p), err
}

func (s *Server) RequestPersonEmergency(ctx context.Context, req gen.RequestPersonEmergencyRequestObject) (gen.RequestPersonEmergencyResponseObject, error) {
	reason := ""
	if req.Body != nil && req.Body.Reason != nil {
		reason = *req.Body.Reason
	}
	if err := s.privacy.RequestEmergency(ctx, tenant.MustFrom(ctx), req.Id, reason); err != nil {
		return nil, err
	}
	p, err := s.personPrivacy(ctx, req.Id)
	return gen.RequestPersonEmergency200JSONResponse(p), err
}

func (s *Server) CancelPersonEmergency(ctx context.Context, req gen.CancelPersonEmergencyRequestObject) (gen.CancelPersonEmergencyResponseObject, error) {
	if err := s.privacy.CancelEmergency(ctx, tenant.MustFrom(ctx), req.Id); err != nil {
		return nil, err
	}
	p, err := s.personPrivacy(ctx, req.Id)
	return gen.CancelPersonEmergency200JSONResponse(p), err
}

func (s *Server) GetMyPrivacy(ctx context.Context, _ gen.GetMyPrivacyRequestObject) (gen.GetMyPrivacyResponseObject, error) {
	scope := tenant.MustFrom(ctx)
	people, err := s.privacy.People(ctx, scope)
	if err != nil {
		return nil, err
	}
	items, err := s.taxonomy.List(ctx, scope, taxonomy.Person)
	if err != nil {
		return nil, err
	}
	out := gen.GetMyPrivacy200JSONResponse{}
	out.Persons = []struct {
		Id      int64             `json:"id"`
		Name    string            `json:"name"`
		Privacy gen.PersonPrivacy `json:"privacy"`
	}{}
	out.EmergencyFor = []struct {
		Emergency gen.PersonEmergency `json:"emergency"`
		Name      string              `json:"name"`
		PersonId  int64               `json:"personId"`
	}{}
	out.Uploaders = []struct {
		DisplayName string `json:"displayName"`
		Documents   int    `json:"documents"`
		PersonId    int64  `json:"personId"`
		UserId      int64  `json:"userId"`
	}{}
	me := scope.User()
	for _, it := range items { // in pinyin order
		p := people[it.ID]
		if p.MyLevel == privacy.LevelSelf {
			id := it.ID
			out.Self = &id
		}
		if p.MyLevel != "" {
			out.Persons = append(out.Persons, struct {
				Id      int64             `json:"id"`
				Name    string            `json:"name"`
				Privacy gen.PersonPrivacy `json:"privacy"`
			}{it.ID, it.Name, toAPIPersonPrivacy(p)})
		}
		for _, e := range p.Emergency {
			if e.UserID == me {
				out.EmergencyFor = append(out.EmergencyFor, struct {
					Emergency gen.PersonEmergency `json:"emergency"`
					Name      string              `json:"name"`
					PersonId  int64               `json:"personId"`
				}{toAPIPersonEmergency(e), it.Name, it.ID})
			}
		}
	}
	ups, err := s.privacy.Uploaders(ctx, scope)
	if err != nil {
		return nil, err
	}
	for _, u := range ups {
		out.Uploaders = append(out.Uploaders, struct {
			DisplayName string `json:"displayName"`
			Documents   int    `json:"documents"`
			PersonId    int64  `json:"personId"`
			UserId      int64  `json:"userId"`
		}{u.DisplayName, int(u.Documents), u.PersonID, u.UserID})
	}
	return out, nil
}

// ---- keys and cloud AI (v1.8.1) ----

func (s *Server) NewPersonRecoveryCode(ctx context.Context, req gen.NewPersonRecoveryCodeRequestObject) (gen.NewPersonRecoveryCodeResponseObject, error) {
	password, code := "", ""
	if req.Body != nil {
		password, code = deref(req.Body.Password), deref(req.Body.RecoveryCode)
	}
	if password != "" {
		if _, err := s.auth.CheckPassword(ctx, mustPrincipal(ctx), password); err != nil {
			return nil, err
		}
	}
	fresh, err := s.privacy.NewRecoveryCode(ctx, tenant.MustFrom(ctx), req.Id, password, code)
	if err != nil {
		return nil, err
	}
	return gen.NewPersonRecoveryCode200JSONResponse{RecoveryCode: fresh}, nil
}

func (s *Server) CheckPersonRecoveryCode(ctx context.Context, req gen.CheckPersonRecoveryCodeRequestObject) (gen.CheckPersonRecoveryCodeResponseObject, error) {
	ok, err := s.privacy.CheckRecoveryCode(ctx, tenant.MustFrom(ctx), req.Id, req.Body.RecoveryCode)
	if err != nil {
		return nil, err
	}
	return gen.CheckPersonRecoveryCode200JSONResponse{Ok: ok}, nil
}
