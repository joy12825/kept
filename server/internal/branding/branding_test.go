package branding

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"kept/server/internal/apperr"
	"kept/server/internal/audit"
	"kept/server/internal/store"
)

func newService(t *testing.T) (*Service, *store.Store, string) {
	t.Helper()
	st, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	dir := filepath.Join(t.TempDir(), "branding")
	n := int64(0)
	return NewService(st.DB, dir, Options{Now: func() time.Time { n++; return time.UnixMilli(1_700_000_000_000 + n) }}), st, dir
}

var ctx = audit.WithActor(context.Background(), audit.Actor{UserID: 0, Name: "管理员", Via: audit.ViaSession})

func pngBytes(t *testing.T) []byte {
	img := image.NewRGBA(image.Rect(0, 0, 4, 4))
	img.Set(1, 1, color.RGBA{255, 0, 0, 255})
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func jpegBytes(t *testing.T) []byte {
	var b bytes.Buffer
	if err := jpeg.Encode(&b, image.NewRGBA(image.Rect(0, 0, 4, 4)), nil); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func isKind(err error, k apperr.Kind) bool {
	var e *apperr.Error
	return errors.As(err, &e) && e.Kind == k
}

func TestDefaults(t *testing.T) {
	s, _, _ := newService(t)
	b, err := s.Get(context.Background())
	if err != nil || b.Name != "收好" || b.LogoVersion != "" {
		t.Errorf("defaults = %+v, %v", b, err)
	}
	if _, _, err := s.OpenLogo(context.Background()); !errors.Is(err, ErrNoLogo) {
		t.Errorf("OpenLogo without a logo err = %v", err)
	}
}

func TestName(t *testing.T) {
	s, st, _ := newService(t)
	b, err := s.SetName(ctx, "  我家档案 ")
	if err != nil || b.Name != "我家档案" {
		t.Fatalf("SetName = %+v, %v", b, err)
	}
	for _, bad := range []string{"", "  ", strings.Repeat("长", 17)} {
		if _, err := s.SetName(ctx, bad); !isKind(err, apperr.KindInvalid) {
			t.Errorf("SetName(%q) err = %v", bad, err)
		}
	}
	var summary string
	if err := st.DB.QueryRow(`SELECT summary FROM audit_events WHERE action = 'settings.brand_update' AND workspace_id IS NULL`).Scan(&summary); err != nil {
		t.Fatal(err)
	}
	if summary != "管理员 将产品名称从“收好”改为“我家档案”" {
		t.Errorf("summary = %q", summary)
	}
}

func TestLogo(t *testing.T) {
	s, _, dir := newService(t)
	b, err := s.SetLogo(ctx, bytes.NewReader(pngBytes(t)))
	if err != nil {
		t.Fatal(err)
	}
	first := b.LogoVersion
	if first == "" {
		t.Fatal("no logo version after upload")
	}
	f, ctype, err := s.OpenLogo(context.Background())
	if err != nil || ctype != "image/png" {
		t.Fatalf("OpenLogo = %q, %v", ctype, err)
	}
	f.Close()

	// Replacing the logo changes the version and removes the old file.
	b, err = s.SetLogo(ctx, bytes.NewReader(jpegBytes(t)))
	if err != nil || b.LogoVersion == first {
		t.Fatalf("second upload = %+v, %v", b, err)
	}
	f, ctype, err = s.OpenLogo(context.Background())
	if err != nil || ctype != "image/jpeg" {
		t.Fatalf("after replacing: %q, %v", ctype, err)
	}
	f.Close()
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Errorf("files in the branding directory: %v, want only the current logo", entries)
	}

	if b, err := s.ClearLogo(ctx); err != nil || b.LogoVersion != "" {
		t.Errorf("ClearLogo = %+v, %v", b, err)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Errorf("logo file kept after clearing: %v", entries)
	}
}

func TestLogoRejectsWhatIsNotARasterImage(t *testing.T) {
	s, _, _ := newService(t)
	svg := []byte(`<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`)
	big := append(pngBytes(t), make([]byte, MaxLogoSize)...)
	for name, body := range map[string][]byte{"svg": svg, "text": []byte("hello"), "empty": nil, "too large": big} {
		if _, err := s.SetLogo(ctx, bytes.NewReader(body)); !isKind(err, apperr.KindInvalid) {
			t.Errorf("%s: err = %v, want invalid", name, err)
		}
	}
	if b, _ := s.Get(context.Background()); b.LogoVersion != "" {
		t.Error("a rejected upload changed the logo")
	}
}
