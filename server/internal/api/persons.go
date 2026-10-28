package api

import (
	"context"

	"kept/server/internal/api/gen"
	"kept/server/internal/taxonomy"
	"kept/server/internal/tenant"
)

// Family members (v1.6.3) work like correspondents; locations say where a
// paper original is kept.

func (s *Server) ListPersons(ctx context.Context, _ gen.ListPersonsRequestObject) (gen.ListPersonsResponseObject, error) {
	out, err := s.listItems(ctx, taxonomy.Person)
	return gen.ListPersons200JSONResponse(out), err
}

func (s *Server) CreatePerson(ctx context.Context, req gen.CreatePersonRequestObject) (gen.CreatePersonResponseObject, error) {
	it, err := s.createItem(ctx, taxonomy.Person, *req.Body)
	if err != nil {
		return nil, err
	}
	return gen.CreatePerson201JSONResponse(it), nil
}

func (s *Server) UpdatePerson(ctx context.Context, req gen.UpdatePersonRequestObject) (gen.UpdatePersonResponseObject, error) {
	it, err := s.updateItem(ctx, taxonomy.Person, req.Id, *req.Body)
	if err != nil {
		return nil, err
	}
	return gen.UpdatePerson200JSONResponse(it), nil
}

func (s *Server) DeletePerson(ctx context.Context, req gen.DeletePersonRequestObject) (gen.DeletePersonResponseObject, error) {
	if err := s.taxonomy.Delete(ctx, tenant.MustFrom(ctx), taxonomy.Person, req.Id); err != nil {
		return nil, err
	}
	return gen.DeletePerson204Response{}, nil
}

func toAPILocation(l taxonomy.Location) gen.Location {
	return gen.Location{Id: l.ID, Name: l.Name, Initial: l.Initial, Note: l.Note, DocumentCount: l.Documents}
}

func locationInput(b gen.LocationInput) taxonomy.LocationInput {
	in := taxonomy.LocationInput{Name: b.Name}
	if b.Note != nil {
		in.Note = *b.Note
	}
	return in
}

func (s *Server) ListLocations(ctx context.Context, _ gen.ListLocationsRequestObject) (gen.ListLocationsResponseObject, error) {
	ls, err := s.taxonomy.ListLocations(ctx, tenant.MustFrom(ctx))
	if err != nil {
		return nil, err
	}
	out := make(gen.ListLocations200JSONResponse, len(ls))
	for i, l := range ls {
		out[i] = toAPILocation(l)
	}
	return out, nil
}

func (s *Server) CreateLocation(ctx context.Context, req gen.CreateLocationRequestObject) (gen.CreateLocationResponseObject, error) {
	l, err := s.taxonomy.CreateLocation(ctx, tenant.MustFrom(ctx), locationInput(*req.Body))
	if err != nil {
		return nil, err
	}
	return gen.CreateLocation201JSONResponse(toAPILocation(l)), nil
}

func (s *Server) UpdateLocation(ctx context.Context, req gen.UpdateLocationRequestObject) (gen.UpdateLocationResponseObject, error) {
	l, err := s.taxonomy.UpdateLocation(ctx, tenant.MustFrom(ctx), req.Id, locationInput(*req.Body))
	if err != nil {
		return nil, err
	}
	return gen.UpdateLocation200JSONResponse(toAPILocation(l)), nil
}

func (s *Server) DeleteLocation(ctx context.Context, req gen.DeleteLocationRequestObject) (gen.DeleteLocationResponseObject, error) {
	if err := s.taxonomy.DeleteLocation(ctx, tenant.MustFrom(ctx), req.Id); err != nil {
		return nil, err
	}
	return gen.DeleteLocation204Response{}, nil
}

func (s *Server) AssignLocationByAsn(ctx context.Context, req gen.AssignLocationByAsnRequestObject) (gen.AssignLocationByAsnResponseObject, error) {
	n, err := s.taxonomy.AssignLocationByASN(ctx, tenant.MustFrom(ctx), req.Body.LocationId, req.Body.From, req.Body.To)
	if err != nil {
		return nil, err
	}
	return gen.AssignLocationByAsn200JSONResponse{Updated: n}, nil
}
