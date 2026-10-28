package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"time"

	"kept/server/internal/apperr"
	"kept/server/internal/audit"
	"kept/server/internal/config"
	"kept/server/internal/files"
	"kept/server/internal/i18n"
	"kept/server/internal/secret"
)

// Storages added on the page (v1.7.2), kept in the storages table besides
// those of KEPT_STORAGES. Their secret key is sealed with the master key.

// Settings of a storage added on the page, without the secret key.
type Settings struct {
	Path      string `json:"path,omitempty"`     // dir
	Endpoint  string `json:"endpoint,omitempty"` // s3: as entered, host[:port] with http:// for no TLS
	Region    string `json:"region,omitempty"`
	Bucket    string `json:"bucket,omitempty"`
	Prefix    string `json:"prefix,omitempty"`
	PathStyle bool   `json:"pathStyle,omitempty"`
	AccessKey string `json:"accessKey,omitempty"`
	URL       string `json:"url,omitempty"`      // webdav: http(s)://host/folder (v1.7.5)
	Username  string `json:"username,omitempty"` // webdav
}

// Input is a storage as the administrator fills it in.
type Input struct {
	Name        string
	Kind        string // dir | s3 | webdav
	Description string
	Settings    Settings
	SecretKey   string // s3 secret key, webdav password; "" when changing one keeps the saved one
	Selectable  bool   // workspace administrators may pick it
	Capacity    int64  // the most it may hold, bytes; 0 no limit (v1.7.10)
}

// Build makes the backend of a configured storage.
func Build(s config.Storage) (files.Backend, error) {
	switch s.Kind {
	case "dir":
		return files.DirBackend{Root: s.Path}, nil
	case "s3":
		return files.NewS3(files.S3Config{Endpoint: s.S3.Endpoint, Secure: s.S3.Secure, Region: s.S3.Region, Bucket: s.S3.Bucket,
			Prefix: s.S3.Prefix, AccessKey: s.S3.AccessKey, SecretKey: s.S3.SecretKey, PathStyle: s.S3.PathStyle})
	case "webdav":
		return files.NewWebDAV(files.WebDAVConfig{URL: s.WebDAV.URL, Username: s.WebDAV.Username, Password: s.WebDAV.Password})
	}
	return nil, fmt.Errorf("unknown kind %q", s.Kind)
}

// configOf checks what was entered and turns it into a storage.
func configOf(dataDir, name, kind string, set Settings, secretKey string) (config.Storage, error) {
	invalid := func(msg string) (config.Storage, error) { return config.Storage{}, apperr.New(apperr.KindInvalid, msg) }
	out := config.Storage{Name: name, Kind: kind}
	switch kind {
	case "dir":
		p, err := config.CheckStorageDir(dataDir, strings.TrimSpace(set.Path))
		if err != nil {
			return invalid("目录要写绝对路径（如 /mnt/nas/kept），并且不能在数据目录里")
		}
		out.Path = p
	case "s3":
		host, secure, err := config.ParseEndpoint(set.Endpoint)
		if err != nil {
			return invalid("服务地址写成“主机:端口”，没有证书的服务前面加 http://，例如 http://192.168.1.10:9000")
		}
		bucket := strings.TrimSpace(set.Bucket)
		if bucket == "" || strings.ContainsAny(bucket, "/?# ") {
			return invalid("请填写桶的名称")
		}
		if strings.TrimSpace(set.AccessKey) == "" || secretKey == "" {
			return invalid("请填写 Access Key 和 Secret Key")
		}
		out.S3 = config.S3{Endpoint: host, Secure: secure, Region: strings.TrimSpace(set.Region), Bucket: bucket,
			Prefix: strings.Trim(strings.TrimSpace(set.Prefix), "/"), PathStyle: set.PathStyle,
			AccessKey: strings.TrimSpace(set.AccessKey), SecretKey: secretKey}
	case "webdav":
		u, err := url.Parse(strings.TrimSpace(set.URL))
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil {
			return invalid("WebDAV 地址写成 https://服务器/文件夹，例如 https://dav.jianguoyun.com/dav/kept；用户名和密码填在下面")
		}
		out.WebDAV = config.WebDAV{URL: strings.TrimSpace(set.URL), Username: strings.TrimSpace(set.Username), Password: secretKey}
	default:
		return invalid("存储类型只能是本机目录、S3 兼容存储或 WebDAV")
	}
	return out, nil
}

// place is where files go: changing it would lose the files already there.
func place(kind string, s Settings) string {
	switch kind {
	case "dir":
		return strings.TrimRight(strings.TrimSpace(s.Path), "/")
	case "webdav":
		return strings.TrimRight(strings.TrimSpace(s.URL), "/")
	}
	return strings.TrimSpace(s.Bucket) + "/" + strings.Trim(strings.TrimSpace(s.Prefix), "/")
}

type savedRow struct {
	kind, description, settings, sealed string
	selectable                          bool
}

func (s *Service) savedRow(ctx context.Context, q interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, name string) (savedRow, error) {
	var r savedRow
	var sel int64
	err := q.QueryRowContext(ctx, `SELECT kind, description, settings, secret, selectable FROM storages WHERE name = ?`, name).
		Scan(&r.kind, &r.description, &r.settings, &r.sealed, &sel)
	r.selectable = sel == 1
	return r, err
}

// broken remembers the saved storages that could not be set up, by name.
type brokenSet struct {
	mu sync.Mutex
	m  map[string]string
}

func (b *brokenSet) set(name string, err error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.m == nil {
		b.m = map[string]string{}
	}
	if err == nil {
		delete(b.m, name)
	} else {
		b.m[name] = err.Error()
	}
}

func (b *brokenSet) get(name string) string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.m[name]
}

// isEnv: a storage of the deployment, not one added on the page.
func (s *Service) isEnv(name string) bool {
	for _, e := range s.opt.Env {
		if e.Name == name {
			return true
		}
	}
	return false
}

// LoadSaved makes the storages added on the page available, at start.
// One that cannot be set up (the master key changed) is listed with its
// error and left out.
func (s *Service) LoadSaved(ctx context.Context) error {
	rows, err := s.db.QueryContext(ctx, `SELECT name, kind, settings, secret FROM storages ORDER BY name`)
	if err != nil {
		return err
	}
	type row struct{ name, kind, settings, sealed string }
	var list []row
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.name, &r.kind, &r.settings, &r.sealed); err != nil {
			rows.Close()
			return err
		}
		list = append(list, r)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	for _, r := range list {
		if s.isEnv(r.name) { // the deployment's wins
			s.broken.set(r.name, errors.New(i18n.N("部署配置里有同名的存储，这一项没有使用")))
			continue
		}
		err := s.register(r.name, r.kind, r.settings, r.sealed)
		s.broken.set(r.name, err)
		if err != nil {
			s.opt.Log.Error("set up a storage added on the page", "storage", r.name, "err", err)
		}
	}
	return nil
}

func (s *Service) register(name, kind, settings, sealed string) error {
	var set Settings
	if err := json.Unmarshal([]byte(settings), &set); err != nil {
		return err
	}
	key := ""
	if sealed != "" {
		if s.opt.Box == nil {
			return secret.ErrDecrypt
		}
		var err error
		if key, err = s.opt.Box.Open(sealed); err != nil {
			return err
		}
	}
	c, err := configOf(s.opt.DataDir, name, kind, set, key)
	if err != nil {
		return err
	}
	b, err := Build(c)
	if err != nil {
		return err
	}
	s.files.AddStorage(name, b)
	return nil
}

func (s *Service) checkNew(ctx context.Context, name string) error {
	if err := config.CheckStorageName(name); err != nil {
		return apperr.New(apperr.KindInvalid, "名称只能用小写字母、数字和 -，以字母开头，最多 32 个字符；local 是数据目录")
	}
	if _, err := s.files.Backend(name); err == nil || s.isEnv(name) {
		return apperr.New(apperr.KindConflict, fmt.Sprintf("已经有叫“%s”的存储", name))
	}
	var n int
	if err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM storages WHERE name = ?`, name).Scan(&n); err != nil {
		return err
	}
	if n > 0 {
		return apperr.New(apperr.KindConflict, fmt.Sprintf("已经有叫“%s”的存储", name))
	}
	// A name documents still use (a storage removed from the deployment).
	if err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM documents WHERE storage = ?`, name).Scan(&n); err != nil {
		return err
	}
	if n > 0 {
		return apperr.New(apperr.KindConflict, fmt.Sprintf("有 %d 份文档记在“%s”里，请换个名字，或在部署配置里把它加回来", n, name))
	}
	return nil
}

// Create adds a storage.
func (s *Service) Create(ctx context.Context, in Input) error {
	in.Name = strings.TrimSpace(in.Name)
	if err := s.checkNew(ctx, in.Name); err != nil {
		return err
	}
	c, err := configOf(s.opt.DataDir, in.Name, in.Kind, in.Settings, in.SecretKey)
	if err != nil {
		return err
	}
	b, err := Build(c)
	if err != nil {
		return apperr.New(apperr.KindInvalid, err.Error())
	}
	sealed, err := s.seal(in)
	if err != nil {
		return err
	}
	in.Settings = clean(in.Kind, in.Settings)
	setJSON, _ := json.Marshal(in.Settings)
	now := s.opt.Now().UnixMilli()
	actor := audit.ActorFrom(ctx)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if in.Capacity < 0 {
		return apperr.New(apperr.KindInvalid, "容量上限不能是负数")
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO storages (name, kind, description, settings, secret, selectable, capacity, created_by, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, in.Name, in.Kind, strings.TrimSpace(in.Description), string(setJSON), sealed, boolInt(in.Selectable),
		in.Capacity, sql.NullInt64{Int64: actor.UserID, Valid: actor.UserID != 0}, now, now); err != nil {
		return err
	}
	if err := audit.Record(ctx, s.q.WithTx(tx), audit.Event{Action: "storage.create", TargetType: "storage", TargetID: in.Name,
		Summary: fmt.Sprintf(i18n.N("%s 添加了存储“%s”"), actor.Name, in.Name), At: s.opt.Now()}); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	s.files.AddStorage(in.Name, b)
	s.broken.set(in.Name, nil)
	return nil
}

// Update changes a storage added on the page. Where a storage with
// documents keeps its files (directory, bucket, prefix) cannot change.
func (s *Service) Update(ctx context.Context, name string, in Input) error {
	if s.isEnv(name) {
		return apperr.New(apperr.KindConflict, "这是部署配置里的存储，请在部署文件里修改")
	}
	old, err := s.savedRow(ctx, s.db, name)
	if errors.Is(err, sql.ErrNoRows) {
		return apperr.New(apperr.KindNotFound, "没有这个存储")
	}
	if err != nil {
		return err
	}
	var oldSet Settings
	_ = json.Unmarshal([]byte(old.settings), &oldSet)
	var docs int
	if err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM documents WHERE storage = ?`, name).Scan(&docs); err != nil {
		return err
	}
	if docs > 0 && (in.Kind != old.kind || place(in.Kind, in.Settings) != place(old.kind, oldSet)) {
		return apperr.New(apperr.KindConflict, fmt.Sprintf("这里已经放了 %d 份文档，不能改类型、目录、桶或前缀；地址和密钥可以改", docs))
	}
	if in.SecretKey == "" && in.Kind != "dir" && old.sealed != "" { // unchanged
		if s.opt.Box == nil {
			return secret.ErrDecrypt
		}
		key, err := s.opt.Box.Open(old.sealed)
		if err != nil {
			return apperr.New(apperr.KindInvalid, "保存的 Secret Key 解不开（主密钥变了），请重新填写")
		}
		in.SecretKey = key
	}
	c, err := configOf(s.opt.DataDir, name, in.Kind, in.Settings, in.SecretKey)
	if err != nil {
		return err
	}
	b, err := Build(c)
	if err != nil {
		return apperr.New(apperr.KindInvalid, err.Error())
	}
	sealed, err := s.seal(in)
	if err != nil {
		return err
	}
	in.Settings = clean(in.Kind, in.Settings)
	setJSON, _ := json.Marshal(in.Settings)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if in.Capacity < 0 {
		return apperr.New(apperr.KindInvalid, "容量上限不能是负数")
	}
	if _, err := tx.ExecContext(ctx, `UPDATE storages SET kind = ?, description = ?, settings = ?, secret = ?, selectable = ?, capacity = ?, updated_at = ? WHERE name = ?`,
		in.Kind, strings.TrimSpace(in.Description), string(setJSON), sealed, boolInt(in.Selectable), in.Capacity, s.opt.Now().UnixMilli(), name); err != nil {
		return err
	}
	if err := audit.Record(ctx, s.q.WithTx(tx), audit.Event{Action: "storage.update", TargetType: "storage", TargetID: name,
		Summary: fmt.Sprintf(i18n.N("%s 修改了存储“%s”"), audit.ActorFrom(ctx).Name, name), At: s.opt.Now()}); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	s.files.AddStorage(name, b)
	s.broken.set(name, nil)
	return nil
}

// Delete removes a storage added on the page that nothing uses.
func (s *Service) Delete(ctx context.Context, name string) error {
	if s.isEnv(name) {
		return apperr.New(apperr.KindConflict, "这是部署配置里的存储，请在部署文件里去掉")
	}
	if _, err := s.savedRow(ctx, s.db, name); errors.Is(err, sql.ErrNoRows) {
		return apperr.New(apperr.KindNotFound, "没有这个存储")
	} else if err != nil {
		return err
	}
	var docs, spaces int
	if err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM documents WHERE storage = ?`, name).Scan(&docs); err != nil {
		return err
	}
	if err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM workspaces WHERE storage = ? AND deleted_at IS NULL`, name).Scan(&spaces); err != nil {
		return err
	}
	if docs > 0 || spaces > 0 {
		return apperr.New(apperr.KindConflict, fmt.Sprintf("还有 %d 份文档、%d 个空间在用这个存储，先把它们换到别处", docs, spaces))
	}
	if s.isBackupTarget(ctx, name) {
		return apperr.New(apperr.KindConflict, "这个存储是备份目标，先在“备份”里换一个")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM storages WHERE name = ?`, name); err != nil {
		return err
	}
	if err := forgetKey(ctx, tx, name); err != nil {
		return err
	}
	if err := audit.Record(ctx, s.q.WithTx(tx), audit.Event{Action: "storage.delete", TargetType: "storage", TargetID: name,
		Summary: fmt.Sprintf(i18n.N("%s 删除了存储“%s”"), audit.ActorFrom(ctx).Name, name), At: s.opt.Now()}); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	s.files.RemoveStorage(name)
	s.broken.set(name, nil)
	return nil
}

// TestInput tries settings before they are saved. existing names the
// storage being changed, whose saved key is used when none is entered.
func (s *Service) TestInput(ctx context.Context, existing string, in Input) (time.Duration, error) {
	if in.SecretKey == "" && existing != "" && in.Kind != "dir" {
		if old, err := s.savedRow(ctx, s.db, existing); err == nil && old.sealed != "" && s.opt.Box != nil {
			in.SecretKey, _ = s.opt.Box.Open(old.sealed)
		}
	}
	c, err := configOf(s.opt.DataDir, "test", in.Kind, in.Settings, in.SecretKey)
	if err != nil {
		return 0, err
	}
	b, err := Build(c)
	if err != nil {
		return 0, err
	}
	return s.probe(ctx, b)
}

func (s *Service) seal(in Input) (string, error) {
	if in.Kind == "dir" || in.SecretKey == "" {
		return "", nil
	}
	if s.opt.Box == nil {
		return "", errors.New("no master key")
	}
	return s.opt.Box.Seal(in.SecretKey)
}

// clean keeps the settings of the kind, trimmed.
func clean(kind string, set Settings) Settings {
	switch kind {
	case "dir":
		return Settings{Path: strings.TrimSpace(set.Path)}
	case "webdav":
		return Settings{URL: strings.TrimSpace(set.URL), Username: strings.TrimSpace(set.Username)}
	}
	return Settings{Endpoint: strings.TrimSpace(set.Endpoint), Region: strings.TrimSpace(set.Region), Bucket: strings.TrimSpace(set.Bucket),
		Prefix: strings.Trim(strings.TrimSpace(set.Prefix), "/"), PathStyle: set.PathStyle, AccessKey: strings.TrimSpace(set.AccessKey)}
}

func boolInt(b bool) int64 {
	if b {
		return 1
	}
	return 0
}

// Choice is a storage a workspace administrator may pick.
type Choice struct {
	Name, Kind, Description string
}

// Choices are the storages workspaces may pick: the data directory, those
// of the deployment, and those added on the page that are allowed.
func (s *Service) Choices(ctx context.Context) ([]Choice, error) {
	list, err := s.List(ctx)
	if err != nil {
		return nil, err
	}
	var out []Choice
	target, err := s.GetBackupTarget(ctx)
	if err != nil {
		return nil, err
	}
	for _, st := range list {
		if st.Kind == "missing" || st.Error != "" || (st.Source == SourcePage && !st.Selectable) || st.Name == target.Storage {
			continue
		}
		out = append(out, Choice{Name: st.Name, Kind: st.Kind, Description: st.Description})
	}
	return out, nil
}

// Allowed: a workspace administrator may use storage name (Choices).
func (s *Service) Allowed(ctx context.Context, name string) (bool, error) {
	choices, err := s.Choices(ctx)
	if err != nil {
		return false, err
	}
	for _, c := range choices {
		if c.Name == name {
			return true, nil
		}
	}
	return false, nil
}

// Backend is a configured storage's backend.
func (s *Service) Backend(name string) (files.Backend, error) { return s.files.Backend(name) }

// SetOwn is SetWorkspace for a workspace administrator: only a storage
// among Choices (or the one it has).
func (s *Service) SetOwn(ctx context.Context, ws int64, name string, move bool) (int64, error) {
	var cur string
	if err := s.db.QueryRowContext(ctx, `SELECT storage FROM workspaces WHERE id = ?`, ws).Scan(&cur); err != nil {
		return 0, err
	}
	if name != cur {
		choices, err := s.Choices(ctx)
		if err != nil {
			return 0, err
		}
		ok := false
		for _, c := range choices {
			ok = ok || c.Name == name
		}
		if !ok {
			return 0, apperr.New(apperr.KindInvalid, "不能选这个存储")
		}
	}
	return s.SetWorkspace(ctx, ws, name, move)
}
