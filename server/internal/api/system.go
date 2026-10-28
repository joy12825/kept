package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"kept/server/internal/api/gen"
	"kept/server/internal/backup"
	"kept/server/internal/classify"
	"kept/server/internal/dedupe"
	"kept/server/internal/drawer"
	"kept/server/internal/i18n"
	"kept/server/internal/ingest"
	"kept/server/internal/integrity"
	"kept/server/internal/jobs"
	"kept/server/internal/logging"
	"kept/server/internal/notify"
	"kept/server/internal/pages"
	"kept/server/internal/storage"
	"kept/server/internal/store/sqlc"
	"kept/server/internal/tenant"
)

// taskLabels name the job kinds a workspace sees in its task list.
var taskLabels = map[string]string{
	ingest.KindDocument: i18n.N("上传"), ingest.KindReprocess: i18n.N("重新处理"), classify.KindApply: i18n.N("重新分类"), classify.KindTrain: i18n.N("训练分类模型"),
	drawer.KindReassign: i18n.N("整理抽屉"), drawer.KindAlerts: i18n.N("检查提醒"), integrity.Kind: i18n.N("完整性检查"),
	pages.KindSplit: i18n.N("拆分文档"), pages.KindMerge: i18n.N("合并文档"), "mail.fetch": i18n.N("收取邮件"),
	notify.KindEmail: i18n.N("发送邮件"), notify.KindAlerts: i18n.N("提醒邮件"),
	dedupe.Kind: i18n.N("检查近似重复"), "agent.intake": i18n.N("AI 识别"), "workflow.run": i18n.N("运行工作流"), "workflow.scheduled": i18n.N("检查定时工作流"), "workflow.webhook": i18n.N("调用 Webhook"),
	storage.KindMigrate: i18n.N("移动文档"), storage.KindExport: i18n.N("导出到存储"), // v1.7.14
}

// taskGroups sort the kinds for the task list's filter (v1.7.14); kinds
// not listed are “other”.
var taskGroups = map[gen.TaskGroup][]string{
	gen.TaskGroupUpload:  {ingest.KindDocument},
	gen.TaskGroupStorage: {storage.KindMigrate, storage.KindExport},
	gen.TaskGroupOrganize: {ingest.KindReprocess, classify.KindApply, classify.KindTrain, drawer.KindReassign, pages.KindSplit, pages.KindMerge,
		dedupe.Kind, "agent.intake", "workflow.run", "workflow.scheduled", "workflow.webhook"},
}

func taskGroup(kind string) gen.TaskGroup {
	for g, kinds := range taskGroups {
		for _, k := range kinds {
			if k == kind {
				return g
			}
		}
	}
	return gen.TaskGroupOther
}

// taskProgress is a running job's progress in percent, when it is known.
func taskProgress(r sqlc.ListWorkspaceJobsRow) *int {
	if r.Status != "running" || !r.Result.Valid {
		return nil
	}
	pct := -1
	switch r.Kind {
	case ingest.KindDocument, ingest.KindReprocess:
		var p ingest.Progress
		if json.Unmarshal([]byte(r.Result.String), &p) == nil {
			pct = p.Progress
		}
	case storage.KindMigrate:
		var m storage.Moved
		if json.Unmarshal([]byte(r.Result.String), &m) == nil && m.Total > 0 {
			pct = (m.Moved + m.Skipped + m.Failed) * 100 / m.Total
		}
	}
	if pct < 0 {
		return nil
	}
	pct = min(pct, 100)
	return &pct
}

// taskSummary describes a job's object or outcome in a few words.
func taskSummary(r sqlc.ListWorkspaceJobsRow) string {
	if out, ok := eeTaskSummary(r); ok { // the paid modules' tasks (v1.13)
		return out
	}
	var res map[string]json.RawMessage
	_ = json.Unmarshal([]byte(r.Result.String), &res)
	num := func(k string) int { var n int; _ = json.Unmarshal(res[k], &n); return n }
	switch r.Kind {
	case ingest.KindDocument:
		var pl struct{ Filename string }
		_ = json.Unmarshal([]byte(r.Payload), &pl)
		return pl.Filename
	case ingest.KindReprocess:
		return r.DocumentTitle
	case storage.KindMigrate:
		var m storage.Moved
		if json.Unmarshal([]byte(r.Result.String), &m) != nil || m.Total == 0 {
			break
		}
		if r.Status == "running" || r.Status == "queued" {
			return fmt.Sprintf(i18n.N("%d/%d 份"), m.Moved+m.Skipped+m.Failed, m.Total)
		}
		switch {
		case m.Skipped > 0 && m.Failed > 0:
			return fmt.Sprintf(i18n.N("移了 %d 份，%d 份稍后再移，%d 份失败"), m.Moved, m.Skipped, m.Failed)
		case m.Skipped > 0:
			return fmt.Sprintf(i18n.N("移了 %d 份，%d 份稍后再移"), m.Moved, m.Skipped)
		case m.Failed > 0:
			return fmt.Sprintf(i18n.N("移了 %d 份，%d 份失败"), m.Moved, m.Failed)
		}
		return fmt.Sprintf(i18n.N("移了 %d 份"), m.Moved)
	case storage.KindExport:
		if r.Status == "succeeded" {
			var res struct {
				Skipped                     string
				Written, Unchanged, Removed int
			}
			_ = json.Unmarshal([]byte(r.Result.String), &res)
			if res.Skipped != "" {
				return res.Skipped
			}
			return fmt.Sprintf(i18n.N("写入 %d 份，%d 份没变，删除 %d 份"), res.Written, res.Unchanged, res.Removed)
		}
	case pages.KindSplit:
		var res pages.Result
		_ = json.Unmarshal([]byte(r.Result.String), &res)
		if r.Status == "succeeded" {
			return fmt.Sprintf(i18n.N("拆成 %d 份"), len(res.Created))
		}
	case notify.KindEmail:
		var pl struct {
			To        []string
			Documents []int64
		}
		_ = json.Unmarshal([]byte(r.Payload), &pl)
		who := ""
		if len(pl.To) > 0 {
			who = pl.To[0]
		}
		if len(pl.To) > 1 {
			who = fmt.Sprintf(i18n.N("%s 等 %d 人"), who, len(pl.To))
		}
		if len(pl.Documents) == 0 {
			return fmt.Sprintf(i18n.N("发给 %s"), who)
		}
		return fmt.Sprintf(i18n.N("发给 %s，%d 份附件"), who, len(pl.Documents))
	case dedupe.Kind:
		if r.Status == "succeeded" {
			var res dedupe.Result
			_ = json.Unmarshal([]byte(r.Result.String), &res)
			if res.Open == 0 {
				return fmt.Sprintf(i18n.N("%d 份文档，没有发现近似重复"), res.Compared)
			}
			return fmt.Sprintf(i18n.N("%d 份文档，%d 对疑似重复"), res.Compared, res.Open)
		}
	case notify.KindAlerts:
		if r.Status == "succeeded" {
			var res notify.AlertsResult
			_ = json.Unmarshal([]byte(r.Result.String), &res)
			if res.Recipients == 0 {
				return i18n.N("没有需要发送的提醒")
			}
			return fmt.Sprintf(i18n.N("%d 条新提醒，发给 %d 人"), res.Alerts, res.Recipients)
		}
	case pages.KindMerge:
		var pl struct{ IDs []int64 }
		_ = json.Unmarshal([]byte(r.Payload), &pl)
		return fmt.Sprintf(i18n.N("%d 份合并为一份"), len(pl.IDs))
	case classify.KindApply:
		var pl struct{ IDs []int64 }
		_ = json.Unmarshal([]byte(r.Payload), &pl)
		if r.Status == "succeeded" {
			return fmt.Sprintf(i18n.N("共 %d 份，改动 %d 份"), len(pl.IDs), num("changed"))
		}
		return fmt.Sprintf(i18n.N("共 %d 份"), len(pl.IDs))
	case classify.KindTrain:
		if r.Status != "succeeded" {
			return ""
		}
		var t classify.TrainResult
		_ = json.Unmarshal([]byte(r.Result.String), &t)
		switch {
		case t.Unchanged:
			return i18n.N("已整理的文档没有变化，未重新训练")
		case t.Trained:
			return fmt.Sprintf(i18n.N("用 %d 份已整理的文档训练"), t.Summary.DocCount)
		}
		return i18n.N("文档不够，没有训练")
	case drawer.KindReassign:
		if r.Status == "succeeded" {
			return fmt.Sprintf(i18n.N("%d 份文档换了抽屉"), num("moved"))
		}
	case drawer.KindAlerts:
		if r.Status == "succeeded" {
			var changed bool
			_ = json.Unmarshal(res["changed"], &changed)
			if changed {
				return i18n.N("提醒有更新")
			}
			return i18n.N("没有变化")
		}
	case integrity.Kind:
		if r.Status != "succeeded" {
			return ""
		}
		var rep integrity.Report
		_ = json.Unmarshal([]byte(r.Result.String), &rep)
		if rep.OK() {
			return fmt.Sprintf(i18n.N("%d 份文档，文件完整"), rep.Documents)
		}
		return fmt.Sprintf(i18n.N("缺少 %d 个文件，%d 个文件没有记录"), rep.MissingCount, rep.OrphanCount)
	}
	return ""
}

// uploadShown: an upload that is not a document yet is listed for the person
// who uploaded it; one nobody uploaded in person (a link, a mailbox) for
// the owners once it failed.
func uploadShown(scope tenant.Scope, createdBy sql.NullInt64, status string) bool {
	if scope.Viewer() == tenant.ViewerSystem {
		return true
	}
	if createdBy.Valid {
		return createdBy.Int64 == scope.Viewer()
	}
	return status == "failed" && scope.Role() == tenant.RoleOwner
}

func (s *Server) ListTasks(ctx context.Context, req gen.ListTasksRequestObject) (gen.ListTasksResponseObject, error) {
	days := 30
	if req.Params.Days != nil {
		days = *req.Params.Days
	}
	if days < 1 || days > 90 {
		return nil, invalid("days 应在 1 到 90 之间")
	}
	var f jobs.ListFilter
	if req.Params.Status != nil {
		f.Status = string(*req.Params.Status)
	}
	if g := req.Params.Group; g != nil {
		switch *g {
		case gen.TaskGroupOther:
			// every listed kind not in another group
			for k := range taskLabels {
				if taskGroup(k) == gen.TaskGroupOther {
					f.Kinds = append(f.Kinds, k)
				}
			}
		default:
			f.Kinds = taskGroups[*g]
		}
		if len(f.Kinds) == 0 {
			return nil, invalid("group 只能是 upload、storage、organize 或 other")
		}
	}
	if len(f.Kinds) == 0 {
		// only the listed kinds, in the query: otherwise the semantic index's
		// runs every five minutes push everything else past the limit (v1.7.19)
		for k := range taskLabels {
			f.Kinds = append(f.Kinds, k)
		}
	}
	f.People = req.Params.People != nil && *req.Params.People
	if req.Params.Mine != nil && *req.Params.Mine {
		f.CreatedBy = mustPrincipal(ctx).UserID
	}
	limit := 500
	if req.Params.Limit != nil {
		limit = *req.Params.Limit
	}
	if limit < 1 || limit > 500 {
		return nil, invalid("limit 应在 1 到 500 之间")
	}
	rows, err := s.jobs.ListWorkspace(ctx, tenant.MustFrom(ctx), time.Now().AddDate(0, 0, -days), f, limit)
	if err != nil {
		return nil, err
	}
	scope := tenant.MustFrom(ctx)
	now := time.Now()
	out := gen.ListTasks200JSONResponse{}
	for _, r := range rows {
		label, ok := taskLabels[r.Kind]
		if !ok {
			continue
		}
		// an upload not yet a document the viewer sees: whether it will be
		// private is not decided (v1.9.12, as the tray since v1.9.10)
		if r.Kind == ingest.KindDocument && !r.DocumentID.Valid && !uploadShown(scope, r.CreatedBy, r.Status) {
			continue
		}
		t := gen.Task{Id: r.ID, Kind: r.Kind, Label: i18n.T(ctx, label), Status: gen.TaskStatus(r.Status), Attempts: int(r.Attempts),
			CreatedAt: time.UnixMilli(r.CreatedAt), DocumentId: idPtr(r.DocumentID.Int64), Summary: strPtr(i18n.T(ctx, taskSummary(r))),
			Group: taskGroup(r.Kind), Progress: taskProgress(r), CreatorName: strPtr(r.CreatorName)}
		if r.FinishedAt.Valid {
			f := time.UnixMilli(r.FinishedAt.Int64)
			t.FinishedAt = &f
		}
		if r.Status == "failed" && r.Error.Valid {
			msg := i18n.T(ctx, ingest.JobErrorMessage(r.Error.String))
			t.Error = &msg
		}
		if r.Kind == ingest.KindDocument && r.Status == "failed" && ingest.MayRetry(scope, r.CreatedBy) {
			ok := s.ingest.Retryable(r.Kind, r.Status, r.Payload, r.FinishedAt, now)
			gone := !ok && ingest.KeptForRetry(r.Error.String) // a duplicate was never kept
			t.Retryable, t.Expired = &ok, &gone
		}
		out = append(out, t)
	}
	return out, nil
}

func (s *Server) AdminGetSystem(ctx context.Context, _ gen.AdminGetSystemRequestObject) (gen.AdminGetSystemResponseObject, error) {
	st, err := s.system.Status(ctx)
	if err != nil {
		return nil, err
	}
	var out gen.SystemStatus
	convert(map[string]any{
		"version": st.Version, "commit": st.Commit, "buildTime": st.BuildTime, "goVersion": st.GoVersion, "startedAt": st.StartedAt,
		"storage": map[string]int64{"database": st.Storage.Database, "index": st.Storage.Index, "media": st.Storage.Media, "backups": st.Storage.Backups, "free": st.Storage.Free},
		"users":   st.Users, "workspaces": st.Workspaces, "documents": st.Documents, "trash": st.Trash,
		"indexDocuments": st.IndexDocuments, "indexBehind": st.IndexBehind, "indexReady": st.IndexReady,
		"jobsQueued": st.JobsQueued, "jobsRunning": st.JobsRunning, "jobsFailed": st.JobsFailed, "oldestQueued": st.OldestQueued,
		"settings": st.Settings,
	}, &out)
	out.Tools = make([]gen.SystemTool, len(st.Tools))
	for i, t := range st.Tools {
		out.Tools[i] = gen.SystemTool{Name: i18n.T(ctx, t.Name), Available: t.Available, Purpose: i18n.T(ctx, t.Purpose), Version: strPtr(i18n.T(ctx, t.Version))}
	}
	integ := make([]map[string]any, len(st.Integrity))
	for i, it := range st.Integrity {
		r := it.Report
		integ[i] = map[string]any{"workspaceId": it.WorkspaceID, "workspaceName": it.WorkspaceName, "documents": r.Documents,
			"missingCount": r.MissingCount, "orphanCount": r.OrphanCount, "missingFiles": r.MissingFiles, "orphanFiles": r.OrphanFiles, "checkedAt": r.CheckedAt}
	}
	convert(integ, &out.Integrity)
	if out.Integrity == nil {
		convert([]any{}, &out.Integrity) // [] rather than null
	}
	b := map[string]any{"snapshots": st.Backup.Snapshots}
	if st.Backup.Dir != "" {
		b["dir"] = st.Backup.Dir
		b["dirFromEnv"] = st.Backup.DirFromEnv
	}
	if l := st.Backup.Last; l != nil {
		last := map[string]any{"status": l.Status, "at": l.At}
		if l.Status == "succeeded" {
			r := l.Result
			last["snapshot"], last["documents"], last["files"], last["copied"], last["bytes"], last["missing"] = r.Snapshot, r.Documents, r.Files, r.Copied, r.Bytes, len(r.Missing)
		}
		if l.Error != "" {
			last["error"] = i18n.T(ctx, l.Error)
		}
		b["last"] = last
	}
	convert(b, &out.Backup)
	sm := map[string]any{"configured": s.smtpEnabled()}
	if s.smtpEnabled() {
		c := s.notify.Settings()
		sm["host"], sm["port"], sm["security"], sm["from"], sm["maxAttachments"] = c.Host, c.Port, c.Security, c.From, c.MaxAttachments
		if c.Username != "" {
			sm["username"] = c.Username
		}
	}
	convert(sm, &out.Smtp)
	wt := map[string]any{"folders": []any{}, "waiting": 0, "failed": 0}
	if w := st.Watch; w != nil {
		folders := make([]map[string]any, len(w.Folders))
		for i, f := range w.Folders {
			folders[i] = map[string]any{"workspaceId": f.WorkspaceID, "workspaceName": f.WorkspaceName, "name": f.Name}
		}
		wt["dir"], wt["folders"], wt["waiting"], wt["failed"] = w.Dir, folders, w.Waiting, w.Failed
		if !w.LastScan.IsZero() {
			wt["lastScan"] = w.LastScan
		}
		if w.Err != "" {
			wt["error"] = i18n.T(ctx, w.Err)
		}
	}
	if on, err := s.ingest.WatchVersions(ctx); err != nil {
		return nil, err
	} else if on {
		wt["versions"] = true
	}
	convert(wt, &out.Watch)
	return gen.AdminGetSystem200JSONResponse(out), nil
}

func (s *Server) AdminSetWatch(ctx context.Context, req gen.AdminSetWatchRequestObject) (gen.AdminSetWatchResponseObject, error) {
	if err := s.ingest.SetWatchVersions(ctx, req.Body.Versions); err != nil {
		return nil, err
	}
	return gen.AdminSetWatch204Response{}, nil
}

var errBackupNotConfigured = &apiError{http.StatusConflict, "backup_not_configured", "还没有设置备份目录：请在“设置 → 实例管理 → 文件安全”里选一个，或设置环境变量 KEPT_BACKUP_DIR"}

func (s *Server) AdminRunBackup(ctx context.Context, _ gen.AdminRunBackupRequestObject) (gen.AdminRunBackupResponseObject, error) {
	if s.backupDir.Get(ctx) == "" {
		return nil, errBackupNotConfigured
	}
	if id, ok, err := s.jobs.Active(ctx, backup.Kind); err != nil {
		return nil, err
	} else if ok {
		return gen.AdminRunBackup202JSONResponse{JobId: id}, nil
	}
	id, err := s.jobs.Enqueue(ctx, backup.Kind, nil, jobs.EnqueueOptions{MaxAttempts: 1, Priority: 5})
	if err != nil {
		return nil, err
	}
	return gen.AdminRunBackup202JSONResponse{JobId: id}, nil
}

// AdminSetBackupDir chooses the daily backup directory on the page (v1.13.9), in every edition.
func (s *Server) AdminSetBackupDir(ctx context.Context, req gen.AdminSetBackupDirRequestObject) (gen.AdminSetBackupDirResponseObject, error) {
	err := s.backupDir.Set(ctx, req.Body.Dir)
	switch {
	case errors.Is(err, backup.ErrDirFromEnv):
		return nil, &apiError{http.StatusConflict, "backup_dir_from_env", err.Error()}
	case errors.Is(err, backup.ErrDirNotAbsolute), errors.Is(err, backup.ErrDirMissing), errors.Is(err, backup.ErrDirNotDir),
		errors.Is(err, backup.ErrDirInData), errors.Is(err, backup.ErrDirNotWritable):
		return nil, &apiError{http.StatusBadRequest, "invalid_backup_dir", err.Error()}
	case err != nil:
		return nil, err
	}
	return gen.AdminSetBackupDir204Response{}, nil
}

func (s *Server) AdminListLogs(_ context.Context, req gen.AdminListLogsRequestObject) (gen.AdminListLogsResponseObject, error) {
	lv := slog.LevelDebug
	if req.Params.Level != nil {
		_ = lv.UnmarshalText([]byte(*req.Params.Level))
	}
	limit := 500
	if req.Params.Limit != nil {
		limit = min(max(*req.Params.Limit, 1), 2000)
	}
	es := logging.Recent.Entries(lv, deref(req.Params.Q), limit)
	out := make(gen.AdminListLogs200JSONResponse, len(es))
	for i, e := range es {
		attrs := map[string]string{}
		for _, a := range e.Attrs {
			attrs[a[0]] = a[1]
		}
		out[i] = gen.LogEntry{Time: e.Time, Level: e.Level, Message: e.Message, Attrs: attrs}
	}
	return out, nil
}
