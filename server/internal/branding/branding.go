// Package branding is the instance's product name and logo, shown on the
// login page and in the sidebar (design doc 11.4). The name is a setting;
// the logo is an image file in the data directory.
package branding

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"kept/server/internal/apperr"
	"kept/server/internal/audit"
	"kept/server/internal/i18n"
	"kept/server/internal/store/sqlc"
)

// defaultName is DefaultName where it is said: in English, Kept.
var _ = i18n.N("收好")

const (
	DefaultName = "收好"
	MaxNameLen  = 16
	MaxLogoSize = 512 << 10

	keyName = "brand_name"
	keyLogo = "brand_logo" // file name of the current logo, e.g. logo-1700000000000.png
)

// Raster formats only: an SVG could carry script.
var logoTypes = map[string]string{"image/png": "png", "image/jpeg": "jpg", "image/webp": "webp"}

var (
	ErrNoLogo   = errors.New("branding: no custom logo")
	errName     = apperr.New(apperr.KindInvalid, "产品名称需要 1 到 16 个字符")
	errLogoType = apperr.New(apperr.KindInvalid, "Logo 只支持 PNG、JPEG、WebP 图片")
	errLogoSize = apperr.New(apperr.KindInvalid, "Logo 不能超过 512 KB")
)

type Options struct {
	Now func() time.Time // default time.Now
}

type Service struct {
	db  *sql.DB
	q   *sqlc.Queries
	dir string
	now func() time.Time
}

// NewService keeps logo files in dir (created when needed).
func NewService(db *sql.DB, dir string, opt Options) *Service {
	if opt.Now == nil {
		opt.Now = time.Now
	}
	return &Service{db: db, q: sqlc.New(db), dir: dir, now: opt.Now}
}

// Branding is what clients show. LogoVersion is empty for the default logo;
// otherwise it changes with every upload, for cache busting.
type Branding struct {
	Name        string
	LogoVersion string
}

func (s *Service) Get(ctx context.Context) (Branding, error) {
	return get(ctx, s.q)
}

func get(ctx context.Context, q *sqlc.Queries) (Branding, error) {
	b := Branding{Name: DefaultName}
	name, err := setting(ctx, q, keyName)
	if err != nil {
		return b, err
	}
	if name != "" {
		b.Name = name
	}
	logo, err := setting(ctx, q, keyLogo)
	if err != nil {
		return b, err
	}
	b.LogoVersion = logoVersion(logo)
	return b, nil
}

func (s *Service) SetName(ctx context.Context, name string) (Branding, error) {
	name = strings.TrimSpace(name)
	if n := utf8.RuneCountInString(name); n < 1 || n > MaxNameLen {
		return Branding{}, errName
	}
	var b Branding
	err := s.inTx(ctx, func(q *sqlc.Queries) error {
		before, err := get(ctx, q)
		if err != nil {
			return err
		}
		b = before
		if name == before.Name {
			return nil
		}
		if err := q.UpsertSetting(ctx, sqlc.UpsertSettingParams{Key: keyName, Value: name}); err != nil {
			return err
		}
		b.Name = name
		return audit.Record(ctx, q, audit.Event{
			Action: "settings.brand_update", TargetType: "setting", TargetID: keyName,
			Summary: fmt.Sprintf(i18n.N("%s 将产品名称从“%s”改为“%s”"), audit.ActorFrom(ctx).Name, before.Name, name),
			Changes: map[string][2]any{"name": {before.Name, name}}, At: s.now(),
		})
	})
	return b, err
}

// SetLogo stores a new logo. The format is decided from the content, not
// from what the client claims.
func (s *Service) SetLogo(ctx context.Context, r io.Reader) (Branding, error) {
	data, err := io.ReadAll(io.LimitReader(r, MaxLogoSize+1))
	if err != nil {
		return Branding{}, err
	}
	if len(data) > MaxLogoSize {
		return Branding{}, errLogoSize
	}
	ext, ok := logoTypes[sniff(data)]
	if !ok {
		return Branding{}, errLogoType
	}
	// The file is written before the transaction (no file I/O inside one);
	// its name carries the version, so an old one is never overwritten.
	file := "logo-" + strconv.FormatInt(s.now().UnixMilli(), 10) + "." + ext
	if err := writeFile(s.dir, file, data); err != nil {
		return Branding{}, err
	}
	b, err := s.replaceLogo(ctx, file, i18n.N("%s 更换了 Logo"))
	if err != nil {
		_ = os.Remove(filepath.Join(s.dir, file))
		return Branding{}, err
	}
	s.removeOtherLogos(file)
	return b, nil
}

// ClearLogo goes back to the default logo.
func (s *Service) ClearLogo(ctx context.Context) (Branding, error) {
	b, err := s.replaceLogo(ctx, "", i18n.N("%s 恢复了默认 Logo"))
	if err != nil {
		return Branding{}, err
	}
	s.removeOtherLogos("")
	return b, nil
}

// summary is a format taking the actor's name.
func (s *Service) replaceLogo(ctx context.Context, file, summary string) (Branding, error) {
	var b Branding
	err := s.inTx(ctx, func(q *sqlc.Queries) error {
		before, err := setting(ctx, q, keyLogo)
		if err != nil {
			return err
		}
		if before == file {
			b, err = get(ctx, q)
			return err
		}
		if file == "" {
			err = q.DeleteSetting(ctx, keyLogo)
		} else {
			err = q.UpsertSetting(ctx, sqlc.UpsertSettingParams{Key: keyLogo, Value: file})
		}
		if err != nil {
			return err
		}
		if err := audit.Record(ctx, q, audit.Event{
			Action: "settings.logo_update", TargetType: "setting", TargetID: keyLogo,
			Summary: fmt.Sprintf(summary, audit.ActorFrom(ctx).Name), At: s.now(),
		}); err != nil {
			return err
		}
		b, err = get(ctx, q)
		return err
	})
	return b, err
}

// OpenLogo returns the current logo file and its content type, or ErrNoLogo.
func (s *Service) OpenLogo(ctx context.Context) (*os.File, string, error) {
	file, err := setting(ctx, s.q, keyLogo)
	if err != nil {
		return nil, "", err
	}
	if file == "" {
		return nil, "", ErrNoLogo
	}
	f, err := os.Open(filepath.Join(s.dir, filepath.Base(file)))
	if errors.Is(err, os.ErrNotExist) {
		return nil, "", ErrNoLogo
	}
	if err != nil {
		return nil, "", err
	}
	head := make([]byte, 512)
	n, _ := f.Read(head)
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		f.Close()
		return nil, "", err
	}
	return f, sniff(head[:n]), nil
}

// removeOtherLogos deletes every logo file but keep (best effort).
func (s *Service) removeOtherLogos(keep string) {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if e.Name() != keep && strings.HasPrefix(e.Name(), "logo-") {
			_ = os.Remove(filepath.Join(s.dir, e.Name()))
		}
	}
}

func sniff(data []byte) string {
	ct := http.DetectContentType(data)
	if i := strings.IndexByte(ct, ';'); i >= 0 {
		ct = ct[:i]
	}
	return ct
}

// logoVersion is the version part of a logo file name ("" for none).
func logoVersion(file string) string {
	v := strings.TrimPrefix(file, "logo-")
	if i := strings.IndexByte(v, '.'); i >= 0 {
		v = v[:i]
	}
	return v
}

// writeFile writes atomically: a temporary file renamed into place.
func writeFile(dir, name string, data []byte) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".upload-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := io.Copy(tmp, bytes.NewReader(data)); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmp.Name(), filepath.Join(dir, name)); err != nil {
		return fmt.Errorf("store logo: %w", err)
	}
	return nil
}

func setting(ctx context.Context, q *sqlc.Queries, key string) (string, error) {
	v, err := q.GetSetting(ctx, key)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return v, err
}

func (s *Service) inTx(ctx context.Context, fn func(*sqlc.Queries) error) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := fn(s.q.WithTx(tx)); err != nil {
		return err
	}
	return tx.Commit()
}
