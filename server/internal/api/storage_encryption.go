package api

import (
	"context"

	"kept/server/internal/api/gen"
	"kept/server/internal/i18n"
	"kept/server/internal/storage"
)

// Encrypted storages (v1.11.0), for the instance administrator. Recovery
// codes are returned once, when made, and never again.

func toStorageEncryption(ctx context.Context, e *storage.Encryption) *gen.StorageEncryption {
	if e == nil {
		return nil
	}
	out := &gen.StorageEncryption{Enabled: e.Enabled, Locked: e.Locked, Converting: e.Converting}
	if e.LastError != "" {
		out.Error = strPtr(i18n.T(ctx, e.LastError))
	}
	if p := e.Progress; p != nil {
		out.Progress = &struct {
			Converted int `json:"converted"`
			Failed    int `json:"failed"`
			Skipped   int `json:"skipped"`
			Total     int `json:"total"`
		}{Converted: p.Converted, Failed: p.Failed, Skipped: p.Skipped, Total: p.Total}
	}
	return out
}

func (s *Server) AdminGetEncryption(ctx context.Context, _ gen.AdminGetEncryptionRequestObject) (gen.AdminGetEncryptionResponseObject, error) {
	st, err := s.storage.EncryptionStatus(ctx)
	if err != nil {
		return nil, err
	}
	out := gen.AdminGetEncryption200JSONResponse{Recovery: st.Recovery, Locked: st.Locked}
	if st.Recovery {
		at := st.RecoveryAt
		out.RecoveryAt = &at
	}
	return out, nil
}

func (s *Server) AdminNewRecoveryCode(ctx context.Context, _ gen.AdminNewRecoveryCodeRequestObject) (gen.AdminNewRecoveryCodeResponseObject, error) {
	code, err := s.storage.NewRecoveryCode(ctx)
	if err != nil {
		return nil, err
	}
	return gen.AdminNewRecoveryCode200JSONResponse{RecoveryCode: code}, nil
}

func (s *Server) AdminRecoverEncryption(ctx context.Context, req gen.AdminRecoverEncryptionRequestObject) (gen.AdminRecoverEncryptionResponseObject, error) {
	n, err := s.storage.Recover(ctx, req.Body.Code)
	if err != nil {
		return nil, err
	}
	return gen.AdminRecoverEncryption200JSONResponse{Recovered: n}, nil
}
