//go:build !ee

package api

import (
	"context"

	"kept/server/internal/api/gen"
	"kept/server/internal/drawer"
	"kept/server/internal/store/sqlc"
)

// The open-source build has no paid modules (v1.13): these hooks say so.

func eeTaskSummary(sqlc.ListWorkspaceJobsRow) (string, bool) { return "", false }

func (s *Server) mailCanSaveBodies() bool { return false }

func (s *Server) hasLab(context.Context, int64) bool { return false }

func (s *Server) stewardOn(context.Context, int64) bool { return false }

func (s *Server) qaAvailable(context.Context, int64) bool { return false }

func (s *Server) withNarrative(context.Context, int, drawer.Yearbook, *gen.Yearbook) error {
	return nil
}
