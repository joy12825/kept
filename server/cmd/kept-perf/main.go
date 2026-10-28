// Command kept-perf measures how Kept holds up with many documents (v1.13.5).
//
//	kept-perf seed --login <user> --space <name> --docs <n>
//	    adds n documents to a space quickly: rows, tags, a field, drawer
//	    placement and a small text original each, without OCR (the space's
//	    types, correspondents, tags and persons come from `kept demo load`).
//	    Rebuild the search index afterwards with `kept index rebuild`.
//	kept-perf ingest --url <http://host:port> --login <user> --password-file <f> --space <name> --dir <files> [--copies 1]
//	    uploads the files in dir (each copy made unique) through the API
//	    and times until all are in: the full pipeline, OCR included.
//	kept-perf bench --url <http://host:port> --login <user> --password-file <f> --space <name> [--rounds 20]
//	    times the pages people use most against a running server and prints
//	    p50 / p95 / max per request.
//
// Both read the usual KEPT_* settings (seed opens KEPT_DATA_DIR directly,
// so stop the server first). Not for production data.
package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"math/rand/v2"
	"mime/multipart"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"kept/server/internal/app"
	"kept/server/internal/config"
	"kept/server/internal/drawer"
	"kept/server/internal/files"
	"kept/server/internal/pinyin"
	"kept/server/internal/store"
	"kept/server/internal/store/sqlc"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "用法：kept-perf seed|bench …（见源码开头的说明）")
		os.Exit(2)
	}
	ctx := context.Background()
	var err error
	switch os.Args[1] {
	case "seed":
		err = seed(ctx, os.Args[2:])
	case "bench":
		err = bench(ctx, os.Args[2:])
	case "ingest":
		err = ingest(ctx, os.Args[2:])
	default:
		err = fmt.Errorf("未知子命令 %s", os.Args[1])
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// ---------- seed ----------

type taxon struct {
	types, corrs, tags, persons []int64
	typeNames                   map[int64]string
	corrNames                   map[int64]string
	money                       int64 // a monetary custom field, 0 when none
}

func seed(ctx context.Context, args []string) error {
	fl := flag.NewFlagSet("seed", flag.ExitOnError)
	login := fl.String("login", "", "the user the documents are added by")
	space := fl.String("space", "", "the space's name")
	n := fl.Int("docs", 1000, "how many documents to add")
	batch := fl.Int("batch", 500, "documents per transaction")
	_ = fl.Parse(args)
	if *login == "" || *space == "" {
		return fmt.Errorf("需要 --login 和 --space")
	}
	cfg, err := config.Load(os.Getenv)
	if err != nil {
		return err
	}
	st, err := store.Open(ctx, cfg.Database())
	if err != nil {
		return err
	}
	defer st.Close()
	db := st.DB
	fs, err := app.OpenFileStore(ctx, cfg, db)
	if err != nil {
		return err
	}
	var userID, ws int64
	if err := db.QueryRowContext(ctx, `SELECT id FROM users WHERE username = ? OR email = ?`, *login, *login).Scan(&userID); err != nil {
		return fmt.Errorf("找不到账号 %s：%w", *login, err)
	}
	if err := db.QueryRowContext(ctx, `SELECT id FROM workspaces WHERE name = ? AND deleted_at IS NULL`, *space).Scan(&ws); err != nil {
		return fmt.Errorf("找不到空间 %s：%w", *space, err)
	}
	tx, err := loadTaxon(ctx, db, ws)
	if err != nil {
		return err
	}
	q := sqlc.New(db)
	storage, err := q.GetWorkspaceStorage(ctx, ws)
	if err != nil {
		return err
	}
	placer := drawer.NewService(db, drawer.Options{})
	tmp, err := os.MkdirTemp("", "kept-perf-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)

	var start int
	_ = db.QueryRowContext(ctx, `SELECT COUNT(*) FROM documents WHERE workspace_id = ?`, ws).Scan(&start)
	rng := rand.New(rand.NewPCG(uint64(start)+1, 7))
	began := time.Now()
	for done := 0; done < *n; {
		size := min(*batch, *n-done)
		ids, texts, err := insertBatch(ctx, db, placer, ws, userID, storage, tx, rng, start+done, size)
		if err != nil {
			return err
		}
		for i, id := range ids {
			src := filepath.Join(tmp, "doc.txt")
			if err := os.WriteFile(src, []byte(texts[i]), 0o600); err != nil {
				return err
			}
			if err := fs.Put(ctx, storage, files.Original(ws, id, "txt"), src, false); err != nil {
				return err
			}
		}
		done += size
		el := time.Since(began)
		fmt.Printf("已添加 %d / %d 份，用时 %s（%.0f 份/秒）\n", done, *n, el.Round(time.Second), float64(done)/el.Seconds())
	}
	return nil
}

func loadTaxon(ctx context.Context, db *sql.DB, ws int64) (taxon, error) {
	t := taxon{typeNames: map[int64]string{}, corrNames: map[int64]string{}}
	read := func(query string, into *[]int64, names map[int64]string) error {
		rows, err := db.QueryContext(ctx, query, ws)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var id int64
			var name string
			if err := rows.Scan(&id, &name); err != nil {
				return err
			}
			*into = append(*into, id)
			if names != nil {
				names[id] = name
			}
		}
		return rows.Err()
	}
	if err := read(`SELECT id, name FROM document_types WHERE workspace_id = ?`, &t.types, t.typeNames); err != nil {
		return t, err
	}
	if err := read(`SELECT id, name FROM correspondents WHERE workspace_id = ?`, &t.corrs, t.corrNames); err != nil {
		return t, err
	}
	if err := read(`SELECT id, name FROM tags WHERE workspace_id = ?`, &t.tags, nil); err != nil {
		return t, err
	}
	if err := read(`SELECT id, name FROM persons WHERE workspace_id = ?`, &t.persons, nil); err != nil {
		return t, err
	}
	_ = db.QueryRowContext(ctx, `SELECT id FROM custom_fields WHERE workspace_id = ? AND data_type = 'monetary' ORDER BY id LIMIT 1`, ws).Scan(&t.money)
	if len(t.types) == 0 || len(t.corrs) == 0 {
		return t, fmt.Errorf("这个空间还没有类型和往来方，先执行 kept demo load")
	}
	return t, nil
}

var (
	items  = []string{"电费", "水费", "燃气费", "物业费", "宽带", "话费", "停车费", "保险", "体检", "学费", "房租", "维修", "家电", "车辆保养", "药费", "旅行", "书籍", "课程", "快递", "取暖费"}
	places = []string{"北京", "上海", "杭州", "成都", "广州", "南京", "武汉", "西安", "苏州", "天津"}
	words  = []string{"本期", "应缴", "合计", "金额", "日期", "编号", "客户", "地址", "说明", "备注", "有效期", "服务", "项目", "数量", "单价", "开票", "收款", "付款", "确认", "签字"}
)

func insertBatch(ctx context.Context, db *sql.DB, placer *drawer.Service, ws, userID int64, storage string, t taxon, rng *rand.Rand, from, n int) ([]int64, []string, error) {
	sqlTx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return nil, nil, err
	}
	defer sqlTx.Rollback()
	q := sqlc.New(db).WithTx(sqlTx)
	now := time.Now()
	ids := make([]int64, 0, n)
	texts := make([]string, 0, n)
	for i := 0; i < n; i++ {
		k := from + i
		typ := t.types[rng.IntN(len(t.types))]
		corr := t.corrs[rng.IntN(len(t.corrs))]
		item := items[rng.IntN(len(items))]
		day := now.AddDate(0, 0, -rng.IntN(365*12))
		date := day.Format("2006-01-02")
		amount := float64(rng.IntN(500000)) / 100
		title := fmt.Sprintf("%d年%d月%s%s", day.Year(), day.Month(), item, t.typeNames[typ])
		var b strings.Builder
		fmt.Fprintf(&b, "%s\n%s\n%s %s\n", t.corrNames[corr], title, places[rng.IntN(len(places))], date)
		for j := 0; j < 40+rng.IntN(120); j++ {
			b.WriteString(words[rng.IntN(len(words))])
			if j%7 == 6 {
				fmt.Fprintf(&b, " %.2f 元\n", amount)
			}
		}
		// one rare token per document, to time a search that hits one
		fmt.Fprintf(&b, "\n单号 KP%07d\n", k)
		text := b.String()
		var person sql.NullInt64
		if len(t.persons) > 0 && rng.IntN(3) == 0 {
			person = sql.NullInt64{Int64: t.persons[rng.IntN(len(t.persons))], Valid: true}
		}
		inbox := int64(0)
		if rng.IntN(100) == 0 {
			inbox = 1
		}
		id, err := q.InsertDocument(ctx, sqlc.InsertDocumentParams{
			WorkspaceID: ws, Title: title, TitleSort: pinyin.TitleKey(title), Content: text, CreatedDate: date,
			AddedAt: now.Add(-time.Duration(rng.IntN(1000)) * time.Hour).UnixMilli(), ModifiedAt: now.UnixMilli(),
			CorrespondentID: sql.NullInt64{Int64: corr, Valid: true}, DocumentTypeID: sql.NullInt64{Int64: typ, Valid: true},
			MimeType: "text/plain", OriginalFilename: fmt.Sprintf("perf-%07d.txt", k), OriginalExt: "txt", PageCount: 1,
			Checksum: fmt.Sprintf("perf-%d-%07d", ws, k), Inbox: inbox, FieldSources: "{}", Source: "perf", Storage: storage,
			PersonID: person, OriginalSize: sql.NullInt64{Int64: int64(len(text)), Valid: true}, DerivedSize: sql.NullInt64{Valid: true}, AddedBy: sql.NullInt64{Int64: userID, Valid: true},
		})
		if err != nil {
			return nil, nil, err
		}
		for _, tag := range pick(rng, t.tags, rng.IntN(3)) {
			if err := q.AddDocumentTag(ctx, sqlc.AddDocumentTagParams{DocumentID: id, TagID: tag}); err != nil {
				return nil, nil, err
			}
		}
		if t.money != 0 && rng.IntN(2) == 0 {
			if _, err := sqlTx.ExecContext(ctx, `INSERT INTO custom_field_values (document_id, field_id, value_monetary) VALUES (?, ?, ?)`, id, t.money, int64(amount*100)); err != nil {
				return nil, nil, err
			}
		}
		if err := placer.PlaceDocument(ctx, sqlTx, ws, id); err != nil {
			return nil, nil, err
		}
		ids = append(ids, id)
		texts = append(texts, text)
	}
	return ids, texts, sqlTx.Commit()
}

func pick(rng *rand.Rand, xs []int64, n int) []int64 {
	if n > len(xs) {
		n = len(xs)
	}
	out := map[int64]bool{}
	for len(out) < n {
		out[xs[rng.IntN(len(xs))]] = true
	}
	ids := make([]int64, 0, n)
	for id := range out {
		ids = append(ids, id)
	}
	return ids
}

// ---------- bench ----------

type client struct {
	base string
	ws   string
	csrf string
	hc   *http.Client
}

// signIn logs in and picks the space by name.
func signIn(base, login, pwFile, space string) (*client, error) {
	pw, err := os.ReadFile(pwFile)
	if err != nil {
		return nil, err
	}
	jar, _ := cookiejar.New(nil)
	c := &client{base: strings.TrimRight(base, "/"), hc: &http.Client{Jar: jar, Timeout: 2 * time.Minute}}
	body, _ := json.Marshal(map[string]any{"login": login, "password": strings.TrimSpace(string(pw))})
	res, err := c.hc.Post(c.base+"/api/v1/auth/login", "application/json", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("登录失败：%s", res.Status)
	}
	var me struct {
		CSRF string `json:"csrfToken"`
	}
	_ = json.NewDecoder(res.Body).Decode(&me)
	c.csrf = me.CSRF
	var spaces []struct {
		ID   int64  `json:"id"`
		Name string `json:"name"`
	}
	if err := c.getJSON("/api/v1/workspaces", &spaces); err != nil {
		return nil, err
	}
	for _, s := range spaces {
		if s.Name == space {
			c.ws = strconv.FormatInt(s.ID, 10)
		}
	}
	if c.ws == "" {
		return nil, fmt.Errorf("找不到空间 %s", space)
	}
	return c, nil
}

// ---------- ingest ----------

func ingest(ctx context.Context, args []string) error {
	fl := flag.NewFlagSet("ingest", flag.ExitOnError)
	base := fl.String("url", "http://127.0.0.1:8000", "the server")
	login := fl.String("login", "", "user name or e-mail")
	pwFile := fl.String("password-file", "", "a file holding the password")
	space := fl.String("space", "", "the space's name")
	dir := fl.String("dir", "", "the files to upload")
	copies := fl.Int("copies", 1, "times each file is uploaded (made unique)")
	_ = fl.Parse(args)
	c, err := signIn(*base, *login, *pwFile, *space)
	if err != nil {
		return err
	}
	names, err := filepath.Glob(filepath.Join(*dir, "*"))
	if err != nil || len(names) == 0 {
		return fmt.Errorf("%s 里没有文件", *dir)
	}
	count := func() int {
		var v struct {
			Total int `json:"total"`
		}
		_ = c.getJSON("/api/v1/documents?limit=1", &v)
		return v.Total
	}
	before := count()
	began := time.Now()
	sent := 0
	for k := 0; k < *copies; k++ {
		for _, name := range names {
			data, err := os.ReadFile(name)
			if err != nil {
				return err
			}
			// a trailing comment keeps PDFs and pictures readable and the checksum new
			data = append(data, []byte(fmt.Sprintf("\n%%kept-perf %d %d %d\n", time.Now().UnixNano(), k, sent))...)
			var b bytes.Buffer
			mw := multipart.NewWriter(&b)
			fw, _ := mw.CreateFormFile("file", fmt.Sprintf("%d-%s", sent, filepath.Base(name)))
			_, _ = fw.Write(data)
			_ = mw.Close()
			req, _ := http.NewRequest(http.MethodPost, c.base+"/api/v1/documents", &b)
			req.Header.Set("Content-Type", mw.FormDataContentType())
			req.Header.Set("X-Workspace-ID", c.ws)
			req.Header.Set("X-CSRF-Token", c.csrf)
			res, err := c.hc.Do(req)
			if err != nil {
				return err
			}
			res.Body.Close()
			if res.StatusCode/100 != 2 {
				return fmt.Errorf("上传 %s：%s", name, res.Status)
			}
			sent++
		}
	}
	upload := time.Since(began)
	fmt.Printf("上传 %d 份，用时 %s；等待入库完成…\n", sent, upload.Round(time.Millisecond))
	last, lastAt := 0, time.Now()
	for {
		got := count() - before
		if got >= sent {
			el := time.Since(began)
			fmt.Printf("入库完成：%d 份，共 %s，平均每份 %s，每分钟 %.0f 份\n", sent, el.Round(time.Second), (el / time.Duration(sent)).Round(time.Millisecond), float64(sent)/el.Minutes())
			return nil
		}
		if got != last {
			last, lastAt = got, time.Now()
		} else if time.Since(lastAt) > 5*time.Minute {
			return fmt.Errorf("5 分钟没有进展，停在 %d / %d 份（看任务记录里是否有失败）", got, sent)
		}
		time.Sleep(time.Second)
	}
}

// get times one request; hits is the list's total when it has one (-1 otherwise).
func (c *client) get(path string) (d time.Duration, code, hits int, err error) {
	req, _ := http.NewRequest(http.MethodGet, c.base+path, nil)
	req.Header.Set("X-Workspace-ID", c.ws)
	t := time.Now()
	res, err := c.hc.Do(req)
	if err != nil {
		return 0, 0, 0, err
	}
	body, _ := io.ReadAll(res.Body)
	res.Body.Close()
	d = time.Since(t)
	var v struct {
		Total *int `json:"total"`
	}
	hits = -1
	if json.Unmarshal(body, &v) == nil && v.Total != nil {
		hits = *v.Total
	}
	return d, res.StatusCode, hits, nil
}

func bench(ctx context.Context, args []string) error {
	fl := flag.NewFlagSet("bench", flag.ExitOnError)
	base := fl.String("url", "http://127.0.0.1:8000", "the server")
	login := fl.String("login", "", "user name or e-mail")
	pwFile := fl.String("password-file", "", "a file holding the password")
	space := fl.String("space", "", "the space's name")
	rounds := fl.Int("rounds", 20, "times each request runs")
	only := fl.String("only", "", "only the requests whose name contains this")
	_ = fl.Parse(args)
	c, err := signIn(*base, *login, *pwFile, *space)
	if err != nil {
		return err
	}
	var total struct {
		Total int `json:"total"`
	}
	_ = c.getJSON("/api/v1/documents?limit=1", &total)
	var recent struct {
		Items []struct {
			ID int64 `json:"id"`
		} `json:"items"`
	}
	_ = c.getJSON("/api/v1/documents?sort=created&dir=asc&limit=1", &recent)
	var types []struct {
		ID int64 `json:"id"`
	}
	_ = c.getJSON("/api/v1/document-types", &types)
	var drawers struct {
		Items []struct {
			ID int64 `json:"id"`
		} `json:"items"`
	}
	_ = c.getJSON("/api/v1/drawers", &drawers)
	fmt.Printf("空间“%s”：%d 份文档；每个请求 %d 次\n\n", *space, total.Total, *rounds)

	f := func(v any) string { b, _ := json.Marshal(v); return url.QueryEscape(string(b)) }
	year := time.Now().Year() - 3
	cases := [][2]string{
		{"首页：抽屉与要点", "/api/v1/drawers"},
		{"首页：提醒", "/api/v1/alerts"},
		{"首页：最近添加", "/api/v1/documents?sort=added&dir=desc&limit=8"},
		{"待整理", "/api/v1/documents?filter=" + f(map[string]any{"field": "inbox", "op": "eq", "value": true}) + "&sort=added&dir=asc&limit=200"},
		{"全部文件：第一页", "/api/v1/documents?sort=created&dir=desc&limit=50"},
		{"全部文件：第 100 页", "/api/v1/documents?sort=created&dir=desc&limit=50&offset=4950"},
		{"按标题排序", "/api/v1/documents?sort=title&dir=asc&limit=50"},
		{"分面计数（侧栏）", "/api/v1/documents/facets?groups=drawer,type,correspondent,person,tag,location,year,month"},
		{"筛选：一个年份", "/api/v1/documents?filter=" + f(map[string]any{"field": "created", "op": "between", "value": []string{fmt.Sprintf("%d-01-01", year), fmt.Sprintf("%d-12-31", year)}}) + "&limit=50"},
		{"搜索：常见词“电费”", "/api/v1/documents?filter=" + f(map[string]any{"text": "电费"}) + "&sort=relevance&dir=desc&limit=50"},
		{"搜索：没有结果的词", "/api/v1/documents?filter=" + f(map[string]any{"text": "不存在的词语"}) + "&sort=relevance&dir=desc&limit=50"},
		{"搜索：只命中一份的单号", "/api/v1/documents?filter=" + f(map[string]any{"text": "KP0005000"}) + "&sort=relevance&dir=desc&limit=50"},
		{"搜索：两个词", "/api/v1/documents?filter=" + f(map[string]any{"text": "物业费 杭州"}) + "&sort=relevance&dir=desc&limit=50"},
		{"边输边搜（前缀）", "/api/v1/documents?filter=" + f(map[string]any{"text": "燃", "prefix": true}) + "&sort=relevance&dir=desc&limit=8"},
		{"年度回顾", fmt.Sprintf("/api/v1/yearbook/%d", year)},
		{"任务记录", "/api/v1/jobs"},
	}
	if len(recent.Items) > 0 {
		cases = append(cases, [2]string{"文档详情", "/api/v1/documents/" + strconv.FormatInt(recent.Items[0].ID, 10)})
	}
	if len(types) > 0 {
		cases = append(cases, [2]string{"筛选：一个类型", "/api/v1/documents?filter=" + f(map[string]any{"field": "type", "op": "in", "value": []int64{types[0].ID}}) + "&limit=50"})
	}
	if len(drawers.Items) > 0 {
		id := strconv.FormatInt(drawers.Items[0].ID, 10)
		cases = append(cases, [2]string{"抽屉页", "/api/v1/drawers/" + id},
			[2]string{"抽屉里的文档", "/api/v1/documents?filter=" + f(map[string]any{"field": "drawer", "op": "in", "value": []int64{drawers.Items[0].ID}}) + "&limit=50"})
	}
	fmt.Printf("%-24s %8s %8s %8s %8s\n", "请求", "p50", "p95", "最慢", "命中")
	for _, cs := range cases {
		if *only != "" && !strings.Contains(cs[0], *only) {
			continue
		}
		var ds []time.Duration
		hits := -1
		for i := 0; i < *rounds; i++ {
			d, code, h, err := c.get(cs[1])
			hits = h
			if err != nil {
				return err
			}
			if code != http.StatusOK {
				fmt.Printf("%-24s 返回 %d\n", cs[0], code)
				break
			}
			ds = append(ds, d)
		}
		if len(ds) == 0 {
			continue
		}
		sort.Slice(ds, func(i, j int) bool { return ds[i] < ds[j] })
		pct := func(p float64) time.Duration { return ds[min(len(ds)-1, int(p*float64(len(ds))))] }
		hit := ""
		if hits >= 0 {
			hit = strconv.Itoa(hits)
		}
		fmt.Printf("%-24s %8s %8s %8s %8s\n", cs[0], ms(pct(0.5)), ms(pct(0.95)), ms(ds[len(ds)-1]), hit)
	}
	return nil
}

func (c *client) getJSON(path string, v any) error {
	req, _ := http.NewRequest(http.MethodGet, c.base+path, nil)
	if c.ws != "" {
		req.Header.Set("X-Workspace-ID", c.ws)
	}
	res, err := c.hc.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("%s：%s", path, res.Status)
	}
	return json.NewDecoder(res.Body).Decode(v)
}

func ms(d time.Duration) string { return fmt.Sprintf("%.1fms", float64(d.Microseconds())/1000) }
