package demo

import (
	"bytes"
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"time"

	"kept/server/internal/ingest"
	"kept/server/internal/store/sqlc"
	"kept/server/internal/tenant"
)

// The picture watermark (v1.11.2): a photo of an ID card (a made-up one,
// shapes only) shared with the bank with a watermark, the photo staying a
// photo.

const idCardFile = "身份证复印件（示例）.jpg"

// idCard draws a card: a light frame, a portrait, lines where the text is.
func idCard() []byte {
	const w, h = 856, 540
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	fill := func(x0, y0, x1, y1 int, c color.RGBA) {
		for y := y0; y < y1; y++ {
			for x := x0; x < x1; x++ {
				img.Set(x, y, c)
			}
		}
	}
	fill(0, 0, w, h, color.RGBA{236, 233, 226, 255})         // the table
	fill(28, 24, w-28, h-24, color.RGBA{214, 230, 238, 255}) // the card
	fill(w-260, 70, w-70, 330, color.RGBA{180, 190, 200, 255})
	fill(w-215, 110, w-115, 210, color.RGBA{120, 130, 145, 255}) // a head
	fill(w-235, 215, w-95, 330, color.RGBA{95, 105, 120, 255})   // shoulders
	for i, n := range []int{6, 4, 9, 14, 18} {                   // lines of text
		fill(80, 80+i*52, 80+n*24, 102+i*52, color.RGBA{70, 80, 95, 255})
	}
	fill(80, 420, 80+18*30, 446, color.RGBA{60, 70, 85, 255}) // the number
	var b bytes.Buffer
	_ = jpeg.Encode(&b, img, &jpeg.Options{Quality: 88})
	return b.Bytes()
}

// demoIDCard adds the photo and its watermarked share; nothing when there.
func demoIDCard(ctx context.Context, d Deps, scope tenant.Scope, userID int64) (int, error) {
	ws := scope.WorkspaceID()
	var id int64
	err := d.DB.QueryRowContext(ctx, `SELECT id FROM documents WHERE workspace_id = ? AND original_filename = ?`, ws, idCardFile).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		if id, err = d.Ingest.Import(ctx, scope, idCardFile, bytes.NewReader(idCard()), ingest.Overrides{Title: "身份证复印件（示例）"}, ingest.Upload{}); err != nil {
			return 0, err
		}
	} else if err != nil {
		return 0, err
	}
	var n int
	if err := d.DB.QueryRowContext(ctx, `SELECT count(*) FROM share_links WHERE workspace_id = ? AND document_id = ? AND watermark <> ''`, ws, id).Scan(&n); err != nil || n > 0 {
		return 0, err
	}
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		return 0, err
	}
	now := time.Now()
	_, err = sqlc.New(d.DB).InsertShareLink(ctx, sqlc.InsertShareLinkParams{WorkspaceID: ws, DocumentID: id, Token: base64.RawURLEncoding.EncodeToString(b),
		File: "original", ExpiresAt: now.AddDate(0, 0, 14).UnixMilli(), CreatedBy: sql.NullInt64{Int64: userID, Valid: true}, CreatedAt: now.UnixMilli(),
		Watermark: "仅供招商银行办理房贷使用", AllowDownload: 1})
	if err != nil {
		return 0, err
	}
	return 1, nil
}
