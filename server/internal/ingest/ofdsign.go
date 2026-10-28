package ingest

import (
	"context"
	"errors"

	"kept/server/internal/apperr"
	"kept/server/internal/parser"
	"kept/server/internal/tenant"
)

// The signatures of an OFD (v1.11.3), checked from the current original
// each time they are asked for.

var (
	errNotOFD      = apperr.WithCode(apperr.KindInvalid, "not_ofd", "只有 OFD 文件有电子签章")
	errOFDBroken   = apperr.WithCode(apperr.KindInvalid, "ofd_unreadable", "这份 OFD 读不出来，可能已损坏")
	errNoSealImage = apperr.New(apperr.KindNotFound, "这个签章没有能显示的印章图片")
)

func isOFD(mime string) bool { return mime == "application/ofd" }

// OFDSignatures checks the signatures of an OFD document.
func (s *Service) OFDSignatures(ctx context.Context, scope tenant.Scope, docID int64) ([]parser.OFDSignature, error) {
	var out []parser.OFDSignature
	err := s.withFile(ctx, scope, docID, isOFD, errNotOFD, func(src string) error {
		sigs, err := parser.ReadOFDSignatures(src)
		if err != nil {
			return errOFDBroken
		}
		out = sigs
		return nil
	})
	return out, err
}

// OFDSeal returns the n-th signature's seal picture (from 1) and its type.
func (s *Service) OFDSeal(ctx context.Context, scope tenant.Scope, docID int64, n int) ([]byte, string, error) {
	var data []byte
	var mime string
	err := s.withFile(ctx, scope, docID, isOFD, errNotOFD, func(src string) error {
		b, m, err := parser.OFDSeal(src, n)
		if errors.Is(err, parser.ErrOFD) {
			return errNoSealImage
		}
		data, mime = b, m
		return err
	})
	return data, mime, err
}
