// Package system reports the instance's state for administrators (design
// doc 6.1 `system`, 11.12): version, storage, the search index, the job
// queue, external tools and the latest integrity checks.
package system

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"kept/server/internal/backup"
	"kept/server/internal/i18n"
	"kept/server/internal/integrity"
	"kept/server/internal/search"
	"kept/server/internal/store"
	"kept/server/internal/version"
	"kept/server/internal/watch"
	"kept/server/internal/watermark"
)

type Options struct {
	DataDir      string
	DBPath       string
	IndexDir     string
	GotenbergURL string
	Index        *search.Index
	IndexReady   func() error // search.Indexer.Ready
	StartedAt    time.Time
	Settings     map[string]string   // configuration worth showing (workers, OCR, …)
	BackupDir    *backup.Dir         // KEPT_BACKUP_DIR or the page's choice (v1.13.9); none when daily backups are off
	Watch        func() watch.Status // the consume directory; nil when off
	// MinFree is the free space below which the health check reports the
	// disk (DefaultMinFree when 0).
	MinFree int64
}

// DefaultMinFree: less free space than this on the data directory's disk
// makes the health check report degraded.
const DefaultMinFree = 1 << 30

// Disk reports the free space on the data directory's disk and whether it
// is below MinFree; ok is false when it cannot be told.
func (s *Service) Disk() (free int64, low, ok bool) {
	free, ok = freeSpace(s.opt.DataDir)
	min := s.opt.MinFree
	if min == 0 {
		min = DefaultMinFree
	}
	return free, ok && free < min, ok
}

type Service struct {
	db  *sql.DB
	opt Options

	toolsOnce sync.Once
	tools     []Tool

	sizeMu sync.Mutex
	sizeAt time.Time
	sizes  map[string]int64
}

func NewService(db *sql.DB, opt Options) *Service { return &Service{db: db, opt: opt} }

// Tool is an external program the parser needs (or Gotenberg).
type Tool struct {
	Name      string
	Available bool
	Version   string
	Purpose   string
}

// Storage sizes in bytes; Free is -1 when unknown.
type Storage struct {
	Database, Index, Media, Backups, Free int64
}

type Integrity struct {
	WorkspaceID   int64
	WorkspaceName string
	Report        integrity.Report
}

// Backup is the daily backup's state: its directory and latest run.
type Backup struct {
	Dir        string
	DirFromEnv bool // KEPT_BACKUP_DIR decides; the page cannot change it (v1.13.9)
	Snapshots  int
	Last       *BackupRun
}

// BackupRun is the latest system.backup job.
type BackupRun struct {
	Status string
	At     time.Time
	Result backup.Result
	Error  string
}

// Status is everything the system page shows.
type Status struct {
	Version, Commit, BuildTime, GoVersion string
	StartedAt                             time.Time
	Storage                               Storage
	Users, Workspaces, Documents, Trash   int
	IndexDocuments                        int
	IndexBehind                           int
	IndexReady                            bool
	JobsQueued, JobsRunning, JobsFailed   int // failed: in the last 24 hours
	OldestQueued                          *time.Time
	Tools                                 []Tool
	Integrity                             []Integrity
	Backup                                Backup
	Watch                                 *watch.Status
	Settings                              map[string]string
}

var probes = []struct {
	name, purpose string
	args          []string
	optional      bool
}{
	{"ocrmypdf", i18n.N("扫描件文字识别"), []string{"--version"}, false},
	{"tesseract", i18n.N("OCR 引擎"), []string{"--version"}, false},
	{"pdftotext", i18n.N("读取 PDF 文字层"), []string{"-v"}, false},
	{"pdftoppm", i18n.N("生成缩略图"), []string{"-v"}, false},
	{"pdfinfo", i18n.N("读取 PDF 信息"), []string{"-v"}, false},
	{"gs", i18n.N("Ghostscript（OCR 依赖）"), []string{"--version"}, false},
	{"qpdf", i18n.N("PDF 处理（OCR 依赖）"), []string{"--version"}, false},
	{"heif-dec", i18n.N("HEIC 照片解码（可选，没有时不收 HEIC）"), []string{"--version"}, true},
}

// ExternalTool is a program on PATH that document processing needs.
type ExternalTool struct {
	Name, Purpose string
	Optional      bool
}

// ExternalTools lists them (kept doctor checks the same list).
func ExternalTools() []ExternalTool {
	out := make([]ExternalTool, len(probes))
	for i, p := range probes {
		out[i] = ExternalTool{p.name, p.purpose, p.optional}
	}
	return out
}

// probeTools runs each tool's version flag once per process.
func (s *Service) probeTools(ctx context.Context) []Tool {
	s.toolsOnce.Do(func() {
		for _, p := range probes {
			t := Tool{Name: p.name, Purpose: p.purpose}
			if path, err := exec.LookPath(p.name); err == nil {
				t.Available = true
				cctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
				out, _ := exec.CommandContext(cctx, path, p.args...).CombinedOutput()
				cancel()
				for _, line := range strings.Split(string(out), "\n") {
					if line = strings.TrimSpace(line); line != "" {
						t.Version = line
						break
					}
				}
			}
			s.tools = append(s.tools, t)
		}
	})
	return append([]Tool{}, s.tools...)
}

// watermarkFont is the font share watermarks are drawn with (v1.9.2).
func watermarkFont() Tool {
	t := Tool{Name: i18n.N("水印字体"), Purpose: i18n.N("分享加水印（可选，KEPT_WATERMARK_FONT）")}
	if p := watermark.Font(); p != "" {
		t.Available, t.Version = true, p
	} else {
		t.Version = i18n.N("没有找到中文字体，分享不能加水印")
	}
	return t
}

// gotenberg checks the Office conversion service now (2 s timeout).
func (s *Service) gotenberg(ctx context.Context) Tool {
	t := Tool{Name: "Gotenberg", Purpose: i18n.N("Office 文档转 PDF")}
	if s.opt.GotenbergURL == "" {
		t.Version = i18n.N("未配置（KEPT_GOTENBERG_URL）")
		return t
	}
	cctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(cctx, http.MethodGet, s.opt.GotenbergURL+"/health", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Version = fmt.Sprintf(i18n.N("无法连接 %s"), s.opt.GotenbergURL)
		return t
	}
	resp.Body.Close()
	t.Available = resp.StatusCode == http.StatusOK
	t.Version = s.opt.GotenbergURL
	return t
}

func dirSize(dir string) int64 {
	var n int64
	_ = filepath.WalkDir(dir, func(_ string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			if info, err := d.Info(); err == nil {
				n += info.Size()
			}
		}
		return nil
	})
	return n
}

// storage measures the data directory, at most once a minute (walking
// media/ can take a while).
func (s *Service) storage() Storage {
	s.sizeMu.Lock()
	defer s.sizeMu.Unlock()
	if s.sizes == nil || time.Since(s.sizeAt) > time.Minute {
		db := int64(0)
		for _, suffix := range []string{"", "-wal", "-shm"} {
			if st, err := os.Stat(s.opt.DBPath + suffix); err == nil {
				db += st.Size()
			}
		}
		if store.IsPostgres(s.db) {
			_ = s.db.QueryRow(`SELECT pg_database_size(current_database())`).Scan(&db)
		}
		s.sizes = map[string]int64{
			"db": db, "index": dirSize(s.opt.IndexDir), "media": dirSize(filepath.Join(s.opt.DataDir, "media")),
			"backups": dirSize(filepath.Join(filepath.Dir(s.opt.DBPath), "backups")),
		}
		s.sizeAt = time.Now()
	}
	st := Storage{Database: s.sizes["db"], Index: s.sizes["index"], Media: s.sizes["media"], Backups: s.sizes["backups"], Free: -1}
	if free, ok := freeSpace(s.opt.DataDir); ok {
		st.Free = free
	}
	return st
}

func (s *Service) Status(ctx context.Context) (Status, error) {
	st := Status{Version: version.Version, Commit: version.Commit, BuildTime: version.BuildTime, GoVersion: runtime.Version(),
		StartedAt: s.opt.StartedAt, Storage: s.storage(), Settings: s.opt.Settings, Integrity: []Integrity{}}
	counts := []struct {
		dst *int
		q   string
	}{
		{&st.Users, `SELECT count(*) FROM users WHERE disabled_at IS NULL`},
		{&st.Workspaces, `SELECT count(*) FROM workspaces WHERE deleted_at IS NULL`},
		{&st.Documents, `SELECT count(*) FROM documents WHERE deleted_at IS NULL`},
		{&st.Trash, `SELECT count(*) FROM documents WHERE deleted_at IS NOT NULL`},
		{&st.IndexBehind, `SELECT count(*) FROM documents WHERE indexed_rev < index_rev`},
		{&st.JobsQueued, `SELECT count(*) FROM jobs WHERE status = 'queued'`},
		{&st.JobsRunning, `SELECT count(*) FROM jobs WHERE status = 'running'`},
	}
	for _, c := range counts {
		if err := s.db.QueryRowContext(ctx, c.q).Scan(c.dst); err != nil {
			return st, err
		}
	}
	since := time.Now().Add(-24 * time.Hour).UnixMilli()
	if err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM jobs WHERE status = 'failed' AND finished_at >= ?`, since).Scan(&st.JobsFailed); err != nil {
		return st, err
	}
	var oldest sql.NullInt64
	if err := s.db.QueryRowContext(ctx, `SELECT min(created_at) FROM jobs WHERE status = 'queued'`).Scan(&oldest); err != nil {
		return st, err
	}
	if oldest.Valid {
		t := time.UnixMilli(oldest.Int64)
		st.OldestQueued = &t
	}
	if s.opt.Index != nil {
		if n, err := s.opt.Index.Count(); err == nil {
			st.IndexDocuments = int(n)
		}
	}
	st.IndexReady = s.opt.IndexReady == nil || s.opt.IndexReady() == nil
	st.Tools = append(s.probeTools(ctx), s.gotenberg(ctx), watermarkFont())

	if s.opt.Watch != nil {
		w := s.opt.Watch()
		st.Watch = &w
	}
	if err := s.backupStatus(ctx, &st.Backup); err != nil {
		return st, err
	}

	rows, err := s.db.QueryContext(ctx, `SELECT j.workspace_id, w.name, j.result FROM jobs AS j JOIN workspaces AS w ON w.id = j.workspace_id
		WHERE j.id IN (SELECT max(id) FROM jobs WHERE kind = ? AND status = 'succeeded' GROUP BY workspace_id) AND w.deleted_at IS NULL
		ORDER BY w.id`, integrity.Kind)
	if err != nil {
		return st, err
	}
	defer rows.Close()
	for rows.Next() {
		var it Integrity
		var result sql.NullString
		if err := rows.Scan(&it.WorkspaceID, &it.WorkspaceName, &result); err != nil {
			return st, err
		}
		_ = json.Unmarshal([]byte(result.String), &it.Report)
		st.Integrity = append(st.Integrity, it)
	}
	return st, rows.Err()
}

func (s *Service) backupStatus(ctx context.Context, b *Backup) error {
	b.Dir = s.opt.BackupDir.Get(ctx)
	b.DirFromEnv = s.opt.BackupDir.FromEnv()
	if b.Dir != "" {
		snaps, _ := backup.Snapshots(b.Dir)
		b.Snapshots = len(snaps)
	}
	var run BackupRun
	var result, errMsg sql.NullString
	var at int64
	err := s.db.QueryRowContext(ctx, `SELECT status, result, error, coalesce(finished_at, created_at) FROM jobs
		WHERE kind = ? ORDER BY id DESC LIMIT 1`, backup.Kind).Scan(&run.Status, &result, &errMsg, &at)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	run.At = time.UnixMilli(at)
	if run.Status == "succeeded" {
		_ = json.Unmarshal([]byte(result.String), &run.Result)
	}
	if run.Status == "failed" {
		run.Error = errMsg.String
	}
	b.Last = &run
	return nil
}
