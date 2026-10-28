package api

import (
	"context"
	"errors"
	"fmt"
	"image"
	_ "image/gif"  // DecodeConfig
	_ "image/jpeg" // DecodeConfig
	_ "image/png"  // DecodeConfig
	"kept/server/internal/files"
	"os"

	openapi_types "github.com/oapi-codegen/runtime/types"

	"kept/server/internal/api/gen"
	"kept/server/internal/exif"
	"kept/server/internal/i18n"
	"kept/server/internal/parser"
	"kept/server/internal/tenant"
)

// GetDocumentFileInfo reads what the original file carries (v1.3.7): a
// PDF's properties or a photo's EXIF. Nothing is stored; a file that
// cannot be read just has no properties.
func (s *Server) GetDocumentFileInfo(ctx context.Context, req gen.GetDocumentFileInfoRequestObject) (gen.GetDocumentFileInfoResponseObject, error) {
	d, err := s.documents.Get(ctx, tenant.MustFrom(ctx), req.Id)
	if err != nil {
		return nil, err
	}
	out := gen.GetDocumentFileInfo200JSONResponse{MimeType: d.MimeType, Props: []gen.FileProp{}}
	if d.ArchiveChecksum != "" {
		out.ArchiveChecksum = &d.ArchiveChecksum
	}
	original, release, err := s.files.Local(ctx, d.Storage, files.Original(d.WorkspaceID, d.ID, d.OriginalExt), "")
	if err != nil {
		return nil, err
	}
	defer release()
	add := func(label, value string) {
		out.Props = append(out.Props, gen.FileProp{Label: i18n.T(ctx, label), Value: value})
	}
	switch d.MimeType {
	case "application/pdf":
		props, err := parser.PDFProps(ctx, original)
		if errors.Is(err, parser.ErrEncrypted) && d.HasArchive {
			// the searchable version is the decrypted copy
			if archive, release, aerr := s.files.Local(ctx, d.Storage, files.Archive(d.WorkspaceID, d.ID), ""); aerr == nil {
				props, err = parser.PDFProps(ctx, archive)
				release()
			}
			props = append(props, parser.Prop{Label: i18n.N("加密"), Value: i18n.N("是（原件需要密码才能打开）")})
		}
		if err == nil {
			for _, p := range props {
				add(p.Label, propValue(ctx, p.Label, p.Value))
			}
		}
	case "image/jpeg", "image/tiff", "image/png", "image/gif", "image/webp":
		x := exif.ReadFile(original)
		if !x.Taken.IsZero() {
			add(i18n.N("拍摄时间"), x.Taken.Format("2006-01-02 15:04"))
			day := openapi_types.Date{Time: x.Taken}
			out.TakenOn = &day
		}
		if cam := joinNonEmpty(x.Make, x.Model); cam != "" {
			add(i18n.N("相机"), cam)
		}
		if x.Software != "" {
			add(i18n.N("软件"), x.Software)
		}
		if f, err := os.Open(original); err == nil {
			if c, _, err := image.DecodeConfig(f); err == nil {
				add(i18n.N("尺寸"), i18n.T(ctx, fmt.Sprintf(i18n.N("%d × %d 像素"), c.Width, c.Height)))
			}
			f.Close()
		}
		if x.HasLocation {
			add(i18n.N("位置"), i18n.T(ctx, i18n.N("照片带有拍摄地点（这里不显示）")))
		}
		out.HasLocation = x.HasLocation
	}
	return out, nil
}

// propValue translates a PDF property's value when the server wrote it
// (encrypted, page size); what the file carries (title, author) is left
// as it is.
func propValue(ctx context.Context, label, value string) string {
	switch label {
	case "加密", "页面大小":
		return i18n.T(ctx, value)
	}
	return value
}

func joinNonEmpty(a, b string) string {
	switch {
	case a == "":
		return b
	case b == "":
		return a
	}
	return a + " " + b
}
