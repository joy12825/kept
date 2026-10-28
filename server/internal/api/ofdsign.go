package api

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"kept/server/internal/api/gen"
	"kept/server/internal/i18n"
	"kept/server/internal/parser"
	"kept/server/internal/tenant"
)

// The signatures of an OFD (v1.11.3).

func (s *Server) GetDocumentSignatures(ctx context.Context, req gen.GetDocumentSignaturesRequestObject) (gen.GetDocumentSignaturesResponseObject, error) {
	scope := tenant.MustFrom(ctx)
	if err := s.guardSensitive(ctx, scope, req.Id); err != nil {
		return nil, err
	}
	sigs, err := s.ingest.OFDSignatures(ctx, scope, req.Id)
	if err != nil {
		return nil, err
	}
	out := gen.GetDocumentSignatures200JSONResponse{Items: make([]gen.OFDSignature, len(sigs))}
	for i, sg := range sigs {
		out.Items[i] = toOFDSignature(ctx, sg)
	}
	return out, nil
}

func toOFDSignature(ctx context.Context, sg parser.OFDSignature) gen.OFDSignature {
	opt := func(v string) *string {
		if v == "" {
			return nil
		}
		return &v
	}
	optTime := func(t time.Time) *time.Time {
		if t.IsZero() {
			return nil
		}
		return &t
	}
	o := gen.OFDSignature{Type: sg.Type, Status: gen.OFDSignatureStatus(sg.Status), OutsideValidity: sg.OutsideValidity, SelfSigned: sg.SelfSigned,
		CertInSeal: sg.CertInSeal, HasSeal: sg.HasSealPicture(), Unsigned: sg.Unsigned,
		Provider: opt(sg.Provider), SealName: opt(sg.SealName), Signer: opt(sg.Signer), Org: opt(sg.Org), Issuer: opt(sg.Issuer),
		Time: optTime(sg.Time), ValidFrom: optTime(sg.ValidFrom), ValidTo: optTime(sg.ValidTo)}
	if sg.Reason != "" {
		o.Reason = opt(i18n.T(ctx, sg.Reason))
	}
	if sg.Page > 0 {
		o.Page = &sg.Page
	}
	if sg.Version > 0 {
		o.Version = &sg.Version
	}
	if o.Unsigned == nil {
		o.Unsigned = []string{}
	}
	o.Files = make([]struct {
		Missing bool   `json:"missing"`
		Ok      bool   `json:"ok"`
		Path    string `json:"path"`
	}, len(sg.Files))
	for i, f := range sg.Files {
		o.Files[i].Missing, o.Files[i].Ok, o.Files[i].Path = f.Missing, f.OK, f.Path
	}
	return o
}

func (s *Server) GetDocumentSeal(ctx context.Context, req gen.GetDocumentSealRequestObject) (gen.GetDocumentSealResponseObject, error) {
	scope := tenant.MustFrom(ctx)
	if err := s.guardSensitive(ctx, scope, req.Id); err != nil {
		return nil, err
	}
	b, mime, err := s.ingest.OFDSeal(ctx, scope, req.Id, req.N)
	if err != nil {
		return nil, err
	}
	return sealImage{data: b, mime: mime}, nil
}

// sealImage is a seal's picture, served like documents' files.
type sealImage struct {
	data []byte
	mime string
}

func (r sealImage) VisitGetDocumentSealResponse(w http.ResponseWriter) error {
	h := w.Header()
	h.Set("Content-Type", r.mime)
	h.Set("Content-Length", strconv.Itoa(len(r.data)))
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Content-Security-Policy", "sandbox")
	h.Set("Cache-Control", "private, no-cache")
	w.WriteHeader(http.StatusOK)
	_, err := w.Write(r.data)
	return err
}
