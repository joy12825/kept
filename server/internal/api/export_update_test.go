package api

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"kept/server/internal/export"
)

// v1.6.5: exporting into a folder again writes only what changed and
// removes what is gone.
func TestExportUpdate(t *testing.T) {
	e := newEnv(t, "auto", nil)
	alice, _, _, homeID := home(t, e)
	up := func(name, text string) float64 {
		return alice.waitJob(alice.upload(name, text, nil).body["jobId"].(float64))["documentId"].(float64)
	}
	up("电费.txt", "国家电网 电费")
	gas := up("燃气.txt", "北京燃气 燃气费")
	dir := filepath.Join(t.TempDir(), "王家")
	run := func() export.Report {
		t.Helper()
		sink, err := export.NewSyncSink(dir)
		if err != nil {
			t.Fatal(err)
		}
		rep, err := e.exp.Run(context.Background(), int64(homeID), export.Options{}, sink)
		if err != nil {
			t.Fatal(err)
		}
		return rep
	}
	if rep := run(); rep.Documents != 2 || rep.Unchanged != 0 {
		t.Fatalf("first: %+v", rep)
	}
	if rep := run(); rep.Documents != 0 || rep.Unchanged != 2 || rep.Removed != 0 {
		t.Errorf("nothing changed: %+v", rep)
	}
	alice.mustDo(http.MethodPatch, "/api/v1/documents/"+itoa(gas), map[string]any{"title": "燃气账单"}, http.StatusOK)
	rep := run()
	if rep.Documents != 1 || rep.Unchanged != 1 || rep.Removed != 1 {
		t.Errorf("after a rename: %+v", rep)
	}
	var names []string
	_ = filepath.Walk(dir, func(p string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() {
			names = append(names, filepath.Base(p))
		}
		return nil
	})
	joined := strings.Join(names, " ")
	if !strings.Contains(joined, "燃气账单") || strings.Contains(joined, " 燃气.txt") || !strings.Contains(joined, "清单.csv") {
		t.Errorf("files: %v", names)
	}
}
