package api

import (
	"context"

	"kept/server/internal/api/gen"
	"kept/server/internal/i18n"
	"kept/server/internal/parser"
	"kept/server/internal/tenant"
)

// Tables and zips (v1.9.3).

func (s *Server) GetDocumentSheets(ctx context.Context, req gen.GetDocumentSheetsRequestObject) (gen.GetDocumentSheetsResponseObject, error) {
	if err := s.guardSensitive(ctx, tenant.MustFrom(ctx), req.Id); err != nil {
		return nil, err
	}
	sheets, err := s.ingest.Sheets(ctx, tenant.MustFrom(ctx), req.Id)
	if err != nil {
		return nil, err
	}
	out := gen.GetDocumentSheets200JSONResponse{}
	for _, sh := range sheets {
		rows := make([][]string, len(sh.Rows))
		for i, r := range sh.Rows {
			if r == nil {
				r = []string{}
			}
			rows[i] = r
		}
		merges := make([][]int, len(sh.Merges))
		for i, m := range sh.Merges {
			merges[i] = m[:]
		}
		out.Sheets = append(out.Sheets, struct {
			Merges    [][]int    `json:"merges"`
			Name      string     `json:"name"`
			Rows      [][]string `json:"rows"`
			Truncated bool       `json:"truncated"`
		}{Merges: merges, Name: sheetName(ctx, sh.Name), Rows: rows, Truncated: sh.Truncated})
	}
	out.Sheets = nonNil(out.Sheets)
	return out, nil
}

func (s *Server) ListZipEntries(ctx context.Context, req gen.ListZipEntriesRequestObject) (gen.ListZipEntriesResponseObject, error) {
	if err := s.guardSensitive(ctx, tenant.MustFrom(ctx), req.Id); err != nil {
		return nil, err
	}
	entries, truncated, err := s.ingest.ZipEntries(ctx, tenant.MustFrom(ctx), req.Id)
	if err != nil {
		return nil, err
	}
	out := gen.ListZipEntries200JSONResponse{Truncated: truncated}
	for _, e := range entries {
		out.Entries = append(out.Entries, struct {
			Dir       bool   `json:"dir"`
			Encrypted bool   `json:"encrypted"`
			Path      string `json:"path"`
			Size      int64  `json:"size"`
		}{Dir: e.Dir, Encrypted: e.Encrypted, Path: e.Path, Size: e.Size})
	}
	out.Entries = nonNil(out.Entries)
	return out, nil
}

func (s *Server) ImportZipEntries(ctx context.Context, req gen.ImportZipEntriesRequestObject) (gen.ImportZipEntriesResponseObject, error) {
	if err := s.guardSensitive(ctx, tenant.MustFrom(ctx), req.Id); err != nil {
		return nil, err
	}
	got, err := s.ingest.ImportEntries(ctx, tenant.MustFrom(ctx), req.Id, req.Body.Paths)
	if err != nil {
		return nil, err
	}
	out := gen.ImportZipEntries200JSONResponse{}
	for _, r := range got {
		x := struct {
			JobId  *int64  `json:"jobId,omitempty"`
			Path   string  `json:"path"`
			Reason *string `json:"reason,omitempty"`
		}{Path: r.Path}
		if r.JobID != 0 {
			x.JobId = &r.JobID
		} else {
			reason := i18n.T(ctx, r.Reason)
			x.Reason = &reason
		}
		out.Results = append(out.Results, x)
	}
	out.Results = nonNil(out.Results)
	return out, nil
}

// sheetName translates the name the server gives a CSV's one table; a
// spreadsheet's own sheet names are left as they are.
func sheetName(ctx context.Context, name string) string {
	if name == parser.CSVSheetName {
		return i18n.T(ctx, name)
	}
	return name
}

// nonNil is s, or an empty list: JSON [] rather than null.
func nonNil[T any](s []T) []T {
	if s == nil {
		return []T{}
	}
	return s
}
