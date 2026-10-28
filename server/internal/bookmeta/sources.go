package bookmeta

import (
	"context"
	"net/url"
	"strings"
)

// openLibrary uses the Books API:
// https://openlibrary.org/dev/docs/api/books (jscmd=data).
type openLibrary struct {
	c    *Client
	base string
}

func (s *openLibrary) Name() string { return "openlibrary" }

func (s *openLibrary) Lookup(ctx context.Context, isbn string) (*Meta, error) {
	var resp map[string]struct {
		Title         string `json:"title"`
		Subtitle      string `json:"subtitle"`
		NumberOfPages int    `json:"number_of_pages"`
		PublishDate   string `json:"publish_date"`
		Authors       []struct {
			Name string `json:"name"`
		} `json:"authors"`
		Publishers []struct {
			Name string `json:"name"`
		} `json:"publishers"`
		Cover struct {
			Large  string `json:"large"`
			Medium string `json:"medium"`
		} `json:"cover"`
		Notes any `json:"notes"` // a string or {"value": …}
	}
	u := s.base + "/api/books?" + url.Values{"bibkeys": {"ISBN:" + isbn}, "format": {"json"}, "jscmd": {"data"}}.Encode()
	if err := s.c.getJSON(ctx, u, &resp); err != nil {
		return nil, err
	}
	b, ok := resp["ISBN:"+isbn]
	if !ok || b.Title == "" {
		return nil, ErrNotFound
	}
	m := &Meta{Title: b.Title, Subtitle: b.Subtitle, Pages: b.NumberOfPages, Published: b.PublishDate, CoverURL: b.Cover.Large}
	if m.CoverURL == "" {
		m.CoverURL = b.Cover.Medium
	}
	for _, a := range b.Authors {
		m.Authors = append(m.Authors, a.Name)
	}
	if len(b.Publishers) > 0 {
		m.Publisher = b.Publishers[0].Name
	}
	switch n := b.Notes.(type) {
	case string:
		m.Description = n
	case map[string]any:
		m.Description, _ = n["value"].(string)
	}
	return m, nil
}

// googleBooks uses the Books API volumes search by ISBN.
type googleBooks struct {
	c    *Client
	base string
	key  string
}

func (s *googleBooks) Name() string { return "googlebooks" }

func (s *googleBooks) Lookup(ctx context.Context, isbn string) (*Meta, error) {
	var resp struct {
		TotalItems int `json:"totalItems"`
		Items      []struct {
			VolumeInfo struct {
				Title         string   `json:"title"`
				Subtitle      string   `json:"subtitle"`
				Authors       []string `json:"authors"`
				Publisher     string   `json:"publisher"`
				PublishedDate string   `json:"publishedDate"`
				Description   string   `json:"description"`
				PageCount     int      `json:"pageCount"`
				Language      string   `json:"language"`
				ImageLinks    struct {
					Thumbnail string `json:"thumbnail"`
				} `json:"imageLinks"`
			} `json:"volumeInfo"`
		} `json:"items"`
	}
	q := url.Values{"q": {"isbn:" + isbn}}
	if s.key != "" {
		q.Set("key", s.key)
	}
	if err := s.c.getJSON(ctx, s.base+"/books/v1/volumes?"+q.Encode(), &resp); err != nil {
		return nil, err
	}
	if resp.TotalItems == 0 || len(resp.Items) == 0 || resp.Items[0].VolumeInfo.Title == "" {
		return nil, ErrNotFound
	}
	v := resp.Items[0].VolumeInfo
	m := &Meta{Title: v.Title, Subtitle: v.Subtitle, Authors: v.Authors, Publisher: v.Publisher, Published: v.PublishedDate,
		Description: v.Description, Pages: v.PageCount, Language: language(v.Language)}
	if t := v.ImageLinks.Thumbnail; t != "" {
		t = strings.Replace(t, "http://", "https://", 1)
		t = strings.Replace(t, "&edge=curl", "", 1)
		m.CoverURL = t
	}
	return m, nil
}

// custom is the deployer's endpoint (KEPT_BOOK_LOOKUP_URL).
type custom struct {
	c   *Client
	url string
}

func (s *custom) Name() string { return "custom" }

func (s *custom) Lookup(ctx context.Context, isbn string) (*Meta, error) {
	var resp struct {
		Title       string   `json:"title"`
		Subtitle    string   `json:"subtitle"`
		Authors     []string `json:"authors"`
		Publisher   string   `json:"publisher"`
		Published   string   `json:"published"`
		Pages       int      `json:"pages"`
		Language    string   `json:"language"`
		Description string   `json:"description"`
		Series      string   `json:"series"`
		Cover       string   `json:"cover"`
	}
	if err := s.c.getJSON(ctx, strings.ReplaceAll(s.url, "{isbn}", isbn), &resp); err != nil {
		return nil, err
	}
	if resp.Title == "" {
		return nil, ErrNotFound
	}
	return &Meta{Title: resp.Title, Subtitle: resp.Subtitle, Authors: resp.Authors, Publisher: resp.Publisher, Published: resp.Published,
		Pages: resp.Pages, Language: language(resp.Language), Description: resp.Description, Series: resp.Series, CoverURL: resp.Cover}, nil
}
