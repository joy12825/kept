package api

import (
	"context"

	"kept/server/internal/api/gen"
	"kept/server/internal/auth"
)

func (s *Server) GetTwoFactor(ctx context.Context, _ gen.GetTwoFactorRequestObject) (gen.GetTwoFactorResponseObject, error) {
	status, err := s.auth.TwoFactorStatus(ctx, mustPrincipal(ctx).UserID)
	if err != nil {
		return nil, err
	}
	return gen.GetTwoFactor200JSONResponse(toAPITwoFactorStatus(status)), nil
}

func (s *Server) BeginTwoFactor(ctx context.Context, _ gen.BeginTwoFactorRequestObject) (gen.BeginTwoFactorResponseObject, error) {
	p := mustPrincipal(ctx)
	setup, err := s.auth.BeginTwoFactor(ctx, p.UserID, p.Username)
	if err != nil {
		return nil, err
	}
	return gen.BeginTwoFactor200JSONResponse{Secret: setup.Secret, OtpauthUri: setup.OtpauthURI}, nil
}

func (s *Server) EnableTwoFactor(ctx context.Context, req gen.EnableTwoFactorRequestObject) (gen.EnableTwoFactorResponseObject, error) {
	codes, err := s.auth.EnableTwoFactor(ctx, mustPrincipal(ctx).UserID, req.Body.Code)
	if err != nil {
		return nil, err
	}
	return gen.EnableTwoFactor200JSONResponse{Codes: codes}, nil
}

func (s *Server) DisableTwoFactor(ctx context.Context, req gen.DisableTwoFactorRequestObject) (gen.DisableTwoFactorResponseObject, error) {
	if err := s.auth.DisableTwoFactor(ctx, mustPrincipal(ctx), req.Body.Password); err != nil {
		return nil, err
	}
	return gen.DisableTwoFactor204Response{}, nil
}

func (s *Server) RegenerateRecoveryCodes(ctx context.Context, req gen.RegenerateRecoveryCodesRequestObject) (gen.RegenerateRecoveryCodesResponseObject, error) {
	codes, err := s.auth.RegenerateRecoveryCodes(ctx, mustPrincipal(ctx), req.Body.Password)
	if err != nil {
		return nil, err
	}
	return gen.RegenerateRecoveryCodes200JSONResponse{Codes: codes}, nil
}

// ---------- admin ----------

// AdminDisableTwoFactor turns off another account's two-factor
// authentication (for a user who lost their authenticator). An admin's own
// goes through DisableTwoFactor, which asks for the current password.
func (s *Server) AdminDisableTwoFactor(ctx context.Context, req gen.AdminDisableTwoFactorRequestObject) (gen.AdminDisableTwoFactorResponseObject, error) {
	if req.Id == mustPrincipal(ctx).UserID {
		return nil, &auth.Error{Kind: auth.KindInvalid, Message: "请在“设置 → 密码与安全”中关闭自己的两步验证"}
	}
	if err := s.auth.AdminDisableTwoFactor(ctx, req.Id); err != nil {
		return nil, err
	}
	return gen.AdminDisableTwoFactor204Response{}, nil
}

// ---------- conversions ----------

func toAPITwoFactorStatus(st auth.TwoFactorStatus) gen.TwoFactorStatus {
	return gen.TwoFactorStatus{Enabled: st.Enabled, RecoveryCodesLeft: st.RecoveryCodesLeft}
}
