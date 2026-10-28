package api

import (
	"context"
	"net/http"
	"strconv"

	"kept/server/internal/api/gen"
	"kept/server/internal/i18n"
	"kept/server/internal/parser"
	"kept/server/internal/tenant"
)

// The Word reading view (v1.9.8).

func (s *Server) GetDocumentWord(ctx context.Context, req gen.GetDocumentWordRequestObject) (gen.GetDocumentWordResponseObject, error) {
	if err := s.guardSensitive(ctx, tenant.MustFrom(ctx), req.Id); err != nil {
		return nil, err
	}
	d, err := s.ingest.Word(ctx, tenant.MustFrom(ctx), req.Id)
	if err != nil {
		return nil, err
	}
	out := gen.GetDocumentWord200JSONResponse{BaseSize: float32(d.BaseSize), Truncated: d.Truncated, Blocks: wordBlocks(ctx, d.Blocks)}
	for _, n := range d.Notes {
		out.Notes = append(out.Notes, gen.WordNote{N: n.N, Blocks: wordBlocks(ctx, n.Blocks)})
	}
	for _, im := range d.Images {
		out.Images = append(out.Images, gen.WordImage{Width: im.Width, Height: im.Height})
	}
	out.Notes, out.Images = nonNil(out.Notes), nonNil(out.Images)
	return out, nil
}

func wordBlocks(ctx context.Context, in []parser.WordBlock) []gen.WordBlock {
	out := make([]gen.WordBlock, 0, len(in))
	for _, b := range in {
		g := gen.WordBlock{Kind: gen.WordBlockKind(b.Kind)}
		if b.Heading > 0 {
			g.Heading = &b.Heading
		}
		if b.Align != "" {
			a := gen.WordBlockAlign(b.Align)
			g.Align = &a
		}
		if b.List != nil {
			g.List = &gen.WordList{Marker: b.List.Marker, Level: b.List.Level}
		}
		switch b.Kind {
		case "paragraph":
			runs := make([]gen.WordRun, len(b.Runs))
			for i, r := range b.Runs {
				runs[i] = wordRun(ctx, r)
			}
			g.Runs = &runs
		case "table":
			rows := make([][]gen.WordCell, len(b.Rows))
			for i, row := range b.Rows {
				rows[i] = make([]gen.WordCell, len(row))
				for j, c := range row {
					rows[i][j] = gen.WordCell{Blocks: wordBlocks(ctx, c.Blocks), ColSpan: c.ColSpan, RowSpan: c.RowSpan}
				}
			}
			g.Rows = &rows
			if b.HeaderRows > 0 {
				g.HeaderRows = &b.HeaderRows
			}
		case "box":
			blocks := wordBlocks(ctx, b.Blocks)
			g.Blocks = &blocks
		}
		out = append(out, g)
	}
	return out
}

func wordRun(ctx context.Context, r parser.WordRun) gen.WordRun {
	var g gen.WordRun
	str := func(v string) *string {
		if v == "" {
			return nil
		}
		return &v
	}
	flag := func(v bool) *bool {
		if !v {
			return nil
		}
		return &v
	}
	num := func(v int) *int {
		if v == 0 {
			return nil
		}
		return &v
	}
	if parser.IsWordPlaceholder(r.Text) { // a drawing the reader cannot show
		r.Text = i18n.T(ctx, r.Text)
	}
	g.Text, g.Color, g.Highlight, g.Link = str(r.Text), str(r.Color), str(r.Highlight), str(r.Link)
	g.Bold, g.Italic, g.Underline, g.Strike = flag(r.Bold), flag(r.Italic), flag(r.Underline), flag(r.Strike)
	g.Image, g.Note = num(r.Image), num(r.Note)
	if r.Size > 0 {
		v := float32(r.Size)
		g.Size = &v
	}
	if r.Vert != "" {
		v := gen.WordRunVert(r.Vert)
		g.Vert = &v
	}
	return g
}

func (s *Server) GetDocumentWordImage(ctx context.Context, req gen.GetDocumentWordImageRequestObject) (gen.GetDocumentWordImageResponseObject, error) {
	if err := s.guardSensitive(ctx, tenant.MustFrom(ctx), req.Id); err != nil {
		return nil, err
	}
	b, mime, err := s.ingest.WordImage(ctx, tenant.MustFrom(ctx), req.Id, req.N)
	if err != nil {
		return nil, err
	}
	return wordImage{data: b, mime: mime}, nil
}

// wordImage is a picture from inside a document, served like its files: never
// run as a page of this origin.
type wordImage struct {
	data []byte
	mime string
}

func (r wordImage) VisitGetDocumentWordImageResponse(w http.ResponseWriter) error {
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
