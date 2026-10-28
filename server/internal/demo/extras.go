package demo

import (
	"bytes"
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"errors"
	"strconv"
	"strings"
	"time"

	"kept/server/internal/dedupe"
	"kept/server/internal/ingest"
	"kept/server/internal/pinyin"
	"kept/server/internal/sealbox"
	"kept/server/internal/storage"
	"kept/server/internal/store/sqlc"
	"kept/server/internal/tenant"
)

// The v0.3 samples of the family workspace: the lease got a second
// version, it is shared by a link, and an invoice was filed twice (the
// e-invoice and a scan), which the near-duplicate check finds.

const leaseTitle = "房屋租赁合同"

var jdInvoice = `电子发票（普通发票）
发票代码：011002300111 发票号码：24871634
开票日期：2023年09月11日 校验码：53120 66871 20914 77305
购买方 名称：王小明
销售方 名称：北京京东世纪贸易有限公司 纳税人识别号：911101087921245832
货物或应税劳务名称：*计算机外部设备*显示器 27英寸 4K 单位：台 数量：1 单价：1680.53 金额：1680.53 税率：13% 税额：218.47
价税合计（大写）壹仟捌佰玖拾玖圆整（小写）1899.00
收款人：张三 复核：李四 开票人：王五
本文件为“收好”的演示数据，仅用于演示。`

// jdScan is the same invoice as OCR reads a phone scan.
var jdScan = strings.NewReplacer("：", ":", "（", "(", "）", ")", "圆整", "园整", "\n价税", " 价税").Replace(jdInvoice)

type extrasReport struct {
	shares, versions, duplicates, agentRuns int
	recoveryCode                            string // of the demo key (v1.8.1)
}

func loadExtras(ctx context.Context, d Deps, userID int64, scope tenant.Scope) (extrasReport, error) {
	var rep extrasReport
	q := sqlc.New(d.DB)
	ws := scope.WorkspaceID()
	lease, err := q.FindDocumentByTitle(ctx, sqlc.FindDocumentByTitleParams{WorkspaceID: ws, Title: leaseTitle})
	if errors.Is(err, sql.ErrNoRows) {
		return rep, nil // documents were skipped
	}
	if err != nil {
		return rep, err
	}

	// A second version: the lease renewed a year later.
	vs, err := d.Ingest.Versions(ctx, scope, lease)
	if err != nil {
		return rep, err
	}
	if len(vs) == 1 {
		pdf, err := samplePDF(docData{title: leaseTitle, docType: "合同", correspondent: "链家", date: "2024-09-01"})
		if err != nil {
			return rep, err
		}
		if _, err := d.Ingest.AddVersion(ctx, scope, lease, leaseTitle+"-2024.pdf", bytes.NewReader(pdf), "2024 年续签"); err != nil {
			return rep, err
		}
		rep.versions = 1
	}

	// A link to the lease, valid for a month.
	n, err := q.CountActiveShareLinks(ctx, sqlc.CountActiveShareLinksParams{WorkspaceID: ws, DocumentID: lease, Now: time.Now().UnixMilli()})
	if err != nil {
		return rep, err
	}
	if n == 0 {
		b := make([]byte, 24)
		if _, err := rand.Read(b); err != nil {
			return rep, err
		}
		now := time.Now()
		if _, err := q.InsertShareLink(ctx, sqlc.InsertShareLinkParams{WorkspaceID: ws, DocumentID: lease, Token: base64.RawURLEncoding.EncodeToString(b),
			File: "archive", ExpiresAt: now.AddDate(0, 0, 30).UnixMilli(), CreatedBy: sql.NullInt64{Int64: userID, Valid: true}, CreatedAt: now.UnixMilli(),
			AllowDownload: 1}); err != nil {
			return rep, err
		}
		rep.shares = 1
	}
	// And one for the bank (v1.9.2): watermarked, to be looked at only.
	var protected int
	if err := d.DB.QueryRowContext(ctx, `SELECT count(*) FROM share_links WHERE workspace_id = ? AND document_id = ? AND watermark <> ''`, ws, lease).Scan(&protected); err != nil {
		return rep, err
	}
	if protected == 0 {
		b := make([]byte, 24)
		if _, err := rand.Read(b); err != nil {
			return rep, err
		}
		now := time.Now()
		if _, err := q.InsertShareLink(ctx, sqlc.InsertShareLinkParams{WorkspaceID: ws, DocumentID: lease, Token: base64.RawURLEncoding.EncodeToString(b),
			File: "archive", ExpiresAt: now.AddDate(0, 0, 14).UnixMilli(), CreatedBy: sql.NullInt64{Int64: userID, Valid: true}, CreatedAt: now.UnixMilli(),
			Watermark: "仅供招商银行办理房贷使用", AllowDownload: 0}); err != nil {
			return rep, err
		}
		rep.shares++
	}

	// A photo shared with a watermark drawn on it (v1.11.2).
	if n, err := demoIDCard(ctx, d, scope, userID); err != nil {
		return rep, err
	} else {
		rep.shares += n
	}

	// A bundle of the work receipts for reimbursement (v1.4.2).
	if err := demoBundle(ctx, d.DB, q, ws, userID); err != nil {
		return rep, err
	}
	if err := demoErrands(ctx, d.DB, ws, userID); err != nil {
		return rep, err
	}
	if err := demoFamily(ctx, d.DB, ws); err != nil {
		return rep, err
	}
	if err := demoSafekeeping(ctx, d.DB, ws, userID); err != nil {
		return rep, err
	}
	if err := demoPrivate(ctx, d.DB, ws, userID); err != nil {
		return rep, err
	}
	if rep.recoveryCode, err = demoKey(ctx, d.DB, ws, userID); err != nil {
		return rep, err
	}
	// v1.6.5: bills are kept three years; older ones show up in 可以清理
	if _, err := d.DB.ExecContext(ctx, `INSERT INTO type_retention (document_type_id, workspace_id, months)
		SELECT id, workspace_id, 36 FROM document_types WHERE workspace_id = ? AND name = '账单' ON CONFLICT (document_type_id) DO NOTHING`, ws); err != nil {
		return rep, err
	}

	// The invoice filed twice, then the check that finds it.
	for _, f := range []struct{ name, text string }{{"京东发票-显示器（电子版）.txt", jdInvoice}, {"京东发票-显示器（扫描件）.txt", jdScan}} {
		title := strings.TrimSuffix(f.name, ".txt")
		if _, err := q.FindDocumentByTitle(ctx, sqlc.FindDocumentByTitleParams{WorkspaceID: ws, Title: title}); err == nil {
			continue
		}
		if _, err := d.Ingest.Import(ctx, scope, f.name, strings.NewReader(f.text), ingest.Overrides{}, ingest.Upload{}); err != nil {
			return rep, err
		}
	}
	res, err := dedupe.NewService(d.DB, dedupe.Options{}).Scan(ctx, ws)
	if err != nil {
		return rep, err
	}
	rep.duplicates = res.Open
	if rep.agentRuns, err = loadAgentDemo(ctx, d, ws); err != nil {
		return rep, err
	}
	return rep, nil
}

// warrantyTitle is the inbox document the sample intake run worked on: it
// filled in the due date (a year of warranty). AI stays off in the demo,
// so the run is a record, not something that runs.
const warrantyTitle = "空气净化器保修卡"

func loadAgentDemo(ctx context.Context, d Deps, ws int64) (int, error) {
	q := sqlc.New(d.DB)
	var n int
	if err := d.DB.QueryRowContext(ctx, `SELECT count(*) FROM agent_runs WHERE workspace_id = ?`, ws).Scan(&n); err != nil || n > 0 {
		return 0, err
	}
	doc, err := q.FindDocumentByTitle(ctx, sqlc.FindDocumentByTitleParams{WorkspaceID: ws, Title: warrantyTitle})
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	var due int64
	if err := d.DB.QueryRowContext(ctx, `SELECT id FROM custom_fields WHERE workspace_id = ? AND key = 'due_date'`, ws).Scan(&due); err != nil {
		return 0, err
	}
	now := time.Now()
	var run int64
	if err := d.DB.QueryRowContext(ctx, `INSERT INTO agent_runs (workspace_id, agent, trigger, document_id, model, status, calls, input_tokens, output_tokens, duration_ms, summary, created_at)
		VALUES (?, 'intake', 'added', ?, 'qwen2.5:7b（演示）', 'done', 1, 1186, 142, 3120, '补充了到期日', ?) RETURNING id`, ws, doc, now.Add(-2*time.Hour).UnixMilli()).Scan(&run); err != nil {
		return 0, err
	}
	if _, err := d.DB.ExecContext(ctx, `INSERT INTO agent_steps (run_id, seq, kind, input, output) VALUES (?, 1, 'model', '（演示数据，没有调用模型）', ?)`,
		run, `{"date":{"value":"2024-09-26","confidence":0.82,"basis":"购买日期"},"fields":[{"name":"到期日","value":"2025-09-26","confidence":0.71,"basis":"一年免费保修"}]}`); err != nil {
		return 0, err
	}
	if _, err := d.DB.ExecContext(ctx, `INSERT INTO custom_field_values (document_id, field_id, value_date) VALUES (?, ?, '2025-09-26')
		ON CONFLICT (document_id, field_id) DO NOTHING`, doc, due); err != nil {
		return 0, err
	}
	payload := `{"fields":{"cf:` + strconv.FormatInt(due, 10) + `":[null,"2025-09-26"]}}`
	if _, err := q.InsertProposal(ctx, sqlc.InsertProposalParams{WorkspaceID: ws, RunID: sql.NullInt64{Int64: run, Valid: true}, Agent: "intake",
		Kind: "set_fields", DocumentID: sql.NullInt64{Int64: doc, Valid: true}, Payload: payload, Confidence: 0.71, Reason: "保修卡写明一年免费保修",
		Status: "applied", CreatedAt: now.Add(-2 * time.Hour).UnixMilli()}); err != nil {
		return 0, err
	}
	return 1, q.BumpIndexRev(ctx, sqlc.BumpIndexRevParams{WorkspaceID: ws, ID: doc})
}

// demoBundle shares three work receipts behind one link, once.
func demoBundle(ctx context.Context, db *sql.DB, q *sqlc.Queries, ws, userID int64) error {
	const title = "2024 年工作报销"
	var n int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM share_bundles WHERE workspace_id = ? AND title = ?`, ws, title).Scan(&n); err != nil || n > 0 {
		return err
	}
	var ids []int64
	for _, t := range []string{"增值税普通发票-打印机", "差旅发票-北京至上海", "增值税专用发票-办公用品"} {
		if doc, err := q.FindDocumentByTitle(ctx, sqlc.FindDocumentByTitleParams{WorkspaceID: ws, Title: t}); err == nil {
			ids = append(ids, doc)
		}
	}
	if len(ids) == 0 {
		return nil
	}
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		return err
	}
	now := time.Now()
	var id int64
	if err := db.QueryRowContext(ctx, `INSERT INTO share_bundles (workspace_id, token, title, file, expires_at, created_by, created_at, access_count, last_access_at)
		VALUES (?, ?, ?, 'archive', ?, ?, ?, 3, ?) RETURNING id`, ws, base64.RawURLEncoding.EncodeToString(b), title, now.AddDate(0, 0, 30).UnixMilli(),
		sql.NullInt64{Int64: userID, Valid: true}, now.UnixMilli(), now.Add(-2*time.Hour).UnixMilli()).Scan(&id); err != nil {
		return err
	}
	for i, doc := range ids {
		if _, err := db.ExecContext(ctx, `INSERT INTO share_bundle_documents (bundle_id, document_id, position) VALUES (?, ?, ?)`, id, doc, i); err != nil {
			return err
		}
	}
	return nil
}

// demoErrands adds a checklist for renting a flat (v1.6.2: some papers are
// there, the household register is missing) and an open upload link for
// the landlord, once each.
func demoErrands(ctx context.Context, db *sql.DB, ws, userID int64) error {
	now := time.Now().UnixMilli()
	user := sql.NullInt64{Int64: userID, Valid: userID != 0}
	var n int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM checklists WHERE workspace_id = ? AND name = '租房'`, ws).Scan(&n); err != nil {
		return err
	}
	if n == 0 {
		items := `[{"id":"a1","label":"劳动合同","filter":{"text":"劳动合同"},"count":1,"note":"证明工作稳定"},` +
			`{"id":"a2","label":"近 3 个月信用卡对账单","filter":{"text":"信用卡对账单"},"count":3},` +
			`{"id":"a3","label":"上一份租赁合同","filter":{"text":"租赁合同"},"count":1},` +
			`{"id":"a4","label":"户口本","filter":{"text":"户口本"},"count":1,"note":"首页和本人页复印件"}]`
		if _, err := db.ExecContext(ctx, `INSERT INTO checklists (workspace_id, name, note, items, created_by, created_at, updated_at) VALUES (?, '租房', '中介要的材料，签约前一天准备好', ?, ?, ?, ?)`,
			ws, items, user, now, now); err != nil {
			return err
		}
	}
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM upload_links WHERE workspace_id = ?`, ws).Scan(&n); err != nil || n > 0 {
		return err
	}
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		return err
	}
	_, err := db.ExecContext(ctx, `INSERT INTO upload_links (workspace_id, token, name, message, max_files, expires_at, created_by, created_at)
		VALUES (?, ?, '请房东上传新的租赁合同', '扫描件或照片都可以，谢谢！', 5, ?, ?, ?)`,
		ws, base64.RawURLEncoding.EncodeToString(b), time.Now().AddDate(0, 0, 14).UnixMilli(), user, now)
	return err
}

// demoFamily adds the family members and where the originals are kept
// (v1.6.3): the child's papers are 乐乐's, the check-ups 爸爸's; the
// identity papers are in the bank's safe deposit box, the rest in two
// file boxes by number. Only documents without a person or a location
// are changed, so a second load leaves edits alone.
func demoFamily(ctx context.Context, db *sql.DB, ws int64) error {
	now := time.Now().UnixMilli()
	people := []struct {
		name   string
		titles []string
	}{
		{"爸爸", []string{"劳动合同", "2024年度体检报告", "2023年度体检报告", "居民身份证复印件", "个税年度汇算回执"}},
		{"妈妈", []string{"居民身份证复印件（配偶）", "门诊医疗票据", "商业医疗保险保单"}},
		{"乐乐", []string{"儿童疫苗接种记录", "幼儿园保育协议", "护照（孩子）"}},
	}
	for _, p := range people {
		if _, err := db.ExecContext(ctx, `INSERT INTO persons (workspace_id, name, sort_key, created_at, updated_at) VALUES (?, ?, ?, ?, ?)
			ON CONFLICT (workspace_id, name) DO NOTHING`, ws, p.name, pinyin.SortKey(p.name), now, now); err != nil {
			return err
		}
		for _, t := range p.titles {
			if _, err := db.ExecContext(ctx, `UPDATE documents SET person_id = (SELECT id FROM persons WHERE workspace_id = ? AND name = ?), index_rev = index_rev + 1
				WHERE workspace_id = ? AND title = ? AND person_id IS NULL`, ws, p.name, ws, t); err != nil {
				return err
			}
		}
	}
	places := []struct {
		name, note string
		from, to   int
	}{
		{"银行保险箱", "招商银行东直门支行，钥匙在书房抽屉", 1, 3},
		{"文件盒 A", "书房柜子第 2 层，2023 年及以前", 4, 50},
		{"文件盒 B", "书房柜子第 2 层，2024 年", 51, 999999},
	}
	for _, p := range places {
		if _, err := db.ExecContext(ctx, `INSERT INTO locations (workspace_id, name, sort_key, note, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?)
			ON CONFLICT (workspace_id, name) DO NOTHING`, ws, p.name, pinyin.SortKey(p.name), p.note, now, now); err != nil {
			return err
		}
		if _, err := db.ExecContext(ctx, `UPDATE documents SET location_id = (SELECT id FROM locations WHERE workspace_id = ? AND name = ?)
			WHERE workspace_id = ? AND asn BETWEEN ? AND ? AND location_id IS NULL AND deleted_at IS NULL`, ws, p.name, ws, p.from, p.to); err != nil {
			return err
		}
	}
	return nil
}

// demoSafekeeping (v1.6.4) marks the identity papers sensitive, and names
// an emergency contact for the health and papers drawers when the instance has an
// account that is not a member (the demo makes no accounts: decision 29).
// Once each.
func demoSafekeeping(ctx context.Context, db *sql.DB, ws, userID int64) error {
	for _, t := range []string{"居民身份证复印件", "户口本首页", "护照（孩子）"} {
		if _, err := db.ExecContext(ctx, `UPDATE documents SET sensitive = 1, index_rev = index_rev + 1 WHERE workspace_id = ? AND title = ? AND sensitive = 0`, ws, t); err != nil {
			return err
		}
	}
	var n int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM emergency_contacts WHERE workspace_id = ?`, ws).Scan(&n); err != nil || n > 0 {
		return err
	}
	var contact int64
	err := db.QueryRowContext(ctx, `SELECT id FROM users WHERE disabled_at IS NULL AND id NOT IN (SELECT user_id FROM workspace_members WHERE workspace_id = ?) ORDER BY id LIMIT 1`, ws).Scan(&contact)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	var drawers []string
	rows, err := db.QueryContext(ctx, `SELECT id FROM drawers WHERE workspace_id = ? AND name IN ('健康', '证件') ORDER BY id`, ws)
	if err != nil {
		return err
	}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		drawers = append(drawers, strconv.FormatInt(id, 10))
	}
	rows.Close()
	if len(drawers) == 0 {
		return nil
	}
	now := time.Now().UnixMilli()
	_, err = db.ExecContext(ctx, `INSERT INTO emergency_contacts (workspace_id, user_id, drawer_ids, wait_hours, created_by, created_at, updated_at) VALUES (?, ?, ?, 72, ?, ?, ?)`,
		ws, contact, "["+strings.Join(drawers, ",")+"]", sql.NullInt64{Int64: userID, Valid: userID != 0}, now, now)
	return err
}

// demoPrivate (v1.8.0): 爸爸 is the loading account, which also looks after
// 乐乐; 爸爸's older check-up is private, and reports become private by
// themselves once they have a family member. Only what is not set yet.
func demoPrivate(ctx context.Context, db *sql.DB, ws, userID int64) error {
	if userID == 0 {
		return nil
	}
	now := time.Now().UnixMilli()
	for _, a := range []struct{ person, level string }{{"爸爸", "self"}, {"乐乐", "guardian"}} {
		if _, err := db.ExecContext(ctx, `INSERT INTO person_access (person_id, user_id, workspace_id, level, created_by, created_at)
			SELECT p.id, ?, ?, ?, ?, ? FROM persons p WHERE p.workspace_id = ? AND p.name = ?
			  AND NOT EXISTS (SELECT 1 FROM person_access a WHERE a.person_id = p.id)
			  AND (? <> 'self' OR NOT EXISTS (SELECT 1 FROM person_access a WHERE a.workspace_id = ? AND a.user_id = ? AND a.level = 'self'))
			ON CONFLICT DO NOTHING`, userID, ws, a.level, userID, now, ws, a.person, a.level, ws, userID); err != nil {
			return err
		}
	}
	if _, err := db.ExecContext(ctx, `UPDATE documents SET private = 1, index_rev = index_rev + 1
		WHERE workspace_id = ? AND title = '2023年度体检报告' AND private = 0 AND private_manual = 0
		  AND person_id IN (SELECT person_id FROM person_access WHERE workspace_id = ?)`, ws, ws); err != nil {
		return err
	}
	_, err := db.ExecContext(ctx, `INSERT INTO type_private (document_type_id, workspace_id)
		SELECT id, workspace_id FROM document_types WHERE workspace_id = ? AND name = '报告' ON CONFLICT (document_type_id) DO NOTHING`, ws)
	return err
}

// demoStorageRecovery (v1.11.0) makes the instance's recovery code for
// encrypted storages, as turning on a storage's encryption the first time
// does; the demo has no remote storage to encrypt. Nothing when there is
// one.
func demoStorageRecovery(ctx context.Context, d Deps) (string, error) {
	if d.Files == nil {
		return "", nil
	}
	svc := storage.NewService(d.DB, d.Files, storage.Options{})
	if has, err := svc.HasRecovery(ctx); err != nil || has {
		return "", err
	}
	return svc.NewRecoveryCode(ctx)
}

// demoKey (v1.8.1) gives 爸爸 a key, so his private documents go to remote
// storages and backups sealed; the recovery code is shown once by
// kept demo load. Nothing when he has one.
func demoKey(ctx context.Context, db *sql.DB, ws, userID int64) (string, error) {
	var person int64
	err := db.QueryRowContext(ctx, `SELECT p.id FROM persons p WHERE p.workspace_id = ? AND p.name = '爸爸'
		AND NOT EXISTS (SELECT 1 FROM person_keys k WHERE k.person_id = p.id)`, ws).Scan(&person)
	if errors.Is(err, sql.ErrNoRows) || userID == 0 {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	k, err := sealbox.NewKeyPair()
	if err != nil {
		return "", err
	}
	code, err := sealbox.NewRecoveryCode()
	if err != nil {
		return "", err
	}
	recovery, err := sealbox.Wrap(code, k.Private)
	if err != nil {
		return "", err
	}
	now := time.Now().UnixMilli()
	_, err = db.ExecContext(ctx, `INSERT INTO person_keys (person_id, workspace_id, public_key, recovery, created_by, created_at, recovery_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		person, ws, base64.RawStdEncoding.EncodeToString(k.Public), recovery, userID, now, now)
	return code, err
}
