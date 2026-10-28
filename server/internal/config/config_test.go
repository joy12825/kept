package config

import (
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func env(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func TestLoadDefaults(t *testing.T) {
	c, err := Load(env(nil))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if c.DataDir != "data" || c.HTTPAddr != ":8000" || c.LogLevel != "info" || c.LogFormat != "text" {
		t.Errorf("unexpected defaults: %+v", c)
	}
	if c.Workers < 1 {
		t.Errorf("Workers = %d, want >= 1", c.Workers)
	}
	if c.MaxUploadSize != 200<<20 || c.OCRMode != "skip" || c.OCRLanguages != "chi_sim+eng" || !c.OCRRotate || !c.OCRDeskew {
		t.Errorf("ingest defaults: %d %q %q", c.MaxUploadSize, c.OCRMode, c.OCRLanguages)
	}
	if c.AuditRetention != 365*24*time.Hour {
		t.Errorf("audit retention default = %s, want 365 days", c.AuditRetention)
	}
	if c.SessionTTL != 30*24*time.Hour || c.SecureCookie != "auto" {
		t.Errorf("session defaults: ttl=%s secure=%s", c.SessionTTL, c.SecureCookie)
	}
	if c.DBPath() != filepath.Join("data", "app.db") {
		t.Errorf("DBPath = %q", c.DBPath())
	}
}

func TestLoadOverrides(t *testing.T) {
	c, err := Load(env(map[string]string{
		"KEPT_DATA_DIR":         "/srv/kept",
		"KEPT_HTTP_ADDR":        "127.0.0.1:9000",
		"KEPT_LOG_LEVEL":        "DEBUG",
		"KEPT_LOG_FORMAT":       "json",
		"KEPT_WORKERS":          "3",
		"KEPT_SHUTDOWN_TIMEOUT": "5s",
		"KEPT_SESSION_TTL":      "72h",
		"KEPT_AUDIT_RETENTION":  "48h",
		"KEPT_MAX_UPLOAD_SIZE":  "50MB",
		"KEPT_OCR_LANGUAGES":    "eng",
		"KEPT_OCR_MODE":         "FORCE",
		"KEPT_OCR_DESKEW":       "false",
		"KEPT_SECURE_COOKIE":    "always",
	}))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if c.DataDir != "/srv/kept" || c.HTTPAddr != "127.0.0.1:9000" || c.LogLevel != "debug" ||
		c.LogFormat != "json" || c.Workers != 3 || c.ShutdownTimeout != 5*time.Second ||
		c.SessionTTL != 72*time.Hour || c.AuditRetention != 48*time.Hour || c.SecureCookie != "always" ||
		c.MaxUploadSize != 50<<20 || c.OCRLanguages != "eng" || c.OCRMode != "force" || !c.OCRRotate || c.OCRDeskew {
		t.Errorf("overrides not applied: %+v", c)
	}
}

func TestLoadInvalidValuesReportsAll(t *testing.T) {
	_, err := Load(env(map[string]string{
		"KEPT_LOG_LEVEL":        "loud",
		"KEPT_LOG_FORMAT":       "xml",
		"KEPT_WORKERS":          "0",
		"KEPT_SHUTDOWN_TIMEOUT": "soon",
		"KEPT_SESSION_TTL":      "-1h",
		"KEPT_AUDIT_RETENTION":  "forever",
		"KEPT_MAX_UPLOAD_SIZE":  "huge",
		"KEPT_SEARCH_LIMIT":     "5",
		"KEPT_GOTENBERG_URL":    "gotenberg:3000",
		"KEPT_OCR_MODE":         "sometimes",
		"KEPT_OCR_ROTATE":       "maybe",
		"KEPT_SECURE_COOKIE":    "sometimes",
		"KEPT_BARCODE_DPI":      "1200",
	}))
	if err == nil {
		t.Fatal("expected an error")
	}
	for _, key := range []string{"KEPT_LOG_LEVEL", "KEPT_LOG_FORMAT", "KEPT_WORKERS", "KEPT_SHUTDOWN_TIMEOUT", "KEPT_SESSION_TTL", "KEPT_AUDIT_RETENTION", "KEPT_MAX_UPLOAD_SIZE", "KEPT_OCR_MODE", "KEPT_OCR_ROTATE", "KEPT_SECURE_COOKIE", "KEPT_SEARCH_LIMIT", "KEPT_GOTENBERG_URL", "KEPT_BARCODE_DPI"} {
		if !strings.Contains(err.Error(), key) {
			t.Errorf("error does not mention %s: %v", key, err)
		}
	}
}

func TestSMTP(t *testing.T) {
	env := func(m map[string]string) func(string) string { return func(k string) string { return m[k] } }
	c, err := Load(env(nil))
	if err != nil || c.SMTP.Host != "" || c.SMTP.MaxAttachments != 20<<20 {
		t.Fatalf("default: %+v %v", c.SMTP, err)
	}
	c, err = Load(env(map[string]string{"KEPT_SMTP_HOST": "smtp.example.com", "KEPT_SMTP_PORT": "465", "KEPT_SMTP_FROM": "收好 <kept@example.com>"}))
	if err != nil || c.SMTP.Security != "tls" || c.SMTP.Port != 465 {
		t.Fatalf("465: %+v %v", c.SMTP, err)
	}
	c, err = Load(env(map[string]string{"KEPT_SMTP_HOST": "smtp.example.com", "KEPT_SMTP_FROM": "kept@example.com"}))
	if err != nil || c.SMTP.Security != "starttls" || c.SMTP.Port != 587 {
		t.Fatalf("default port: %+v %v", c.SMTP, err)
	}
	_, err = Load(env(map[string]string{"KEPT_SMTP_HOST": "smtp.example.com", "KEPT_SMTP_SECURITY": "ssl", "KEPT_SMTP_MAX_ATTACHMENTS": "big"}))
	for _, key := range []string{"KEPT_SMTP_FROM", "KEPT_SMTP_SECURITY", "KEPT_SMTP_MAX_ATTACHMENTS"} {
		if err == nil || !strings.Contains(err.Error(), key) {
			t.Errorf("error does not mention %s: %v", key, err)
		}
	}
}

func TestAI(t *testing.T) {
	env := func(m map[string]string) func(string) string { return func(k string) string { return m[k] } }
	c, err := Load(env(nil))
	if err != nil || c.AI.URL != "" || c.AI.Timeout != 60*time.Second {
		t.Fatalf("default: %+v %v", c.AI, err)
	}
	c, err = Load(env(map[string]string{"KEPT_AI_URL": "http://ollama:11434/v1/", "KEPT_AI_MODEL": "qwen2.5:7b", "KEPT_AI_TIMEOUT": "30s"}))
	if err != nil || c.AI.URL != "http://ollama:11434/v1" || c.AI.Timeout != 30*time.Second {
		t.Fatalf("set: %+v %v", c.AI, err)
	}
	_, err = Load(env(map[string]string{"KEPT_AI_URL": "ollama:11434", "KEPT_AI_TIMEOUT": "soon"}))
	for _, key := range []string{"KEPT_AI_URL", "KEPT_AI_MODEL", "KEPT_AI_TIMEOUT"} {
		if err == nil || !strings.Contains(err.Error(), key) {
			t.Errorf("error does not mention %s: %v", key, err)
		}
	}
}

func TestBookSources(t *testing.T) {
	env := func(m map[string]string) func(string) string { return func(k string) string { return m[k] } }
	for _, tc := range []struct {
		env  map[string]string
		want string
	}{
		{nil, "openlibrary,googlebooks"},
		{map[string]string{"KEPT_BOOK_SOURCES": "none"}, ""},
		{map[string]string{"KEPT_BOOK_LOOKUP_URL": "http://lib.lan/isbn/{isbn}"}, "custom,openlibrary,googlebooks"},
		{map[string]string{"KEPT_BOOK_SOURCES": "googlebooks, custom", "KEPT_BOOK_LOOKUP_URL": "https://lib.lan/q?isbn={isbn}"}, "googlebooks,custom"},
	} {
		c, err := Load(env(tc.env))
		if err != nil || strings.Join(c.Books.Sources, ",") != tc.want {
			t.Errorf("%v: %v %v", tc.env, c.Books.Sources, err)
		}
	}
	for _, bad := range []map[string]string{
		{"KEPT_BOOK_SOURCES": "douban"},
		{"KEPT_BOOK_SOURCES": "custom"},
		{"KEPT_BOOK_LOOKUP_URL": "http://lib.lan/isbn"},
		{"KEPT_BOOK_LOOKUP_URL": "lib.lan/{isbn}"},
	} {
		if _, err := Load(env(bad)); err == nil || !strings.Contains(err.Error(), "KEPT_BOOK") {
			t.Errorf("%v: %v", bad, err)
		}
	}
}

func TestEmbedConfig(t *testing.T) {
	env := func(m map[string]string) func(string) string { return func(k string) string { return m[k] } }
	c, err := Load(env(map[string]string{"KEPT_AI_URL": "http://ollama:11434/v1", "KEPT_AI_MODEL": "qwen2.5:7b", "KEPT_AI_KEY": "k", "KEPT_AI_EMBED_MODEL": "bge-m3"}))
	if err != nil || c.AI.EmbedURL != "http://ollama:11434/v1" || c.AI.EmbedKey != "k" || c.AI.EmbedModel != "bge-m3" {
		t.Fatalf("defaults: %+v %v", c.AI, err)
	}
	c, err = Load(env(map[string]string{"KEPT_AI_EMBED_MODEL": "bge-m3", "KEPT_AI_EMBED_URL": "http://emb:8080/v1/"}))
	if err != nil || c.AI.URL != "" || c.AI.EmbedURL != "http://emb:8080/v1" {
		t.Fatalf("embeddings only: %+v %v", c.AI, err)
	}
	if _, err := Load(env(map[string]string{"KEPT_AI_EMBED_MODEL": "bge-m3"})); err == nil || !strings.Contains(err.Error(), "KEPT_AI_EMBED_MODEL") {
		t.Errorf("no URL: %v", err)
	}
}

func TestSSOConfig(t *testing.T) {
	env := func(m map[string]string) func(string) string { return func(k string) string { return m[k] } }
	c, err := Load(env(map[string]string{"KEPT_OIDC_ISSUER": "https://id.example.com/realms/home/", "KEPT_OIDC_CLIENT_ID": "kept",
		"KEPT_OIDC_CLIENT_SECRET": "s", "KEPT_PUBLIC_URLS": "https://docs.example.com, http://192.168.1.5:8000"}))
	if err != nil || c.SSO.Issuer != "https://id.example.com/realms/home" || c.SSO.Name != "单点登录" ||
		c.SSO.RedirectURL != "https://docs.example.com/api/v1/auth/sso/callback" || strings.Join(c.SSO.Scopes, " ") != "openid email profile" {
		t.Fatalf("defaults: %+v %v", c.SSO, err)
	}
	c, err = Load(env(map[string]string{"KEPT_OIDC_ISSUER": "https://id.example.com", "KEPT_OIDC_CLIENT_ID": "kept", "KEPT_OIDC_NAME": "飞书",
		"KEPT_OIDC_SCOPES": "email,profile", "KEPT_OIDC_REDIRECT_URL": "https://k.example.com/api/v1/auth/sso/callback", "KEPT_OIDC_TRUST_EMAIL": "true"}))
	if err != nil || c.SSO.Name != "飞书" || strings.Join(c.SSO.Scopes, " ") != "openid email profile" || !c.SSO.TrustEmail {
		t.Fatalf("set: %+v %v", c.SSO, err)
	}
	if _, err := Load(env(map[string]string{"KEPT_OIDC_ISSUER": "https://id.example.com"})); err == nil ||
		!strings.Contains(err.Error(), "KEPT_OIDC_CLIENT_ID") || !strings.Contains(err.Error(), "KEPT_OIDC_REDIRECT_URL") {
		t.Errorf("missing: %v", err)
	}
	if c, err := Load(env(nil)); err != nil || c.SSO.Issuer != "" {
		t.Errorf("off: %+v %v", c.SSO, err)
	}
}

func TestDefaultQuota(t *testing.T) {
	env := func(m map[string]string) func(string) string { return func(k string) string { return m[k] } }
	if c, err := Load(env(map[string]string{"KEPT_DEFAULT_QUOTA": "20GB"})); err != nil || c.DefaultQuota != 20<<30 {
		t.Errorf("20GB = %d %v", c.DefaultQuota, err)
	}
	if c, err := Load(env(nil)); err != nil || c.DefaultQuota != 0 {
		t.Errorf("default = %d %v", c.DefaultQuota, err)
	}
	if _, err := Load(env(map[string]string{"KEPT_DEFAULT_QUOTA": "lots"})); err == nil {
		t.Error("lots accepted")
	}
}

func TestDatabaseURL(t *testing.T) {
	env := func(m map[string]string) func(string) string { return func(k string) string { return m[k] } }
	c, err := Load(env(map[string]string{"KEPT_DATABASE_URL": "postgres://kept:secret@db:5432/kept?sslmode=disable", "KEPT_DATA_DIR": "/data"}))
	if err != nil || c.Database() != "postgres://kept:secret@db:5432/kept?sslmode=disable" || c.DatabaseLabel() != "postgres://db:5432/kept" {
		t.Fatalf("postgres = %q %q %v", c.Database(), c.DatabaseLabel(), err)
	}
	if c, _ := Load(env(map[string]string{"KEPT_DATA_DIR": "/data"})); c.Database() != "/data/app.db" {
		t.Errorf("sqlite = %q", c.Database())
	}
	if _, err := Load(env(map[string]string{"KEPT_DATABASE_URL": "mysql://x"})); err == nil {
		t.Error("mysql accepted")
	}
}

func TestTrustedProxies(t *testing.T) {
	env := func(m map[string]string) func(string) string { return func(k string) string { return m[k] } }
	c, err := Load(env(map[string]string{"KEPT_TRUSTED_PROXIES": "172.16.0.0/12, 10.0.0.7 ,::1"}))
	if err != nil || len(c.TrustedProxies) != 3 || c.TrustedProxies[1].String() != "10.0.0.7/32" || c.TrustedProxies[2].String() != "::1/128" {
		t.Fatalf("proxies = %v %v", c.TrustedProxies, err)
	}
	if _, err := Load(env(map[string]string{"KEPT_TRUSTED_PROXIES": "caddy"})); err == nil {
		t.Error("a host name accepted")
	}
}

func TestStorages(t *testing.T) {
	c, err := Load(env(map[string]string{"KEPT_DATA_DIR": "/data", "KEPT_STORAGES": "nas, cold-1",
		"KEPT_STORAGE_NAS_URL": "file:///mnt/nas/kept", "KEPT_STORAGE_COLD_1_URL": "file:///mnt/cold/"}))
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Storages) != 2 || c.Storages[0] != (Storage{Name: "nas", Kind: "dir", Path: "/mnt/nas/kept"}) || c.Storages[1] != (Storage{Name: "cold-1", Kind: "dir", Path: "/mnt/cold"}) {
		t.Errorf("storages = %+v", c.Storages)
	}
	if c, err := Load(env(nil)); err != nil || len(c.Storages) != 0 {
		t.Errorf("none = %+v %v", c.Storages, err)
	}
	// S3: a MinIO on the local network, and a cloud bucket.
	c, err = Load(env(map[string]string{"KEPT_STORAGES": "minio,cold-oss",
		"KEPT_STORAGE_MINIO_URL": "s3://kept/docs/?endpoint=http://192.168.1.10:9000&path_style=true", "KEPT_STORAGE_MINIO_ACCESS_KEY": "ak", "KEPT_STORAGE_MINIO_SECRET_KEY": "sk",
		"KEPT_STORAGE_COLD_OSS_URL": "s3://archive?endpoint=oss-cn-hangzhou.aliyuncs.com&region=cn-hangzhou", "KEPT_STORAGE_COLD_OSS_ACCESS_KEY": "ak2", "KEPT_STORAGE_COLD_OSS_SECRET_KEY": "sk2"}))
	if err != nil {
		t.Fatal(err)
	}
	if got := c.Storages[0]; got.Kind != "s3" || got.S3 != (S3{Endpoint: "192.168.1.10:9000", Bucket: "kept", Prefix: "docs", PathStyle: true, AccessKey: "ak", SecretKey: "sk"}) {
		t.Errorf("minio = %+v", got)
	}
	// WebDAV: webdav:// is http, webdavs:// https (v1.7.5).
	if c, err := Load(env(map[string]string{"KEPT_STORAGES": "jgy,nas", "KEPT_STORAGE_JGY_URL": "webdavs://dav.jianguoyun.com/dav/kept",
		"KEPT_STORAGE_JGY_USERNAME": "wang@example.com", "KEPT_STORAGE_JGY_PASSWORD": "app-pass", "KEPT_STORAGE_NAS_URL": "webdav://192.168.1.5:5005/kept"})); err != nil {
		t.Fatal(err)
	} else if c.Storages[0].WebDAV != (WebDAV{URL: "https://dav.jianguoyun.com/dav/kept", Username: "wang@example.com", Password: "app-pass"}) ||
		c.Storages[1].WebDAV.URL != "http://192.168.1.5:5005/kept" || c.Storages[1].Kind != "webdav" {
		t.Errorf("webdav = %+v", c.Storages)
	}
	if got := c.Storages[1].S3; got != (S3{Endpoint: "oss-cn-hangzhou.aliyuncs.com", Secure: true, Region: "cn-hangzhou", Bucket: "archive", AccessKey: "ak2", SecretKey: "sk2"}) {
		t.Errorf("oss = %+v", got)
	}
	for _, m := range []map[string]string{
		{"KEPT_STORAGES": "local", "KEPT_STORAGE_LOCAL_URL": "file:///x"},
		{"KEPT_STORAGES": "nas"},
		{"KEPT_STORAGES": "nas,nas", "KEPT_STORAGE_NAS_URL": "file:///x"},
		{"KEPT_STORAGES": "9nas", "KEPT_STORAGE_9NAS_URL": "file:///x"},
		{"KEPT_STORAGES": "nas", "KEPT_STORAGE_NAS_URL": "file://relative/x"},
		{"KEPT_STORAGES": "nas", "KEPT_STORAGE_NAS_URL": "file:///data/media"},
		{"KEPT_STORAGES": "nas", "KEPT_STORAGE_NAS_URL": "s3://bucket/kept"}, // no keys
		{"KEPT_STORAGES": "oss", "KEPT_STORAGE_OSS_URL": "s3:///kept", "KEPT_STORAGE_OSS_ACCESS_KEY": "a", "KEPT_STORAGE_OSS_SECRET_KEY": "b"},
		{"KEPT_STORAGES": "oss", "KEPT_STORAGE_OSS_URL": "s3://b?endpoint=http://h/x", "KEPT_STORAGE_OSS_ACCESS_KEY": "a", "KEPT_STORAGE_OSS_SECRET_KEY": "b"},
		{"KEPT_STORAGES": "oss", "KEPT_STORAGE_OSS_URL": "s3://b?path_style=yes", "KEPT_STORAGE_OSS_ACCESS_KEY": "a", "KEPT_STORAGE_OSS_SECRET_KEY": "b"},
		{"KEPT_STORAGES": "nas", "KEPT_STORAGE_NAS_URL": "webdavs:///kept"},
		{"KEPT_STORAGES": "nas", "KEPT_STORAGE_NAS_URL": "ftp://nas/kept"},
	} {
		m["KEPT_DATA_DIR"] = "/data"
		if _, err := Load(env(m)); err == nil {
			t.Errorf("%v accepted", m)
		}
	}
}
