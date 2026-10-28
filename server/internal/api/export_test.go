package api

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"slices"
	"strings"
	"testing"
)

// v1.4.1: the whole space as ordinary files: folders by drawer and year,
// and the lists; only space admins may.
func TestExportWorkspace(t *testing.T) {
	e := newEnv(t, "auto", nil)
	alice, bob, _, homeID := home(t, e)
	doc := alice.waitJob(alice.upload("电费.txt", "国家电网 电费 326.40 元", map[string]string{"createdDate": "2024-03-05"}).body["jobId"].(float64))["documentId"].(float64)
	alice.mustDo(http.MethodPost, "/api/v1/documents/"+itoa(doc)+"/notes", map[string]any{"body": "已缴"}, http.StatusCreated)
	bob.mustDo(http.MethodGet, "/api/v1/export", nil, http.StatusForbidden) // a member, not an admin

	r := alice.do(http.MethodGet, "/api/v1/export", nil)
	if r.code != http.StatusOK || !strings.Contains(r.header.Get("Content-Disposition"), "attachment") {
		t.Fatalf("export: %d %v", r.code, r.header)
	}
	zr, err := zip.NewReader(bytes.NewReader([]byte(r.raw)), int64(len(r.raw)))
	if err != nil {
		t.Fatal(err)
	}
	read := func(name string) string {
		f, err := zr.Open(name)
		if err != nil {
			t.Fatalf("%s: %v (have %v)", name, err, zipNames(zr))
		}
		defer f.Close()
		b, _ := io.ReadAll(f)
		return string(b)
	}
	got := zipNames(zr)
	date := alice.mustDo(http.MethodGet, "/api/v1/documents/"+itoa(doc), nil, http.StatusOK).body["createdDate"].(string)
	file := "未入抽屉/" + date[:4] + "/" + date + " 电费.txt"
	if !slices.Contains(got, file) || !slices.Contains(got, "清单.csv") || !slices.Contains(got, "说明.txt") {
		t.Fatalf("files: %v", got)
	}
	if read(file) != "国家电网 电费 326.40 元" {
		t.Error("the document's file")
	}
	if csv := read("清单.csv"); !strings.HasPrefix(csv, "\xef\xbb\xbf文件,标题,日期") || !strings.Contains(csv, file) || !strings.Contains(csv, "已缴") {
		t.Errorf("清单.csv:\n%s", csv)
	}
	var list []map[string]any
	if err := json.Unmarshal([]byte(read("清单.json")), &list); err != nil || len(list) != 1 || list[0]["title"] != "电费" || len(list[0]["notes"].([]any)) != 1 {
		t.Errorf("清单.json: %v %v", list, err)
	}
	// a download link carries the space in the query, not a header
	if q := personalOf(alice).do(http.MethodGet, "/api/v1/export?file=original&workspace="+itoa(homeID), nil); q.code != http.StatusOK || !strings.Contains(q.raw, "电费.txt") {
		t.Errorf("by query: %d", q.code)
	}
	if !slices.Contains(actionsOf(alice.mustDo(http.MethodGet, "/api/v1/audit", nil, http.StatusOK)), "workspace.export") {
		t.Error("an export should be in the activity log")
	}
}

func zipNames(zr *zip.Reader) []string {
	var out []string
	for _, f := range zr.File {
		out = append(out, f.Name)
	}
	return out
}
