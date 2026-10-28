package reading

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"kept/server/internal/apperr"
	"kept/server/internal/store"
	"kept/server/internal/tenant"
)

func setup(t *testing.T) *Service {
	t.Helper()
	st, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	for _, q := range []string{
		`INSERT INTO users (id, username, display_name, password_hash, password_changed_at, created_at, updated_at) VALUES (1, 'wang', '小王', 'x', 1, 1, 1), (2, 'li', '小李', 'x', 1, 1, 1)`,
		`INSERT INTO workspaces (id, name, kind, created_at) VALUES (1, 'a', 'family', 1), (2, 'b', 'family', 1)`,
		`INSERT INTO documents (id, workspace_id, title, created_date, added_at, modified_at, mime_type, original_filename, original_ext, checksum)
		 VALUES (1, 1, '唐诗选读', '2024-03-05', 1, 1, 'application/epub+zip', 'a.epub', 'epub', 'a'), (2, 1, '红楼梦', '2024-03-05', 1, 1, 'application/pdf', 'b.pdf', 'pdf', 'b'),
		        (3, 2, '别人的', '2024-03-05', 1, 1, 'application/pdf', 'c.pdf', 'pdf', 'c')`,
	} {
		if _, err := st.DB.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	s := NewService(st.DB)
	s.now = func() time.Time { return time.Date(2024, 10, 8, 9, 0, 0, 0, time.Local) }
	return s
}

func kind(err error) apperr.Kind {
	var ae *apperr.Error
	if errors.As(err, &ae) {
		return ae.Kind
	}
	return 0
}

func TestStateAndProgress(t *testing.T) {
	s := setup(t)
	ctx, scope := context.Background(), tenant.New(1, tenant.RoleViewer)
	want := "want"
	p, err := s.SetState(ctx, scope, 1, 1, StatePatch{Status: &want})
	if err != nil || p.Status != "want" || p.StartedOn != "" {
		t.Fatalf("want = %+v %v", p, err)
	}
	// Opening the book starts reading it, today.
	if p, err = s.Save(ctx, scope, 1, 1, "epubcfi(/6/4!/4/2/1:0)", 0.3); err != nil || p.Status != "reading" || p.StartedOn != "2024-10-08" || p.Position == "" {
		t.Fatalf("save = %+v %v", p, err)
	}
	finished, five := "finished", 5
	if p, err = s.SetState(ctx, scope, 1, 1, StatePatch{Status: &finished, Rating: &five}); err != nil || p.FinishedOn != "2024-10-08" || p.Rating != 5 || p.StartedOn != "2024-10-08" {
		t.Fatalf("finish = %+v %v", p, err)
	}
	// Reading again later does not reopen a finished book.
	if p, _ = s.Save(ctx, scope, 1, 1, "epubcfi(/6/2!/4/2/1:0)", 0.1); p.Status != "finished" {
		t.Errorf("status after reopening = %q", p.Status)
	}
	date := "2024-05-20"
	if p, _ = s.SetState(ctx, scope, 1, 1, StatePatch{FinishedOn: &date}); p.FinishedOn != "2024-05-20" {
		t.Errorf("date = %+v", p)
	}
	// Each person has their own.
	if p, _ := s.Get(ctx, scope, 2, 1); p.Status != "" || p.Position != "" {
		t.Errorf("other user = %+v", p)
	}
	bad, six, badDate := "done", 6, "20240520"
	for _, patch := range []StatePatch{{Status: &bad}, {Rating: &six}, {StartedOn: &badDate}} {
		if _, err := s.SetState(ctx, scope, 1, 1, patch); kind(err) != apperr.KindInvalid {
			t.Errorf("%+v: %v", patch, err)
		}
	}
	if _, err := s.SetState(ctx, scope, 1, 3, StatePatch{Status: &want}); err == nil {
		t.Error("another workspace's document")
	}
}

func TestHighlights(t *testing.T) {
	s := setup(t)
	ctx, scope := context.Background(), tenant.New(1, tenant.RoleViewer)
	a, err := s.AddHighlight(ctx, scope, 1, 1, HighlightInput{Locator: "epubcfi(/6/4!/4/4,/1:0,/1:5)", Quote: "国破山河在", Color: "pink"})
	if err != nil || a.Color != "pink" {
		t.Fatalf("add = %+v %v", a, err)
	}
	if _, err := s.AddHighlight(ctx, scope, 1, 2, HighlightInput{Locator: "page:3", Note: "这一页写得好"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AddHighlight(ctx, scope, 2, 1, HighlightInput{Locator: "page:1", Quote: "李的"}); err != nil {
		t.Fatal(err)
	}
	for _, in := range []HighlightInput{{Quote: "x"}, {Locator: "page:1"}, {Locator: "page:1", Quote: "x", Color: "red"}} {
		if _, err := s.AddHighlight(ctx, scope, 1, 1, in); kind(err) != apperr.KindInvalid {
			t.Errorf("%+v: %v", in, err)
		}
	}
	note := "感时花溅泪"
	if h, err := s.UpdateHighlight(ctx, scope, 1, a.ID, &note, nil); err != nil || h.Note != note || h.Color != "pink" {
		t.Errorf("update = %+v %v", h, err)
	}
	if _, err := s.UpdateHighlight(ctx, scope, 2, a.ID, &note, nil); kind(err) != apperr.KindNotFound {
		t.Errorf("someone else's: %v", err)
	}
	if p, _ := s.Get(ctx, scope, 1, 1); p.Highlights != 1 {
		t.Errorf("count = %d", p.Highlights)
	}

	page, err := s.Search(ctx, scope, 1, SearchQuery{})
	if err != nil || len(page.Items) != 2 || page.Items[0].DocumentTitle != "红楼梦" || page.Items[1].Quote != "国破山河在" {
		t.Fatalf("search = %+v %v", page, err)
	}
	if page, _ = s.Search(ctx, scope, 1, SearchQuery{Text: "花溅"}); len(page.Items) != 1 || page.Items[0].ID != a.ID {
		t.Errorf("by note = %+v", page)
	}
	if page, _ = s.Search(ctx, scope, 1, SearchQuery{Color: "yellow"}); len(page.Items) != 1 || page.Items[0].Locator != "page:3" {
		t.Errorf("by colour = %+v", page)
	}
	if page, _ = s.Search(ctx, scope, 1, SearchQuery{Limit: 1}); len(page.Items) != 1 || page.NextCursor == "" {
		t.Errorf("paging = %+v", page)
	} else if next, _ := s.Search(ctx, scope, 1, SearchQuery{Limit: 1, Cursor: page.NextCursor}); len(next.Items) != 1 || next.Items[0].ID != a.ID {
		t.Errorf("next page = %+v", next)
	}
	if page, _ = s.Search(ctx, scope, 1, SearchQuery{Text: "100%_"}); len(page.Items) != 0 {
		t.Errorf("wildcards are literal: %+v", page)
	}
	if err := s.DeleteHighlight(ctx, scope, 1, a.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteHighlight(ctx, scope, 1, a.ID); kind(err) != apperr.KindNotFound {
		t.Errorf("twice: %v", err)
	}
}
