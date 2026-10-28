// Package app wires the server together and runs it until shutdown.
package app

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"kept/server/internal/files"
	"kept/server/internal/i18n"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"

	"golang.org/x/sync/errgroup"

	"kept/server/internal/api"
	"kept/server/internal/audit"
	"kept/server/internal/auth"
	"kept/server/internal/backup"
	"kept/server/internal/barcode"
	"kept/server/internal/booklists"
	"kept/server/internal/bookmeta"
	"kept/server/internal/books"
	"kept/server/internal/branding"
	"kept/server/internal/checklist"
	"kept/server/internal/classify"
	"kept/server/internal/config"
	"kept/server/internal/dedupe"
	"kept/server/internal/document"
	"kept/server/internal/drawer"
	"kept/server/internal/edition"
	"kept/server/internal/events"
	"kept/server/internal/export"
	"kept/server/internal/ingest"
	"kept/server/internal/integrity"
	"kept/server/internal/jobs"
	"kept/server/internal/netguard"
	"kept/server/internal/notes"
	"kept/server/internal/notify"
	"kept/server/internal/pages"
	"kept/server/internal/parser"
	"kept/server/internal/privacy"
	"kept/server/internal/quota"
	"kept/server/internal/reading"
	"kept/server/internal/search"
	"kept/server/internal/secret"
	"kept/server/internal/share"
	"kept/server/internal/storage"
	"kept/server/internal/store"
	"kept/server/internal/system"
	"kept/server/internal/taxonomy"
	"kept/server/internal/transfer"
	"kept/server/internal/version"
	"kept/server/internal/views"
	"kept/server/internal/watch"
	"kept/server/internal/watermark"
	"kept/server/internal/web"
	"kept/server/internal/workspace"
)

// Serve runs the HTTP server and the job workers until ctx is cancelled.
// If ready is non-nil it receives the bound listen address once serving.
func Serve(ctx context.Context, cfg config.Config, log *slog.Logger, ready chan<- string) error {
	log.Info("starting kept", "version", version.Version, "commit", version.Commit, "data_dir", cfg.DataDir)

	if err := prepareDirs(cfg); err != nil {
		return err
	}
	st, err := store.Open(ctx, cfg.Database())
	if err != nil {
		return err
	}
	defer st.Close()
	if st.Backup != "" {
		log.Info("backed up the database before upgrading it", "path", st.Backup)
	}
	// What this instance may do (v1.12): a license, the trial, or free.
	editionSvc := edition.NewService(st.DB, edition.Options{Override: edition.DevOverride(), OpenSource: edition.OpenSourceBuild})
	if err := editionSvc.Init(ctx); err != nil {
		return fmt.Errorf("read the edition: %w", err)
	}
	allows := func(e edition.Edition) func() bool { return func() bool { return editionSvc.Allows(e) } }

	queue := jobs.NewQueue(st.DB)
	pool := jobs.NewPool(queue, jobs.PoolConfig{
		Workers: cfg.Workers,
		Grace:   max(cfg.ShutdownTimeout-5*time.Second, time.Second),
	}, log.With("component", "jobs"))
	registerJobs(pool)

	bus := events.NewBus()
	bus.OnDrop(func(e events.Event) { log.Debug("event dropped for a slow subscriber", "type", e.Type) })
	fileStore, err := NewFileStore(cfg)
	if err != nil {
		return err
	}
	// Storages added on the page (v1.7.2) join before anything reads files.
	box, err := secret.Load(cfg.DataDir, cfg.SecretKey)
	if err != nil {
		return fmt.Errorf("the master key: %w", err)
	}
	storageSvc := storage.NewService(st.DB, fileStore, storage.Options{Queue: queue, Log: log.With("component", "storage"),
		Env: cfg.Storages, Box: box, DataDir: cfg.DataDir})
	if err := storageSvc.LoadSaved(ctx); err != nil {
		return fmt.Errorf("load the storages: %w", err)
	}
	if err := storageSvc.LoadKeys(ctx); err != nil {
		return fmt.Errorf("load the storages' encryption keys: %w", err)
	}
	index, rebuilt, err := search.Open(cfg.IndexDir())
	if err != nil {
		return fmt.Errorf("open the search index: %w", err)
	}
	defer index.Close()
	indexer := search.NewIndexer(st.DB, index, bus, log.With("component", "search"))
	if rebuilt {
		log.Info("the search index was created or its format changed; indexing every document in the background")
		if err := indexer.MarkAll(ctx); err != nil {
			return err
		}
	}

	integrity.NewChecker(st.DB, fileStore, cfg.DataDir, index).Register(pool)
	drawers := drawer.NewService(st.DB, drawer.Options{Queue: queue, Bus: bus, Files: fileStore})
	drawers.Register(pool)
	drawers.RegisterAlerts(pool)

	brandingSvc := branding.NewService(st.DB, cfg.BrandingDir(), branding.Options{})
	instanceName := func(ctx context.Context) string {
		if b, err := brandingSvc.Get(ctx); err == nil {
			return b.Name
		}
		return branding.DefaultName
	}
	authSvc := auth.NewService(st.DB, auth.Options{SessionTTL: cfg.SessionTTL, Name: instanceName})
	// Workflows need documents and documents tell workflows about changes;
	// workflows are a paid module (v1.13), wired last, so both are set later.
	var userUpdated func(ws int64, ids []int64)
	workflows := &ingest.LateWorkflows{}
	documents := document.NewService(st.DB, document.Options{Bus: bus, Files: fileStore,
		OnUserUpdate: func(ws int64, ids []int64) {
			if userUpdated != nil {
				userUpdated(ws, ids)
			}
		},
		// a private document's files stay here, sealed copies go out (v1.8.1)
		OnPrivacyChange: func(ws int64) {
			if err := storageSvc.Requeue(context.Background(), ws); err != nil {
				log.Warn("queue moving private documents' files", "workspace", ws, "err", err)
			}
		},
		Search: index, SearchReady: indexer.Ready, SearchLimit: cfg.SearchLimit, Placer: drawers, OnPurge: func(ids []int64) {
			if err := index.Delete(ids); err != nil {
				log.Warn("remove purged documents from the search index", "err", err)
			}
		}})
	classifier := classify.NewService(st.DB, classify.Options{Log: log.With("component", "classify"), Queue: queue, Documents: documents})
	classifier.Register(pool)
	pagesSvc := pages.NewService(st.DB, fileStore, pages.Options{Bus: bus})
	publicURL := ""
	if len(cfg.PublicURLs) > 0 {
		publicURL = cfg.PublicURLs[0]
	}
	notifySvc := notify.NewService(st.DB, notify.Options{SMTP: cfg.SMTP, Name: instanceName, Queue: queue, Files: fileStore, Documents: documents,
		Drawers: drawers, PublicURL: publicURL, Log: log.With("component", "notify")})
	notifySvc.Register(pool)
	privacySvc := privacy.New(st.DB)
	privacySvc.SetMailer(func(ctx context.Context, ws int64, userIDs []int64, subject, text string) {
		if !notifySvc.Enabled() {
			return
		}
		to, err := userEmails(ctx, st.DB, userIDs)
		if err != nil || len(to) == 0 {
			return
		}
		if publicURL != "" {
			text += "\n\n" + publicURL
		}
		// a notice: each recipient gets it in their language (v1.10.2)
		if _, err := notifySvc.QueueNotice(ctx, ws, to, fmt.Sprintf(i18n.N("【收好】%s"), subject), text); err != nil {
			log.Warn("email about private documents", "workspace", ws, "err", err)
		}
	})
	hooks := netguard.Policy{AllowPrivate: cfg.WebhookAllowPrivate, DenyAddrs: netguard.LocalAddrs()}
	if u, err := url.Parse(cfg.GotenbergURL); err == nil && u.Host != "" {
		hooks.DenyHosts = append(hooks.DenyHosts, u.Host)
	}
	taxSvc := taxonomy.NewService(st.DB, taxonomy.Options{})
	exportSvc := export.NewService(export.Deps{DB: st.DB, Documents: documents, Taxonomy: taxSvc, Files: fileStore})
	exportSvc.Register(pool, cfg.ExportDir, log.With("component", "export"))
	booksSvc := books.NewService(st.DB, bus, drawers, fileStore)
	dedupeSvc := dedupe.NewService(st.DB, dedupe.Options{Queue: queue, Documents: documents})
	dedupeSvc.Register(pool)
	quotaSvc := quota.NewService(st.DB, quota.Options{Room: storageSvc.Room, Files: fileStore, DefaultBytes: cfg.DefaultQuota, Log: log.With("component", "quota")})
	storageSvc.Register(pool)
	// The paid modules hear about new documents too (v1.13), set later.
	var eeAdded func(ctx context.Context, ws, id int64)
	watermark.SetFont(cfg.WatermarkFont) // share watermarks (v1.9.2)
	ingestSvc := ingest.NewService(st.DB, queue, fileStore, bus, ingest.Options{Quota: quotaSvc,
		Parser:         parser.Config{OCRLanguages: cfg.OCRLanguages, OCRMode: cfg.OCRMode, Rotate: cfg.OCRRotate, Deskew: cfg.OCRDeskew, RotateThreshold: cfg.OCRRotateThreshold, GotenbergURL: cfg.GotenbergURL},
		MaxUploadSize:  cfg.MaxUploadSize,
		Classifier:     classifier,
		Placer:         drawers,
		Log:            log.With("component", "ingest"),
		AfterReprocess: pagesSvc.Reapply,
		Barcodes:       cfg.Barcodes,
		BarcodeOptions: barcode.Options{Separator: cfg.BarcodeSep, ASNPrefix: cfg.BarcodeASN, DPI: cfg.BarcodeDPI},
		Workflows:      workflows,
		OnAdded: func(ctx context.Context, ws, id int64) {
			if eeAdded != nil {
				eeAdded(ctx, ws, id)
			}
		},
		SetFields: ingest.SetFieldsWith(documents),
	})
	ingestSvc.Register(pool)
	// Remote inboxes (v1.7.7): folders of storages read every 5 minutes.
	pagesSvc.Register(pool, pages.Deps{Queue: queue, Ingest: ingestSvc, Documents: documents})
	if n, err := ingestSvc.CleanTmp(ctx); err != nil {
		log.Warn("clean temporary directory", "err", err)
	} else if n > 0 {
		log.Info("removed leftover upload directories", "count", n)
	}
	if missing := parser.MissingTools(); len(missing) > 0 {
		log.Warn("tools for ingesting PDFs and scans are missing; such uploads will fail", "missing", missing)
	}

	workspaces := workspace.NewService(st.DB, workspace.Options{})
	auditLog := audit.NewLog(st.DB)
	// The consume directory (KEPT_CONSUME_DIR): files dropped there are
	// ingested like uploads.
	var watcher *watch.Watcher
	var watchStatus func() watch.Status
	if cfg.ConsumeDir != "" {
		watcher = watch.New(st.DB, ingestSvc, taxSvc, watch.Options{Dir: cfg.ConsumeDir, Log: log.With("component", "watch")})
		watchStatus = watcher.Status
	}
	// where the daily backup goes: KEPT_BACKUP_DIR, or the page's choice (v1.13.9)
	backupDir := backup.NewDir(st.DB, cfg.BackupDir, cfg.DataDir)
	systemSvc := system.NewService(st.DB, system.Options{
		DataDir: cfg.DataDir, DBPath: cfg.DBPath(), IndexDir: cfg.IndexDir(), GotenbergURL: cfg.GotenbergURL,
		Index: index, IndexReady: indexer.Ready, StartedAt: time.Now(), BackupDir: backupDir, Watch: watchStatus,
		Settings: map[string]string{
			"KEPT_WORKERS": strconv.Itoa(cfg.Workers), "KEPT_OCR_LANGUAGES": cfg.OCRLanguages, "KEPT_OCR_MODE": cfg.OCRMode, "KEPT_OCR_ROTATE": strconv.FormatBool(cfg.OCRRotate), "KEPT_WEBHOOK_ALLOW_PRIVATE": strconv.FormatBool(cfg.WebhookAllowPrivate), "KEPT_MAIL_ALLOW_PRIVATE": strconv.FormatBool(cfg.MailAllowPrivate), "KEPT_OCR_DESKEW": strconv.FormatBool(cfg.OCRDeskew), "KEPT_OCR_ROTATE_THRESHOLD": strconv.FormatFloat(cfg.OCRRotateThreshold, 'f', -1, 64),
			"KEPT_MAX_UPLOAD_SIZE": strconv.FormatInt(cfg.MaxUploadSize>>20, 10) + " MB", "KEPT_SEARCH_LIMIT": strconv.Itoa(cfg.SearchLimit),
			"KEPT_SESSION_TTL": cfg.SessionTTL.String(), "KEPT_AUDIT_RETENTION": cfg.AuditRetention.String(), "TZ": time.Local.String(),
			"KEPT_PUBLIC_URLS": strings.Join(cfg.PublicURLs, ", "), "KEPT_CONSUME_DIR": cfg.ConsumeDir,
			"KEPT_BARCODES":      strconv.FormatBool(cfg.Barcodes),
			"KEPT_STORAGES":      storageNames(cfg.Storages),
			"KEPT_SMTP_HOST":     cfg.SMTP.Host,
			"KEPT_DEFAULT_QUOTA": quotaText(cfg.DefaultQuota), "KEPT_OIDC_ISSUER": cfg.SSO.Issuer,
			"KEPT_AI_URL": cfg.AI.URL, "KEPT_AI_MODEL": cfg.AI.Model,
			"KEPT_DATABASE_URL": databaseText(cfg), "KEPT_TRUSTED_PROXIES": proxiesText(cfg.TrustedProxies),
		},
	})
	// Done when shutdown starts, so open event streams end at once.
	stopping, stopStreams := context.WithCancel(context.WithoutCancel(ctx))
	defer stopStreams()
	ee := wireEE(eeWiring{db: st.DB, ingest: ingestSvc, cfg: cfg, bus: bus, queue: queue, pool: pool, notify: notifySvc,
		publicURL: publicURL, log: log, taxonomy: taxSvc, storage: storageSvc, allows: allows, hooks: hooks,
		documents: documents, files: fileStore, drawers: drawers, privacy: privacySvc, export: exportSvc,
		classifier: classifier, books: booksSvc, pages: pagesSvc})
	workflows.W, userUpdated, eeAdded = ee.workflows, ee.userUpdated, ee.added
	apiSrv, err := api.NewServer(api.Deps{
		DB: st, Auth: authSvc, Workspaces: workspaces, Audit: auditLog, Taxonomy: taxSvc,
		Branding:  brandingSvc,
		Documents: documents, Classifier: classifier, Drawers: drawers, Pages: pagesSvc, Notify: notifySvc, Shares: share.NewService(st.DB, share.Options{Documents: documents, Files: fileStore}), Dedupe: dedupeSvc, System: systemSvc, Notes: notes.NewService(st.DB, bus), Books: booksSvc, BookLookup: bookmeta.New(bookmeta.Options{Sources: cfg.Books.Sources, CustomURL: cfg.Books.LookupURL, GoogleKey: cfg.Books.GoogleKey}), Reading: reading.NewService(st.DB), BookLists: booklists.NewService(st.DB), Views: views.NewService(st.DB), Search: index, Ingest: ingestSvc, Files: fileStore, Events: bus, Jobs: queue,
		Log: log.With("component", "http"), SecureCookie: cfg.SecureCookie, TrustedProxies: cfg.TrustedProxies, PublicURLs: cfg.PublicURLs, BackupDir: backupDir, ExportDir: cfg.ExportDir,
		Web: web.Handler(), Stopping: stopping, Quota: quotaSvc, Storage: storageSvc,
		Export:     exportSvc,
		Checklists: checklist.NewService(st.DB, checklist.Options{Documents: documents}), Privacy: privacySvc, Edition: editionSvc,
		EE:       ee.deps,
		Transfer: transfer.NewService(st.DB, transfer.Options{Documents: documents, Taxonomy: taxSvc, Files: fileStore, Placer: drawers, Quota: quotaSvc, Bus: bus}),
	})
	if err != nil {
		return err
	}
	srv := &http.Server{
		Handler:           apiSrv.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		BaseContext:       func(net.Listener) context.Context { return context.WithoutCancel(ctx) },
	}
	srv.RegisterOnShutdown(stopStreams)
	ln, err := net.Listen("tcp", cfg.HTTPAddr)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", cfg.HTTPAddr, err)
	}
	log.Info("listening", "addr", ln.Addr().String(), "workers", cfg.Workers)
	if ready != nil {
		ready <- ln.Addr().String()
	}

	housekeeping(pool, log, map[string]func(context.Context) (int64, error){
		kindPurgeSessions: authSvc.PurgeExpiredSessions,
		kindPurgeAudit: func(ctx context.Context) (int64, error) {
			return auditLog.Purge(ctx, time.Now().Add(-cfg.AuditRetention))
		},
		kindPurgeWorkspaces: func(ctx context.Context) (int64, error) {
			cutoff := time.Now().Add(-workspace.PurgeRetention)
			ids, err := workspaces.DeletedBefore(ctx, cutoff)
			if err != nil {
				return 0, err
			}
			n, err := workspaces.PurgeDeleted(ctx, cutoff)
			if err != nil {
				return n, err
			}
			for _, id := range ids {
				if err := fileStore.RemoveWorkspace(ctx, id); err != nil {
					log.Error("remove files of a purged workspace", "workspace", id, "err", err)
				}
				if err := index.DeleteWorkspace(id); err != nil {
					log.Error("remove a purged workspace from the search index", "workspace", id, "err", err)
				}
			}
			return n, nil
		},
		kindPurgeTrash: func(ctx context.Context) (int64, error) {
			return documents.PurgeExpired(ctx, time.Now().Add(-document.TrashRetention))
		},
		// as far back as the task list goes (v1.7.19); failed uploads' files
		// after a week (v1.9.12)
		kindPurgeJobs: func(ctx context.Context) (int64, error) {
			n, err := ingestSvc.PurgeFailed(ctx, time.Now())
			if err != nil {
				return n, err
			}
			m, err := queue.PurgeFinished(ctx, time.Now().Add(-JobRetention))
			return n + m, err
		},
		// watermarked copies of shared files not opened for a week (v1.9.2)
		kindPurgeCache: func(context.Context) (int64, error) { return fileStore.CleanCache(7 * 24 * time.Hour) },
		// the query planner's statistics (v1.13.5)
		kindOptimize: func(ctx context.Context) (int64, error) { return 0, store.Optimize(ctx, st.DB, st.Postgres) },
	})
	// The daily backup runs only when a directory is set, in the deployment
	// or on the page (v1.13.9); its job is registered regardless so "back up
	// now" and leftover queued runs work.
	backup.Register(pool, st.DB, backupDir, cfg.DataDir, fileStore, log.With("component", "backup"))
	// the paid modules' scheduled work (v1.13), already limited to their edition
	entries := append(slices.Clone(schedule), ee.schedule...)
	entries = append(entries, jobs.Entry{Name: "backup", Kind: backup.Kind, At: "02:30", MaxAttempts: 1,
		When: func(ctx context.Context) bool { return backupDir.Get(ctx) != "" }})
	if cfg.ExportDir != "" {
		entries = append(slices.Clone(entries), jobs.Entry{Name: "export", Kind: export.KindScheduled, At: "03:15", PerWorkspace: true, MaxAttempts: 1})
	}
	// Per-workspace checks only where they have something to do (v1.7.16,
	// v1.7.19): no empty runs filling the task table.
	whenWorkspace := map[string]func(context.Context, int64) bool{}
	for name, q := range onlyWhere {
		whenWorkspace[name] = sqlYes(st.DB, q)
	}
	for i, e := range entries {
		if f, ok := whenWorkspace[e.Name]; ok {
			entries[i].WhenWorkspace = f
		}
		if e.Name == "notify-alerts" {
			entries[i].When = func(context.Context) bool { return notifySvc.Enabled() }
		}
		if e.Name == "convert-pending" {
			entries[i].When = func(context.Context) bool { return ingestSvc.CanConvert() }
		}
	}
	scheduler := jobs.NewScheduler(st.DB, queue, entries, log.With("component", "scheduler"))

	g, gctx := errgroup.WithContext(ctx)
	g.Go(func() error {
		if err := srv.Serve(ln); !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		return nil
	})
	g.Go(func() error { return pool.Run(gctx) })
	g.Go(func() error { indexer.Run(gctx); return nil })
	g.Go(func() error { scheduler.Run(gctx); return nil })
	if watcher != nil {
		g.Go(func() error { watcher.Run(gctx); return nil })
	}
	g.Go(func() error {
		drawers.RunAlertRefresher(gctx, bus, 2*time.Second, log.With("component", "drawer"))
		return nil
	})
	g.Go(func() error {
		quotaSvc.Watch(gctx, bus)
		return nil
	})
	for _, run := range ee.runs { // the paid modules' own loops (v1.13)
		g.Go(func() error { run(gctx); return nil })
	}
	g.Go(func() error { // tell administrators when a space nearly fills up (v1.7.4)
		quotaSvc.RunWarnings(gctx, 10*time.Minute, notifySvc.SendQuotaWarning)
		return nil
	})
	g.Go(func() error {
		<-gctx.Done()
		log.Info("shutting down", "timeout", cfg.ShutdownTimeout)
		sctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), cfg.ShutdownTimeout)
		defer cancel()
		return srv.Shutdown(sctx)
	})
	err = g.Wait()
	log.Info("stopped")
	return err
}

// Migrate applies pending database migrations and exits.
func Migrate(ctx context.Context, cfg config.Config, log *slog.Logger) error {
	if err := prepareDirs(cfg); err != nil {
		return err
	}
	st, err := store.Open(ctx, cfg.Database()) // Open runs migrations
	if err != nil {
		return err
	}
	defer st.Close()
	if st.Backup != "" {
		log.Info("backed up the database before upgrading it", "path", st.Backup)
	}
	log.Info("database is up to date", "database", cfg.DatabaseLabel())
	return nil
}

func prepareDirs(cfg config.Config) error {
	for _, d := range cfg.Dirs() {
		if err := os.MkdirAll(d, 0o750); err != nil {
			return fmt.Errorf("create data directory: %w", err)
		}
	}
	return nil
}

const (
	kindPurgeSessions   = "system.purge_sessions"
	kindPurgeAudit      = "system.purge_audit"
	kindPurgeWorkspaces = "system.purge_workspaces"
	kindPurgeTrash      = "system.purge_trash"
	kindPurgeJobs       = "system.purge_jobs"
	kindPurgeCache      = "system.purge_cache"
	kindOptimize        = "system.optimize"
)

// JobRetention is how long finished jobs are kept: as far back as the task
// list reaches (v1.7.19).
const JobRetention = 90 * 24 * time.Hour

// eeWiring is what the paid modules (v1.13) are made from; the official
// build (-tags ee) makes them in ee_wire.go, the open-source build has none.
type eeWiring struct {
	db        *sql.DB
	ingest    *ingest.Service
	cfg       config.Config
	bus       *events.Bus
	queue     *jobs.Queue
	pool      *jobs.Pool
	notify    *notify.Service
	publicURL string
	log       *slog.Logger
	taxonomy  *taxonomy.Service
	storage   *storage.Service
	// allows says whether the edition includes one (v1.12)
	allows     func(edition.Edition) func() bool
	hooks      netguard.Policy // what outgoing connections may reach
	documents  *document.Service
	files      *files.Store
	drawers    *drawer.Service
	privacy    *privacy.Service
	export     *export.Service
	classifier *classify.Service
	books      *books.Service
	pages      *pages.Service
}

// eeParts are the paid modules (v1.13): their services, the loops they
// run, and their scheduled work (already limited to their edition).
type eeParts struct {
	deps     api.EEDeps
	runs     []func(context.Context)
	schedule []jobs.Entry
	// workflows hear about new documents (nil: none), userUpdated about
	// documents people changed
	workflows   ingest.Workflows
	userUpdated func(ws int64, ids []int64)
	// added hears about new documents (nil: nothing to do)
	added func(ctx context.Context, ws, id int64)
}

// onlyIn adds the edition's check to an entry's own condition.
func onlyIn(e jobs.Entry, allowed func() bool) jobs.Entry {
	when := e.When
	e.When = func(ctx context.Context) bool { return allowed() && (when == nil || when(ctx)) }
	return e
}

// onlyWhere limits per-workspace entries to the workspaces this query says
// yes for; every ? is the workspace's ID (v1.7.16).
var onlyWhere = map[string]string{
	"mail-fetch":         `SELECT EXISTS (SELECT 1 FROM mail_accounts WHERE workspace_id = ? AND enabled = 1)`,
	"health-alerts":      `SELECT EXISTS (SELECT 1 FROM lab_reports WHERE workspace_id = ? AND status = 'confirmed')`,
	"convert-pending":    `SELECT EXISTS (SELECT 1 FROM pending_conversions WHERE workspace_id = ?)`,
	"workflow-scheduled": `SELECT EXISTS (SELECT 1 FROM workflows WHERE workspace_id = ? AND enabled = 1 AND triggers LIKE '%"type":"scheduled"%')`,
	// v1.7.19: only when there is no model of this format yet, or documents
	// or the types, correspondents and tags changed since it was trained.
	"classifier-train": fmt.Sprintf(`SELECT NOT EXISTS (SELECT 1 FROM classifier_models WHERE workspace_id = ? AND format_version = %d)
		OR EXISTS (SELECT 1 FROM classifier_models AS m WHERE m.workspace_id = ? AND (
			EXISTS (SELECT 1 FROM documents AS d WHERE d.workspace_id = m.workspace_id AND (d.modified_at > m.trained_at OR d.deleted_at > m.trained_at))
			OR EXISTS (SELECT 1 FROM document_types AS t WHERE t.workspace_id = m.workspace_id AND t.updated_at > m.trained_at)
			OR EXISTS (SELECT 1 FROM correspondents AS c WHERE c.workspace_id = m.workspace_id AND c.updated_at > m.trained_at)
			OR EXISTS (SELECT 1 FROM tags AS g WHERE g.workspace_id = m.workspace_id AND g.updated_at > m.trained_at)))`, classify.FormatVersion),
}

// sqlYes asks q, with the workspace's ID for every ?; on an error the entry
// runs as before.
func sqlYes(db *sql.DB, q string) func(context.Context, int64) bool {
	n := strings.Count(q, "?")
	return func(ctx context.Context, ws int64) bool {
		args := make([]any, n)
		for i := range args {
			args[i] = ws
		}
		var yes bool
		return db.QueryRowContext(ctx, q, args...).Scan(&yes) != nil || yes
	}
}

// schedule is the scheduler's table (design doc 11.7). Daily times are in
// the server's local time zone (TZ). The trash purge is one job for all
// workspaces; it purges each workspace in its own transaction.
var schedule = []jobs.Entry{
	{Name: "purge-sessions", Kind: kindPurgeSessions, Every: 6 * time.Hour, MaxAttempts: 1},
	{Name: "purge-audit", Kind: kindPurgeAudit, At: "05:00", MaxAttempts: 1},
	{Name: "purge-workspaces", Kind: kindPurgeWorkspaces, At: "04:30", MaxAttempts: 1},
	{Name: "purge-trash", Kind: kindPurgeTrash, At: "04:00", MaxAttempts: 1},
	{Name: "purge-jobs", Kind: kindPurgeJobs, At: "04:15", MaxAttempts: 1},
	{Name: "purge-cache", Kind: kindPurgeCache, At: "04:20", MaxAttempts: 1},
	{Name: "optimize", Kind: kindOptimize, At: "04:25", MaxAttempts: 1},
	{Name: "drawer-alerts", Kind: drawer.KindAlerts, At: "03:00", PerWorkspace: true, MaxAttempts: 1},
	{Name: "integrity", Kind: integrity.Kind, Every: 7 * 24 * time.Hour, PerWorkspace: true, MaxAttempts: 1},
	{Name: "classifier-train", Kind: classify.KindTrain, Every: time.Hour, PerWorkspace: true, MaxAttempts: 1},
	{Name: "notify-alerts", Kind: notify.KindAlerts, At: "08:00", PerWorkspace: true, MaxAttempts: 3},
	{Name: "dedupe-scan", Kind: dedupe.Kind, Every: 7 * 24 * time.Hour, PerWorkspace: true, MaxAttempts: 1},
	// Office documents and e-mails kept before Gotenberg was set up (v1.9.1)
	{Name: "convert-pending", Kind: ingest.KindConvertPending, Every: time.Hour, PerWorkspace: true, MaxAttempts: 1},
}

// housekeeping registers the clean-up jobs; each logs and returns how many
// rows it removed.
func housekeeping(p *jobs.Pool, log *slog.Logger, tasks map[string]func(context.Context) (int64, error)) {
	for kind, task := range tasks {
		p.Handle(kind, func(ctx context.Context, _ jobs.Job) (any, error) {
			n, err := task(ctx)
			if err != nil {
				return nil, err
			}
			if n > 0 {
				log.Info("housekeeping", "job", kind, "removed", n)
			}
			return map[string]int64{"removed": n}, nil
		})
	}
}

// registerJobs is where job handlers are added as features land
// (ingest, index_document, classifier training, ...).
func registerJobs(p *jobs.Pool) {
	p.Handle("system.noop", func(context.Context, jobs.Job) (any, error) { return nil, nil })
}

// proxiesText shows KEPT_TRUSTED_PROXIES on the system page.
func proxiesText(ps []netip.Prefix) string {
	out := make([]string, len(ps))
	for i, p := range ps {
		out[i] = p.String()
	}
	return strings.Join(out, ", ")
}

// databaseText shows KEPT_DATABASE_URL on the system page, without the password.
func databaseText(cfg config.Config) string {
	if cfg.DatabaseURL == "" {
		return ""
	}
	return cfg.DatabaseLabel()
}

// quotaText shows KEPT_DEFAULT_QUOTA on the system page.
func quotaText(n int64) string {
	if n == 0 {
		return "不限"
	}
	return quota.Human(n)
}

// userEmails returns the addresses of active accounts that have one.
func userEmails(ctx context.Context, db *sql.DB, ids []int64) ([]string, error) {
	var out []string
	for _, id := range ids {
		var email string
		err := db.QueryRowContext(ctx, `SELECT email FROM users WHERE id = ? AND disabled_at IS NULL`, id).Scan(&email)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if email != "" {
			out = append(out, email)
		}
	}
	return out, nil
}
