package demo

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"strings"
	"time"

	"github.com/go-pdf/fpdf"

	"kept/server/internal/booklists"
	"kept/server/internal/books"
	"kept/server/internal/epubtest"
	"kept/server/internal/ingest"
	"kept/server/internal/pinyin"
	"kept/server/internal/store/sqlc"
	"kept/server/internal/tenant"
)

// The v0.4 samples of the family workspace: the 书房 and a shelf of
// classics, each a one-page cover PDF with its catalog entry. The ISBNs
// are well-formed but made up.

type bookData struct {
	title, publisher, isbn, published, series, description, date string
	authors                                                      []string
	pages                                                        int
	cover                                                        color.RGBA // set: a separate cover image in this colour
	// noPublisher: the cover shows the publisher, the catalog leaves it
	// out (the library assistant suggests it, stage 33).
	noPublisher bool
}

var familyBooks = []bookData{
	{title: "红楼梦", authors: []string{"曹雪芹", "高鹗"}, publisher: "人民文学出版社", isbn: "9787020002207", published: "1996-12", pages: 1606,
		series: "中国古典文学读本丛书", date: "2024-02-18", cover: color.RGBA{R: 142, G: 36, B: 40, A: 255}, description: "以贾、史、王、薛四大家族的兴衰为背景，写宝玉、黛玉、宝钗之间的爱情与家族的没落。"},
	{title: "西游记", authors: []string{"吴承恩"}, publisher: "人民文学出版社", isbn: "9787020002214", published: "1980-05", pages: 1206,
		series: "中国古典文学读本丛书", date: "2024-03-09", cover: color.RGBA{R: 38, G: 84, B: 124, A: 255}, description: "唐僧师徒四人西天取经，一路降妖除魔，历经九九八十一难。"},
	{title: "三国演义", authors: []string{"罗贯中"}, publisher: "人民文学出版社", isbn: "9787020002221", published: "1973-12", pages: 1186,
		series: "中国古典文学读本丛书", date: "2024-05-01", description: "从黄巾起义写到三分归晋，魏、蜀、吴三国之间的征战与谋略。"},
	{title: "水浒传", authors: []string{"施耐庵", "罗贯中"}, publisher: "人民文学出版社", isbn: "9787020002238", published: "1975-10", pages: 1384,
		series: "中国古典文学读本丛书", date: "2024-06-22", description: "一百单八将聚义梁山泊，从起义到受招安的故事。"},
	{title: "呐喊", authors: []string{"鲁迅"}, publisher: "人民文学出版社", isbn: "9787020002245", published: "1973-03", pages: 186,
		date: "2024-08-15", description: "短篇小说集，收《狂人日记》《孔乙己》《故乡》《阿Q正传》等十四篇。"},
	{title: "朝花夕拾", authors: []string{"鲁迅"}, publisher: "人民文学出版社", isbn: "9787020002252", published: "1973-03", pages: 102,
		date: "2024-09-03", description: "回忆童年与青年时代的散文集，原名《旧事重提》。", noPublisher: true},
}

// coverPDF renders a plain cover: the title, the authors and the publisher.
func coverPDF(b bookData) ([]byte, error) {
	p := fpdf.New("P", "mm", "A5", "")
	p.SetTitle(b.title, true)
	p.SetCreator("收好 演示数据", true)
	fixed := time.Date(2024, 10, 8, 9, 0, 0, 0, time.UTC)
	p.SetCreationDate(fixed)
	p.SetModificationDate(fixed)
	p.AddUTF8FontFromBytes("noto", "", fontData)
	p.SetMargins(16, 20, 16)
	p.SetAutoPageBreak(false, 0)
	p.AddPage()
	w, h := p.GetPageSize()
	p.SetFillColor(246, 240, 228)
	p.Rect(0, 0, w, h, "F")
	p.SetFillColor(156, 60, 40)
	p.Rect(0, 0, 10, h, "F")

	p.SetY(48)
	p.SetFont("noto", "", 34)
	p.SetTextColor(40, 34, 30)
	p.MultiCell(0, 16, b.title, "", "C", false)
	p.Ln(10)
	p.SetFont("noto", "", 14)
	p.SetTextColor(90, 80, 70)
	p.MultiCell(0, 8, strings.Join(b.authors, "　")+" 著", "", "C", false)
	if b.series != "" {
		p.Ln(4)
		p.SetFont("noto", "", 11)
		p.MultiCell(0, 7, b.series, "", "C", false)
	}
	p.SetY(h - 30)
	p.SetFont("noto", "", 12)
	p.SetTextColor(90, 80, 70)
	p.CellFormat(0, 8, b.publisher, "", 1, "C", false, 0, "")
	p.SetFont("noto", "", 9)
	p.CellFormat(0, 6, "ISBN "+b.isbn, "", 1, "C", false, 0, "")

	var buf bytes.Buffer
	err := p.Output(&buf)
	return buf.Bytes(), err
}

// demoEPUB is the sample e-book: a few Tang poems (public domain), which
// ingest turns into a book on its own.
var demoEPUB = epubtest.Book{
	Title: "唐诗选读", Authors: []string{"李白", "杜甫", "王维"}, Publisher: "收好演示出版社", Date: "2020-05", Language: "zh-CN",
	ISBN: "9787020002269", Description: "选录唐代名家诗作若干首，供演示电子书入库与阅读。",
	Chapters: []epubtest.Chapter{
		{Title: "静夜思（李白）", Paras: []string{"床前明月光，疑是地上霜。", "举头望明月，低头思故乡。"}},
		{Title: "春望（杜甫）", Paras: []string{"国破山河在，城春草木深。", "感时花溅泪，恨别鸟惊心。", "烽火连三月，家书抵万金。", "白头搔更短，浑欲不胜簪。"}},
		{Title: "山居秋暝（王维）", Paras: []string{"空山新雨后，天气晚来秋。", "明月松间照，清泉石上流。", "竹喧归浣女，莲动下渔舟。", "随意春芳歇，王孙自可留。"}},
	},
}

// coverImage is a plain cover in the book's colour with a light band, as
// a PNG (the standard library draws no text; the PDF carries the title).
func coverImage(c color.RGBA) []byte {
	const w, h = 400, 600
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	band := color.RGBA{R: 238, G: 226, B: 200, A: 255}
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			px := c
			if (y > 150 && y < 330 && x > 40 && x < w-40) || x < 14 {
				px = band
			}
			img.SetRGBA(x, y, px)
		}
	}
	var buf bytes.Buffer
	_ = png.Encode(&buf, img)
	return buf.Bytes()
}

// loadBooks opens the library and adds the sample books (missing ones
// only). ctx carries the actor for the activity log.
func loadBooks(ctx context.Context, d Deps, scope tenant.Scope, userID int64) (int, error) {
	if _, _, err := d.Drawers.EnableLibrary(ctx, scope); err != nil {
		return 0, err
	}
	q := sqlc.New(d.DB)
	ws := scope.WorkspaceID()
	svc := books.NewService(d.DB, nil, d.Drawers, d.Files)
	added := 0
	for _, b := range familyBooks {
		if id, err := q.FindDocumentByTitle(ctx, sqlc.FindDocumentByTitleParams{WorkspaceID: ws, Title: b.title}); err == nil {
			// Loaded before covers existed (v0.4 stage 23): add the cover.
			if bk, err := q.GetBook(ctx, sqlc.GetBookParams{WorkspaceID: ws, DocumentID: id}); err == nil && bk.Cover == 0 && b.cover.A != 0 && d.Files != nil {
				if err := svc.SaveCover(ctx, scope, id, coverImage(b.cover)); err != nil {
					return added, fmt.Errorf("%s: %w", b.title, err)
				}
			}
			continue
		} else if !errors.Is(err, sql.ErrNoRows) {
			return added, err
		}
		pdf, err := coverPDF(b)
		if err != nil {
			return added, err
		}
		id, err := d.Ingest.Import(ctx, scope, b.title+".pdf", bytes.NewReader(pdf), ingest.Overrides{}, ingest.Upload{})
		if err != nil {
			return added, fmt.Errorf("%s: %w", b.title, err)
		}
		if err := q.SetDocumentMeta(ctx, sqlc.SetDocumentMetaParams{
			Title: b.title, TitleSort: pinyin.TitleKey(b.title), CreatedDate: b.date, ModifiedAt: time.Now().UnixMilli(), WorkspaceID: ws, ID: id,
		}); err != nil {
			return added, err
		}
		publisher := b.publisher
		if b.noPublisher {
			publisher = ""
		}
		if _, err := svc.Set(ctx, scope, id, books.Input{ISBN: b.isbn, Authors: b.authors, Publisher: publisher, Series: b.series,
			Published: b.published, Pages: b.pages, Language: "中文", Description: b.description}); err != nil {
			return added, fmt.Errorf("%s: %w", b.title, err)
		}
		if b.cover.A != 0 && d.Files != nil {
			if err := svc.SaveCover(ctx, scope, id, coverImage(b.cover)); err != nil {
				return added, fmt.Errorf("%s: %w", b.title, err)
			}
		}
		added++
	}
	// The e-book: ingest fills in its catalog entry and cover.
	if _, err := q.FindDocumentByTitle(ctx, sqlc.FindDocumentByTitleParams{WorkspaceID: ws, Title: demoEPUB.Title}); errors.Is(err, sql.ErrNoRows) {
		book := demoEPUB
		book.Cover = coverImage(color.RGBA{R: 46, G: 110, B: 86, A: 255})
		id, err := d.Ingest.Import(ctx, scope, "唐诗选读.epub", bytes.NewReader(epubtest.Build(book)), ingest.Overrides{}, ingest.Upload{})
		if err != nil {
			return added, fmt.Errorf("%s: %w", demoEPUB.Title, err)
		}
		if err := q.SetDocumentMeta(ctx, sqlc.SetDocumentMetaParams{
			Title: demoEPUB.Title, TitleSort: pinyin.TitleKey(demoEPUB.Title), CreatedDate: "2024-10-01", ModifiedAt: time.Now().UnixMilli(), WorkspaceID: ws, ID: id,
		}); err != nil {
			return added, err
		}
		if err := d.Drawers.PlaceDocument(ctx, d.DB, ws, id); err != nil {
			return added, err
		}
		added++
	} else if err != nil {
		return added, err
	}
	// Reading positions (stage 26): the e-book open at 春望, 红楼梦 read
	// to its last page. Existing positions are kept.
	for title, pos := range map[string]struct {
		position string
		percent  float64
	}{demoEPUB.Title: {"epubcfi(/6/4!/4/2/1:0)", 0.4}, "红楼梦": {"page:1", 1}} {
		id, err := q.FindDocumentByTitle(ctx, sqlc.FindDocumentByTitleParams{WorkspaceID: ws, Title: title})
		if err != nil {
			continue
		}
		if _, err := q.GetReadingProgress(ctx, sqlc.GetReadingProgressParams{UserID: userID, DocumentID: id, WorkspaceID: ws}); err == nil {
			continue
		}
		if err := q.UpsertReadingProgress(ctx, sqlc.UpsertReadingProgressParams{UserID: userID, DocumentID: id, WorkspaceID: ws,
			Position: pos.position, Percent: pos.percent, Now: time.Now().UnixMilli()}); err != nil {
			return added, err
		}
	}
	// Reading status and highlights (stage 27), for books not marked yet.
	for _, st := range []struct {
		title, status, started, finished string
		rating                           int64
	}{
		{"红楼梦", "finished", "2024-03-01", "2024-05-20", 5},
		{demoEPUB.Title, "reading", "2024-10-01", "", 0},
		{"西游记", "want", "", "", 0},
	} {
		id, err := q.FindDocumentByTitle(ctx, sqlc.FindDocumentByTitleParams{WorkspaceID: ws, Title: st.title})
		if err != nil {
			continue
		}
		if p, err := q.GetReadingProgress(ctx, sqlc.GetReadingProgressParams{UserID: userID, DocumentID: id, WorkspaceID: ws}); err == nil && p.Status != "" {
			continue
		}
		if err := q.UpsertReadingState(ctx, sqlc.UpsertReadingStateParams{UserID: userID, DocumentID: id, WorkspaceID: ws, Status: st.status,
			Rating: st.rating, StartedOn: st.started, FinishedOn: st.finished, Now: time.Now().UnixMilli()}); err != nil {
			return added, err
		}
	}
	if id, err := q.FindDocumentByTitle(ctx, sqlc.FindDocumentByTitleParams{WorkspaceID: ws, Title: demoEPUB.Title}); err == nil {
		if n, err := q.CountHighlights(ctx, sqlc.CountHighlightsParams{UserID: userID, DocumentID: id, WorkspaceID: ws}); err == nil && n == 0 {
			for _, h := range []struct{ locator, quote, note, color string }{
				{"epubcfi(/6/2!/4/4,/1:0,/1:5)", "床前明月光", "", "yellow"},
				{"epubcfi(/6/4!/4/6,/1:0,/1:5)", "感时花溅泪", "杜甫写于安史之乱中的长安。", "pink"},
			} {
				if _, err := q.InsertHighlight(ctx, sqlc.InsertHighlightParams{UserID: userID, DocumentID: id, WorkspaceID: ws, Locator: h.locator,
					Quote: h.quote, Note: h.note, Color: h.color, Now: time.Now().UnixMilli()}); err != nil {
					return added, err
				}
			}
		}
	}
	// A book list (stage 28): the four classics, each with a line.
	if _, err := q.FindBookListByName(ctx, sqlc.FindBookListByNameParams{WorkspaceID: ws, Name: "四大名著"}); errors.Is(err, sql.ErrNoRows) {
		lists := booklists.NewService(d.DB)
		l, err := lists.Create(ctx, scope, "四大名著", "中国古典小说的四座高峰，按成书先后排。")
		if err != nil {
			return added, err
		}
		for _, it := range []struct{ title, note string }{
			{"三国演义", "七分事实，三分虚构"},
			{"水浒传", "一百单八将，各有各的来路"},
			{"西游记", "适合和孩子一起读"},
			{"红楼梦", "前八十回值得反复读"},
		} {
			id, err := q.FindDocumentByTitle(ctx, sqlc.FindDocumentByTitleParams{WorkspaceID: ws, Title: it.title})
			if err != nil {
				continue
			}
			if err := lists.Add(ctx, scope, l.ID, id, it.note); err != nil {
				return added, err
			}
		}
	} else if err != nil {
		return added, err
	}
	return added, nil
}
