// Package config loads server settings from environment variables.
package config

import (
	"errors"
	"fmt"
	"net/mail"
	"net/netip"
	"net/url"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"time"

	"kept/server/internal/secret"
)

type Config struct {
	DataDir         string        // KEPT_DATA_DIR
	HTTPAddr        string        // KEPT_HTTP_ADDR
	LogLevel        string        // KEPT_LOG_LEVEL: debug | info | warn | error
	LogFormat       string        // KEPT_LOG_FORMAT: text | json
	Workers         int           // KEPT_WORKERS
	ShutdownTimeout time.Duration // KEPT_SHUTDOWN_TIMEOUT
	SessionTTL      time.Duration // KEPT_SESSION_TTL: idle lifetime of a login session
	AuditRetention  time.Duration // KEPT_AUDIT_RETENTION: how long audit events are kept
	MaxUploadSize   int64         // KEPT_MAX_UPLOAD_SIZE: bytes, e.g. 200MB
	DatabaseURL     string        // KEPT_DATABASE_URL: postgres://… to use Postgres; "" is SQLite in the data directory (v1.0)
	DefaultQuota    int64         // KEPT_DEFAULT_QUOTA: bytes a workspace may store unless set otherwise; 0 is no limit (v1.0)
	OCRLanguages    string        // KEPT_OCR_LANGUAGES: tesseract languages, e.g. chi_sim+eng
	OCRMode         string        // KEPT_OCR_MODE: skip | redo | force
	// KEPT_WEBHOOK_ALLOW_PRIVATE (v1.4.3): workflow webhooks may reach the
	// local network (Home Assistant, a NAS); never this machine or a
	// cloud metadata address.
	WebhookAllowPrivate bool
	// KEPT_MAIL_ALLOW_PRIVATE (v1.6.0): mail accounts may use a server on
	// the local network; never this machine or a metadata address.
	MailAllowPrivate bool
	OCRRotate        bool // KEPT_OCR_ROTATE: turn sideways and upside-down pages upright (v1.3.9)
	OCRDeskew        bool // KEPT_OCR_DESKEW: straighten crooked scans (v1.3.9)
	// KEPT_OCR_ROTATE_THRESHOLD: how sure tesseract must be of a page's
	// orientation to turn it. ocrmypdf's 14 leaves most Chinese pages
	// alone (a full page of Chinese scores 10–15); 5 still leaves sparse
	// slips, whose guess is weak, as they are.
	OCRRotateThreshold float64
	SearchLimit        int            // KEPT_SEARCH_LIMIT: most documents a keyword search recalls
	GotenbergURL       string         // KEPT_GOTENBERG_URL: Office conversion service; "" disables Office documents
	WatermarkFont      string         // KEPT_WATERMARK_FONT: a TrueType font with Chinese for share watermarks (v1.9.2); "" looks for one
	SecureCookie       string         // KEPT_SECURE_COOKIE: auto | always | never
	PublicURLs         []string       // KEPT_PUBLIC_URLS: addresses phones try, in order (pairing QR code)
	TrustedProxies     []netip.Prefix // KEPT_TRUSTED_PROXIES: reverse proxies whose X-Forwarded-For is believed
	BackupDir          string         // KEPT_BACKUP_DIR: daily backups go here; "" turns them off
	ExportDir          string         // KEPT_EXPORT_DIR: every workspace is exported here daily, kept up to date (v1.6.5); "" turns it off
	ConsumeDir         string         // KEPT_CONSUME_DIR: files dropped here are ingested; "" turns it off
	Barcodes           bool           // KEPT_BARCODES: read separator sheets and ASN labels in PDFs
	BarcodeSep         string         // KEPT_BARCODE_SEPARATOR: separator sheet code; default PATCHT
	BarcodeASN         string         // KEPT_BARCODE_ASN_PREFIX: ASN label prefix; default ASN
	BarcodeDPI         int            // KEPT_BARCODE_DPI: resolution pages are read at (72–600); default 300 (v1.6.5)
	SMTP               SMTP           // KEPT_SMTP_*: outgoing mail; Host "" turns it off
	AI                 AI             // KEPT_AI_*: the model agents use; URL "" turns AI off
	Books              Books          // KEPT_BOOK_*: looking books up by ISBN
	SSO                SSO            // KEPT_OIDC_*: single sign-on; Issuer "" turns it off
	// KEPT_STORAGES (v1.7.0): more places documents' files can be kept,
	// besides the data directory ("local").
	Storages []Storage
	// KEPT_SECRET_KEY (v1.7.2): the master key sealing the storage keys
	// entered on the page, base64 of 32 bytes; "" uses <data>/secret.key.
	SecretKey string
}

// Storage is a named place for documents' files: KEPT_STORAGES lists the
// names, KEPT_STORAGE_<NAME>_URL says where each is: a directory of this
// machine (file:///mnt/nas/kept, v1.7.0) or a bucket of an S3-compatible
// service (s3://bucket/prefix?endpoint=…, v1.7.1). WebDAV comes in v1.7.2.
type Storage struct {
	Name     string
	Capacity int64  // KEPT_STORAGE_<NAME>_CAPACITY in bytes, 0 no limit (v1.7.10)
	Kind     string // dir | s3 | webdav
	Path     string // dir: an absolute directory
	S3       S3     // s3
	WebDAV   WebDAV // webdav (v1.7.5)
}

// WebDAV is a folder on a WebDAV server (v1.7.5). The password comes from
// KEPT_STORAGE_<NAME>_PASSWORD.
type WebDAV struct {
	URL      string // http(s)://host/folder
	Username string
	Password string
}

// S3 is a bucket of an S3-compatible service. The keys come from
// KEPT_STORAGE_<NAME>_ACCESS_KEY and _SECRET_KEY.
type S3 struct {
	Endpoint  string // host[:port]
	Secure    bool
	Region    string
	Bucket    string
	Prefix    string
	PathStyle bool
	AccessKey string
	SecretKey string
}

// CheckStorageName: letters, digits and -, starting with a letter, at
// most 32; "local" is the data directory.
func CheckStorageName(name string) error {
	ok := name != "" && len(name) <= 32 && name[0] >= 'a' && name[0] <= 'z'
	for _, r := range name {
		ok = ok && (r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-')
	}
	if !ok {
		return errors.New("a name is lowercase letters, digits and -, starting with a letter, at most 32")
	}
	if name == "local" {
		return errors.New("local is the data directory, already there")
	}
	return nil
}

// CheckStorageDir cleans an absolute directory for a storage, which must
// not be inside the data directory.
func CheckStorageDir(dataDir, p string) (string, error) {
	p = filepath.Clean(p)
	if !filepath.IsAbs(p) {
		return "", errors.New("must be an absolute directory, such as /mnt/nas/kept")
	}
	if data, err := filepath.Abs(dataDir); err == nil {
		if rel, err := filepath.Rel(data, p); err == nil && (rel == "." || !strings.HasPrefix(rel, "..")) {
			return "", fmt.Errorf("must not be inside the data directory %s", data)
		}
	}
	return p, nil
}

// ParseEndpoint reads an S3 service address: host[:port], with http://
// for one without TLS; "" is AWS.
func ParseEndpoint(ep string) (host string, secure bool, err error) {
	ep = strings.TrimSpace(ep)
	switch {
	case ep == "":
		return "s3.amazonaws.com", true, nil
	case strings.HasPrefix(ep, "http://"):
		host = strings.TrimPrefix(ep, "http://")
	default:
		host, secure = strings.TrimPrefix(ep, "https://"), true
	}
	host = strings.TrimSuffix(host, "/")
	if host == "" || strings.ContainsAny(host, "/?#@ ") {
		return "", false, fmt.Errorf("endpoint must be host[:port], optionally with http:// or https://, got %q", ep)
	}
	return host, secure, nil
}

// storageEnvPrefix is KEPT_STORAGE_<NAME>_.
func storageEnvPrefix(name string) string {
	return strings.TrimSuffix(StorageEnv(name), "URL")
}

// loadS3 reads s3://bucket[/prefix]?endpoint=host[:port]&region=…&path_style=true.
// The endpoint may carry http:// for a service without TLS (a MinIO on
// the local network); it defaults to AWS.
func loadS3(name string, u *url.URL, getenv func(string) string) (S3, error) {
	env := StorageEnv(name)
	c := S3{Bucket: u.Host, Prefix: strings.Trim(u.Path, "/"), Secure: true, Region: u.Query().Get("region")}
	if c.Bucket == "" {
		return c, fmt.Errorf("%s: say the bucket, such as s3://kept-bucket?endpoint=minio.lan:9000", env)
	}
	var err error
	if c.Endpoint, c.Secure, err = ParseEndpoint(u.Query().Get("endpoint")); err != nil {
		return c, fmt.Errorf("%s: %w", env, err)
	}
	switch v := u.Query().Get("path_style"); v {
	case "", "false":
	case "true":
		c.PathStyle = true
	default:
		return c, fmt.Errorf("%s: path_style must be true or false, got %q", env, v)
	}
	p := storageEnvPrefix(name)
	c.AccessKey, c.SecretKey = strings.TrimSpace(getenv(p+"ACCESS_KEY")), strings.TrimSpace(getenv(p+"SECRET_KEY"))
	if c.AccessKey == "" || c.SecretKey == "" {
		return c, fmt.Errorf("%sACCESS_KEY and %sSECRET_KEY: the keys of storage %s", p, p, name)
	}
	return c, nil
}

// StorageEnv is the variable holding a storage's address.
func StorageEnv(name string) string {
	return "KEPT_STORAGE_" + strings.ToUpper(strings.ReplaceAll(name, "-", "_")) + "_URL"
}

func loadStorages(dataDir string, getenv func(string) string) ([]Storage, []error) {
	var out []Storage
	var errs []error
	seen := map[string]bool{}
	for _, name := range strings.Split(getenv("KEPT_STORAGES"), ",") {
		name = strings.ToLower(strings.TrimSpace(name))
		if name == "" {
			continue
		}
		if err := CheckStorageName(name); err != nil {
			errs = append(errs, fmt.Errorf("KEPT_STORAGES: %q: %w", name, err))
			continue
		}
		switch {
		case seen[name]:
			errs = append(errs, fmt.Errorf("KEPT_STORAGES: %s is listed twice", name))
			continue
		}
		seen[name] = true
		var capacity int64
		if v := strings.TrimSpace(getenv(storageEnvPrefix(name) + "CAPACITY")); v != "" {
			n, err := parseSize(v)
			if err != nil || n < 0 {
				errs = append(errs, fmt.Errorf("%sCAPACITY: must be a size such as 500GB, got %q", storageEnvPrefix(name), v))
				continue
			}
			capacity = n
		}
		env := StorageEnv(name)
		raw := strings.TrimSpace(getenv(env))
		u, err := url.Parse(raw)
		if raw == "" || err != nil {
			errs = append(errs, fmt.Errorf("%s: say where storage %s is, such as file:///mnt/nas/kept", env, name))
			continue
		}
		switch u.Scheme {
		case "file":
			if u.Host != "" {
				errs = append(errs, fmt.Errorf("%s: must be file:// with an absolute directory, such as file:///mnt/nas/kept", env))
				continue
			}
			p, err := CheckStorageDir(dataDir, filepath.FromSlash(u.Path))
			if err != nil {
				errs = append(errs, fmt.Errorf("%s: %w", env, err))
				continue
			}
			out = append(out, Storage{Name: name, Capacity: capacity, Kind: "dir", Path: p})
		case "s3":
			c, err := loadS3(name, u, getenv)
			if err != nil {
				errs = append(errs, err)
				continue
			}
			out = append(out, Storage{Name: name, Capacity: capacity, Kind: "s3", S3: c})
		case "webdav", "webdavs": // webdav:// is http, webdavs:// https
			scheme := "https"
			if u.Scheme == "webdav" {
				scheme = "http"
			}
			if u.Host == "" {
				errs = append(errs, fmt.Errorf("%s: say the server, such as webdavs://dav.jianguoyun.com/dav/kept", env))
				continue
			}
			p := storageEnvPrefix(name)
			out = append(out, Storage{Name: name, Capacity: capacity, Kind: "webdav", WebDAV: WebDAV{URL: scheme + "://" + u.Host + u.EscapedPath(),
				Username: strings.TrimSpace(getenv(p + "USERNAME")), Password: getenv(p + "PASSWORD")}})
		default:
			errs = append(errs, fmt.Errorf("%s: unknown kind %q; use file:///a/directory, s3://bucket?endpoint=host or webdavs://host/folder", env, u.Scheme))
		}
	}
	return out, errs
}

// SSO is single sign-on with an OpenID Connect provider (v1.0).
type SSO struct {
	Issuer       string   // KEPT_OIDC_ISSUER
	ClientID     string   // KEPT_OIDC_CLIENT_ID
	ClientSecret string   // KEPT_OIDC_CLIENT_SECRET
	Name         string   // KEPT_OIDC_NAME: the button's name, default 单点登录
	Scopes       []string // KEPT_OIDC_SCOPES: default openid email profile
	RedirectURL  string   // KEPT_OIDC_REDIRECT_URL: default the first public URL + /api/v1/auth/sso/callback
	TrustEmail   bool     // KEPT_OIDC_TRUST_EMAIL: trust the email when the provider does not say it is verified
}

// Books is where book data is looked up by ISBN (v0.4).
type Books struct {
	Sources   []string // KEPT_BOOK_SOURCES: in order; empty turns lookups off
	LookupURL string   // KEPT_BOOK_LOOKUP_URL: the deployer's endpoint, {isbn} replaced
	GoogleKey string   // KEPT_GOOGLE_BOOKS_KEY: optional API key
}

// SMTP is the outgoing mail server (v0.3: emailing documents, reminder
// emails).
type SMTP struct {
	Host           string // KEPT_SMTP_HOST
	Port           int    // KEPT_SMTP_PORT: default 587, or 465 with tls
	Security       string // KEPT_SMTP_SECURITY: starttls | tls | none
	Username       string // KEPT_SMTP_USERNAME: "" sends without logging in
	Password       string // KEPT_SMTP_PASSWORD
	From           string // KEPT_SMTP_FROM: e.g. 收好 <kept@example.com>
	MaxAttachments int64  // KEPT_SMTP_MAX_ATTACHMENTS: bytes per message, default 20MB
}

// AI is the model the agents call (v0.3): an OpenAI-compatible API, local
// (Ollama) or in the cloud.
type AI struct {
	URL     string        // KEPT_AI_URL: base URL ending before /chat/completions, e.g. http://ollama:11434/v1
	Key     string        // KEPT_AI_KEY: bearer token; "" for none
	Model   string        // KEPT_AI_MODEL: the model for the intake agent
	Timeout time.Duration // KEPT_AI_TIMEOUT: one model call; default 60s
	// Embeddings (v0.5): EmbedModel "" turns vectors off. URL and key
	// default to the chat model's.
	EmbedModel string // KEPT_AI_EMBED_MODEL, e.g. bge-m3
	EmbedURL   string // KEPT_AI_EMBED_URL
	EmbedKey   string // KEPT_AI_EMBED_KEY
	// VisionModel (v1.9.0) reads photos of lab reports on the chat model's
	// service; "" for none.
	VisionModel string // KEPT_AI_VISION_MODEL, e.g. qwen2.5vl
}

// DBPath is the SQLite database file inside the data directory.
func (c Config) DBPath() string { return filepath.Join(c.DataDir, "app.db") }

// DatabaseLabel names the database for logs and the system page, without
// the password: postgres://host:port/name, or the SQLite file.
func (c Config) DatabaseLabel() string {
	if c.DatabaseURL == "" {
		return c.DBPath()
	}
	u, err := url.Parse(c.DatabaseURL)
	if err != nil {
		return "postgres"
	}
	return "postgres://" + u.Host + u.Path
}

// Database is what store.Open opens: the Postgres URL, or the SQLite file.
func (c Config) Database() string {
	if c.DatabaseURL != "" {
		return c.DatabaseURL
	}
	return c.DBPath()
}

// IndexDir holds the full-text index; it can be deleted and rebuilt.
func (c Config) IndexDir() string { return filepath.Join(c.DataDir, "index") }

// BrandingDir holds the instance logo (design doc 11.4).
func (c Config) BrandingDir() string { return filepath.Join(c.DataDir, "branding") }

// Dirs lists the data directories the server creates on start-up
// (layout from the design doc, section 2.5).
func (c Config) Dirs() []string {
	return []string{
		c.DataDir,
		c.IndexDir(),
		filepath.Join(c.DataDir, "tmp"),
		filepath.Join(c.DataDir, "media", "originals"),
		filepath.Join(c.DataDir, "media", "archive"),
		filepath.Join(c.DataDir, "media", "thumbnails"),
		filepath.Join(c.DataDir, "media", "versions"),
		c.BrandingDir(),
	}
}

func defaults() Config {
	return Config{
		DataDir:   "data",
		HTTPAddr:  ":8000",
		LogLevel:  "info",
		LogFormat: "text",
		// Half the CPUs (OCR is CPU-bound), capped so a big host does not
		// open dozens of SQLite writers.
		Workers:             min(8, max(1, runtime.NumCPU()/2)),
		ShutdownTimeout:     30 * time.Second,
		SessionTTL:          30 * 24 * time.Hour,
		AuditRetention:      365 * 24 * time.Hour,
		MaxUploadSize:       200 << 20,
		OCRLanguages:        "chi_sim+eng",
		OCRMode:             "skip",
		OCRRotate:           true,
		WebhookAllowPrivate: true,
		MailAllowPrivate:    true,
		OCRDeskew:           true,
		OCRRotateThreshold:  5,
		SearchLimit:         10000,
		SecureCookie:        "auto",
	}
}

// Load reads the configuration using getenv (os.Getenv in production).
func Load(getenv func(string) string) (Config, error) {
	c := defaults()
	var errs []error

	if v := getenv("KEPT_DATA_DIR"); v != "" {
		c.DataDir = v
	}
	if v := getenv("KEPT_HTTP_ADDR"); v != "" {
		c.HTTPAddr = v
	}
	if v := getenv("KEPT_LOG_LEVEL"); v != "" {
		c.LogLevel = strings.ToLower(v)
	}
	switch c.LogLevel {
	case "debug", "info", "warn", "error":
	default:
		errs = append(errs, fmt.Errorf("KEPT_LOG_LEVEL: unknown level %q", c.LogLevel))
	}
	if v := getenv("KEPT_LOG_FORMAT"); v != "" {
		c.LogFormat = strings.ToLower(v)
	}
	if c.LogFormat != "text" && c.LogFormat != "json" {
		errs = append(errs, fmt.Errorf("KEPT_LOG_FORMAT: must be text or json, got %q", c.LogFormat))
	}
	if v := getenv("KEPT_WORKERS"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 {
			errs = append(errs, fmt.Errorf("KEPT_WORKERS: must be a positive integer, got %q", v))
		} else {
			c.Workers = n
		}
	}
	if v := getenv("KEPT_SHUTDOWN_TIMEOUT"); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil || d <= 0 {
			errs = append(errs, fmt.Errorf("KEPT_SHUTDOWN_TIMEOUT: must be a positive duration such as 30s, got %q", v))
		} else {
			c.ShutdownTimeout = d
		}
	}
	if v := getenv("KEPT_SESSION_TTL"); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil || d <= 0 {
			errs = append(errs, fmt.Errorf("KEPT_SESSION_TTL: must be a positive duration such as 720h, got %q", v))
		} else {
			c.SessionTTL = d
		}
	}
	if v := getenv("KEPT_AUDIT_RETENTION"); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil || d <= 0 {
			errs = append(errs, fmt.Errorf("KEPT_AUDIT_RETENTION: must be a positive duration such as 8760h, got %q", v))
		} else {
			c.AuditRetention = d
		}
	}
	if v := strings.TrimSpace(getenv("KEPT_DATABASE_URL")); v != "" {
		if !strings.HasPrefix(v, "postgres://") && !strings.HasPrefix(v, "postgresql://") {
			errs = append(errs, errors.New("KEPT_DATABASE_URL: must be a Postgres address such as postgres://kept:secret@db:5432/kept; leave it out to use SQLite"))
		} else {
			c.DatabaseURL = v
		}
	}
	if v := strings.TrimSpace(getenv("KEPT_DEFAULT_QUOTA")); v != "" {
		n, err := parseSize(v)
		if err != nil || n < 0 {
			errs = append(errs, fmt.Errorf("KEPT_DEFAULT_QUOTA: must be a size such as 20GB, or 0 for no limit, got %q", v))
		} else {
			c.DefaultQuota = n
		}
	}
	if v := getenv("KEPT_MAX_UPLOAD_SIZE"); v != "" {
		n, err := parseSize(v)
		if err != nil || n <= 0 {
			errs = append(errs, fmt.Errorf("KEPT_MAX_UPLOAD_SIZE: must be a size such as 200MB, got %q", v))
		} else {
			c.MaxUploadSize = n
		}
	}
	if v := getenv("KEPT_OCR_LANGUAGES"); v != "" {
		c.OCRLanguages = v
	}
	if v := getenv("KEPT_OCR_MODE"); v != "" {
		c.OCRMode = strings.ToLower(v)
	}
	switch c.OCRMode {
	case "skip", "redo", "force":
	default:
		errs = append(errs, fmt.Errorf("KEPT_OCR_MODE: must be skip, redo or force, got %q", c.OCRMode))
	}
	for _, o := range []struct {
		key string
		to  *bool
	}{{"KEPT_OCR_ROTATE", &c.OCRRotate}, {"KEPT_OCR_DESKEW", &c.OCRDeskew}, {"KEPT_WEBHOOK_ALLOW_PRIVATE", &c.WebhookAllowPrivate}, {"KEPT_MAIL_ALLOW_PRIVATE", &c.MailAllowPrivate}} {
		if v := strings.TrimSpace(getenv(o.key)); v != "" {
			b, err := strconv.ParseBool(v)
			if err != nil {
				errs = append(errs, fmt.Errorf("%s: must be true or false, got %q", o.key, v))
			}
			*o.to = b
		}
	}
	if v := strings.TrimSpace(getenv("KEPT_OCR_ROTATE_THRESHOLD")); v != "" {
		f, err := strconv.ParseFloat(v, 64)
		if err != nil || f < 0 || f > 100 {
			errs = append(errs, fmt.Errorf("KEPT_OCR_ROTATE_THRESHOLD: must be a number from 0 to 100, got %q", v))
		} else {
			c.OCRRotateThreshold = f
		}
	}
	if v := getenv("KEPT_SEARCH_LIMIT"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 100 || n > 1000000 {
			errs = append(errs, fmt.Errorf("KEPT_SEARCH_LIMIT: must be an integer from 100 to 1000000, got %q", v))
		} else {
			c.SearchLimit = n
		}
	}
	if v := strings.TrimRight(strings.TrimSpace(getenv("KEPT_GOTENBERG_URL")), "/"); v != "" {
		if u, err := url.Parse(v); err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			errs = append(errs, fmt.Errorf("KEPT_GOTENBERG_URL: must be an http(s) address such as http://gotenberg:3000, got %q", v))
		} else {
			c.GotenbergURL = v
		}
	}
	c.WatermarkFont = strings.TrimSpace(getenv("KEPT_WATERMARK_FONT"))
	for _, v := range strings.Split(getenv("KEPT_PUBLIC_URLS"), ",") {
		v = strings.TrimRight(strings.TrimSpace(v), "/")
		if v == "" {
			continue
		}
		if u, err := url.Parse(v); err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			errs = append(errs, fmt.Errorf("KEPT_PUBLIC_URLS: each address must be http(s), such as https://docs.example.com, got %q", v))
		} else {
			c.PublicURLs = append(c.PublicURLs, v)
		}
	}
	for _, v := range strings.Split(getenv("KEPT_TRUSTED_PROXIES"), ",") {
		v = strings.TrimSpace(v)
		if v == "" {
			continue
		}
		if p, err := netip.ParsePrefix(v); err == nil {
			c.TrustedProxies = append(c.TrustedProxies, p.Masked())
		} else if a, err := netip.ParseAddr(v); err == nil {
			c.TrustedProxies = append(c.TrustedProxies, netip.PrefixFrom(a.Unmap(), a.Unmap().BitLen()))
		} else {
			errs = append(errs, fmt.Errorf("KEPT_TRUSTED_PROXIES: each entry must be an IP address or a CIDR range such as 172.16.0.0/12, got %q", v))
		}
	}
	if v := strings.TrimSpace(getenv("KEPT_BARCODES")); v != "" {
		b, err := strconv.ParseBool(v)
		if err != nil {
			errs = append(errs, fmt.Errorf("KEPT_BARCODES: must be true or false, got %q", v))
		}
		c.Barcodes = b
	}
	c.BarcodeSep = strings.TrimSpace(getenv("KEPT_BARCODE_SEPARATOR"))
	c.BarcodeASN = strings.TrimSpace(getenv("KEPT_BARCODE_ASN_PREFIX"))
	if v := strings.TrimSpace(getenv("KEPT_BARCODE_DPI")); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 72 || n > 600 {
			errs = append(errs, fmt.Errorf("KEPT_BARCODE_DPI: must be a whole number from 72 to 600, got %q", v))
		} else {
			c.BarcodeDPI = n
		}
	}
	if v := strings.TrimSpace(getenv("KEPT_CONSUME_DIR")); v != "" {
		if !filepath.IsAbs(v) {
			errs = append(errs, fmt.Errorf("KEPT_CONSUME_DIR: must be an absolute path such as /consume, got %q", v))
		} else {
			c.ConsumeDir = filepath.Clean(v)
		}
	}
	if v := strings.TrimSpace(getenv("KEPT_BACKUP_DIR")); v != "" {
		if !filepath.IsAbs(v) {
			errs = append(errs, fmt.Errorf("KEPT_BACKUP_DIR: must be an absolute path such as /backups, got %q", v))
		} else {
			c.BackupDir = filepath.Clean(v)
		}
	}
	if v := strings.TrimSpace(getenv("KEPT_EXPORT_DIR")); v != "" {
		if !filepath.IsAbs(v) {
			errs = append(errs, fmt.Errorf("KEPT_EXPORT_DIR: must be an absolute path such as /exports, got %q", v))
		} else {
			c.ExportDir = filepath.Clean(v)
		}
	}
	if v := strings.TrimSpace(getenv("KEPT_SECRET_KEY")); v != "" {
		if _, err := secret.ParseKey(v); err != nil {
			errs = append(errs, fmt.Errorf("KEPT_SECRET_KEY: %w", err))
		} else {
			c.SecretKey = v
		}
	}
	storages, serrs := loadStorages(c.DataDir, getenv)
	c.Storages = storages
	errs = append(errs, serrs...)
	errs = append(errs, loadSMTP(&c.SMTP, getenv)...)
	errs = append(errs, loadAI(&c.AI, getenv)...)
	errs = append(errs, loadBooks(&c.Books, getenv)...)
	errs = append(errs, loadSSO(&c.SSO, c.PublicURLs, getenv)...)
	if v := getenv("KEPT_SECURE_COOKIE"); v != "" {
		c.SecureCookie = strings.ToLower(v)
	}
	switch c.SecureCookie {
	case "auto", "always", "never":
	default:
		errs = append(errs, fmt.Errorf("KEPT_SECURE_COOKIE: must be auto, always or never, got %q", c.SecureCookie))
	}
	return c, errors.Join(errs...)
}

// parseSize reads "200MB", "1GB", "512KB" or a plain number of bytes.
func parseSize(v string) (int64, error) {
	v = strings.ToUpper(strings.TrimSpace(v))
	mult := int64(1)
	for _, u := range []struct {
		suffix string
		mult   int64
	}{{"GB", 1 << 30}, {"MB", 1 << 20}, {"KB", 1 << 10}, {"B", 1}} {
		if strings.HasSuffix(v, u.suffix) {
			v, mult = strings.TrimSpace(strings.TrimSuffix(v, u.suffix)), u.mult
			break
		}
	}
	n, err := strconv.ParseInt(v, 10, 64)
	return n * mult, err
}

func loadSMTP(m *SMTP, getenv func(string) string) []error {
	var errs []error
	m.MaxAttachments = 20 << 20
	if v := getenv("KEPT_SMTP_MAX_ATTACHMENTS"); v != "" {
		n, err := parseSize(v)
		if err != nil || n <= 0 {
			errs = append(errs, fmt.Errorf("KEPT_SMTP_MAX_ATTACHMENTS: must be a size such as 20MB, got %q", v))
		} else {
			m.MaxAttachments = n
		}
	}
	m.Host = strings.TrimSpace(getenv("KEPT_SMTP_HOST"))
	if m.Host == "" {
		return errs
	}
	m.Security = strings.ToLower(strings.TrimSpace(getenv("KEPT_SMTP_SECURITY")))
	if v := strings.TrimSpace(getenv("KEPT_SMTP_PORT")); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n <= 0 || n > 65535 {
			errs = append(errs, fmt.Errorf("KEPT_SMTP_PORT: must be a port number, got %q", v))
		}
		m.Port = n
	}
	if m.Security == "" {
		m.Security = "starttls"
		if m.Port == 465 {
			m.Security = "tls"
		}
	}
	switch m.Security {
	case "starttls", "tls", "none":
	default:
		errs = append(errs, fmt.Errorf("KEPT_SMTP_SECURITY: must be starttls, tls or none, got %q", m.Security))
	}
	if m.Port == 0 {
		m.Port = 587
		if m.Security == "tls" {
			m.Port = 465
		}
	}
	m.Username = getenv("KEPT_SMTP_USERNAME")
	m.Password = getenv("KEPT_SMTP_PASSWORD")
	m.From = strings.TrimSpace(getenv("KEPT_SMTP_FROM"))
	if m.From == "" {
		errs = append(errs, errors.New("KEPT_SMTP_FROM: required when KEPT_SMTP_HOST is set, e.g. 收好 <kept@example.com>"))
	} else if _, err := mail.ParseAddress(m.From); err != nil {
		errs = append(errs, fmt.Errorf("KEPT_SMTP_FROM: not an email address: %q", m.From))
	}
	return errs
}

func loadBooks(b *Books, getenv func(string) string) []error {
	var errs []error
	b.LookupURL = strings.TrimSpace(getenv("KEPT_BOOK_LOOKUP_URL"))
	b.GoogleKey = strings.TrimSpace(getenv("KEPT_GOOGLE_BOOKS_KEY"))
	if b.LookupURL != "" {
		if u, err := url.Parse(strings.ReplaceAll(b.LookupURL, "{isbn}", "9780306406157")); err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || !strings.Contains(b.LookupURL, "{isbn}") {
			errs = append(errs, fmt.Errorf("KEPT_BOOK_LOOKUP_URL: must be an http(s) address containing {isbn}, such as http://library.lan/isbn/{isbn}, got %q", b.LookupURL))
		}
	}
	v := strings.ToLower(strings.TrimSpace(getenv("KEPT_BOOK_SOURCES")))
	switch {
	case v == "none":
		return errs
	case v == "" && b.LookupURL != "":
		b.Sources = []string{"custom", "openlibrary", "googlebooks"}
		return errs
	case v == "":
		b.Sources = []string{"openlibrary", "googlebooks"}
		return errs
	}
	for _, name := range strings.Split(v, ",") {
		name = strings.TrimSpace(name)
		switch name {
		case "":
		case "openlibrary", "googlebooks":
			b.Sources = append(b.Sources, name)
		case "custom":
			if b.LookupURL == "" {
				errs = append(errs, errors.New("KEPT_BOOK_SOURCES: custom needs KEPT_BOOK_LOOKUP_URL"))
			}
			b.Sources = append(b.Sources, name)
		default:
			errs = append(errs, fmt.Errorf("KEPT_BOOK_SOURCES: unknown source %q; use openlibrary, googlebooks, custom or none", name))
		}
	}
	return errs
}

func loadAI(a *AI, getenv func(string) string) []error {
	var errs []error
	a.Timeout = 60 * time.Second
	if v := strings.TrimSpace(getenv("KEPT_AI_TIMEOUT")); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil || d <= 0 {
			errs = append(errs, fmt.Errorf("KEPT_AI_TIMEOUT: must be a positive duration such as 60s, got %q", v))
		} else {
			a.Timeout = d
		}
	}
	a.URL = strings.TrimRight(strings.TrimSpace(getenv("KEPT_AI_URL")), "/")
	a.EmbedModel = strings.TrimSpace(getenv("KEPT_AI_EMBED_MODEL"))
	a.VisionModel = strings.TrimSpace(getenv("KEPT_AI_VISION_MODEL"))
	a.EmbedURL = strings.TrimRight(strings.TrimSpace(getenv("KEPT_AI_EMBED_URL")), "/")
	a.EmbedKey = strings.TrimSpace(getenv("KEPT_AI_EMBED_KEY"))
	if a.EmbedURL == "" {
		a.EmbedURL = a.URL
	}
	if a.EmbedKey == "" {
		a.EmbedKey = strings.TrimSpace(getenv("KEPT_AI_KEY"))
	}
	if a.EmbedModel != "" {
		if a.EmbedURL == "" {
			errs = append(errs, errors.New("KEPT_AI_EMBED_MODEL: needs KEPT_AI_URL or KEPT_AI_EMBED_URL"))
		} else if u, err := url.Parse(a.EmbedURL); err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			errs = append(errs, fmt.Errorf("KEPT_AI_EMBED_URL: must be an http(s) address such as http://ollama:11434/v1, got %q", a.EmbedURL))
		}
	}
	if a.URL == "" {
		return errs
	}
	if u, err := url.Parse(a.URL); err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		errs = append(errs, fmt.Errorf("KEPT_AI_URL: must be an http(s) address such as http://ollama:11434/v1, got %q", a.URL))
	}
	a.Key = strings.TrimSpace(getenv("KEPT_AI_KEY"))
	a.Model = strings.TrimSpace(getenv("KEPT_AI_MODEL"))
	if a.Model == "" {
		errs = append(errs, errors.New("KEPT_AI_MODEL: required when KEPT_AI_URL is set, e.g. qwen2.5:7b"))
	}
	return errs
}

// SSOCallbackPath is where the provider sends people back.
const SSOCallbackPath = "/api/v1/auth/sso/callback"

func loadSSO(o *SSO, public []string, getenv func(string) string) []error {
	o.Issuer = strings.TrimRight(strings.TrimSpace(getenv("KEPT_OIDC_ISSUER")), "/")
	if o.Issuer == "" {
		return nil
	}
	var errs []error
	if u, err := url.Parse(o.Issuer); err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		errs = append(errs, fmt.Errorf("KEPT_OIDC_ISSUER: must be an http(s) address such as https://id.example.com/realms/home, got %q", o.Issuer))
	}
	o.ClientID = strings.TrimSpace(getenv("KEPT_OIDC_CLIENT_ID"))
	o.ClientSecret = strings.TrimSpace(getenv("KEPT_OIDC_CLIENT_SECRET"))
	if o.ClientID == "" {
		errs = append(errs, errors.New("KEPT_OIDC_CLIENT_ID: required with KEPT_OIDC_ISSUER"))
	}
	o.Name = strings.TrimSpace(getenv("KEPT_OIDC_NAME"))
	if o.Name == "" {
		o.Name = "单点登录"
	}
	o.Scopes = strings.Fields(strings.ReplaceAll(getenv("KEPT_OIDC_SCOPES"), ",", " "))
	if len(o.Scopes) == 0 {
		o.Scopes = []string{"openid", "email", "profile"}
	}
	if !slices.Contains(o.Scopes, "openid") {
		o.Scopes = append([]string{"openid"}, o.Scopes...)
	}
	o.RedirectURL = strings.TrimSpace(getenv("KEPT_OIDC_REDIRECT_URL"))
	if o.RedirectURL == "" && len(public) > 0 {
		o.RedirectURL = public[0] + SSOCallbackPath
	}
	if o.RedirectURL == "" {
		errs = append(errs, errors.New("KEPT_OIDC_REDIRECT_URL: set it, or KEPT_PUBLIC_URLS, so the provider knows where to send people back"))
	} else if u, err := url.Parse(o.RedirectURL); err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		errs = append(errs, fmt.Errorf("KEPT_OIDC_REDIRECT_URL: must be an http(s) address, got %q", o.RedirectURL))
	}
	if v := strings.TrimSpace(getenv("KEPT_OIDC_TRUST_EMAIL")); v != "" {
		b, err := strconv.ParseBool(v)
		if err != nil {
			errs = append(errs, fmt.Errorf("KEPT_OIDC_TRUST_EMAIL: must be true or false, got %q", v))
		}
		o.TrustEmail = b
	}
	return errs
}
