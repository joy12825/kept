package storage

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"kept/server/internal/files"
	"kept/server/internal/secret"
	"kept/server/internal/store"
)

// TestLoadSaved: storages added on the page come back on the next start;
// with another master key, one with a secret key is listed with an error.
func TestLoadSaved(t *testing.T) {
	ctx := context.Background()
	data := t.TempDir()
	st, err := store.Open(ctx, filepath.Join(data, "app.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	box, err := secret.Load(data, "")
	if err != nil {
		t.Fatal(err)
	}
	svc := NewService(st.DB, files.NewStore(data), Options{Box: box, DataDir: data})
	if err := svc.Create(ctx, Input{Name: "disk2", Kind: "dir", Settings: Settings{Path: t.TempDir()}, Selectable: true}); err != nil {
		t.Fatal(err)
	}
	if err := svc.Create(ctx, Input{Name: "cloud", Kind: "s3", Settings: Settings{Endpoint: "http://127.0.0.1:1", Bucket: "b", AccessKey: "ak"},
		SecretKey: "sk", Selectable: false}); err != nil {
		t.Fatal(err)
	}
	if err := svc.Create(ctx, Input{Name: "inside", Kind: "dir", Settings: Settings{Path: filepath.Join(data, "media", "x")}}); err == nil {
		t.Error("a directory inside the data directory accepted")
	}

	// The next start, same key.
	fs := files.NewStore(data)
	again := NewService(st.DB, fs, Options{Box: box, DataDir: data})
	if err := again.LoadSaved(ctx); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(fs.Storages(), ","); got != "local,cloud,disk2" {
		t.Errorf("storages = %s", got)
	}
	choices, _ := again.Choices(ctx)
	if len(choices) != 2 || choices[1].Name != "disk2" { // cloud is not selectable
		t.Errorf("choices = %+v", choices)
	}

	// Another master key: cloud cannot be set up, disk2 (no key) can.
	other, _ := secret.Load(t.TempDir(), "")
	fs = files.NewStore(data)
	lost := NewService(st.DB, fs, Options{Box: other, DataDir: data})
	if err := lost.LoadSaved(ctx); err != nil {
		t.Fatal(err)
	}
	list, err := lost.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var cloud Info
	for _, s := range list {
		if s.Name == "cloud" {
			cloud = s
		}
	}
	if cloud.Source != SourcePage || cloud.Error == "" || cloud.Settings == nil || cloud.Settings.Bucket != "b" {
		t.Errorf("cloud = %+v", cloud)
	}
	if _, err := lost.Test(ctx, "cloud"); err == nil || !strings.Contains(err.Error(), "主密钥") {
		t.Errorf("test a broken storage: %v", err)
	}
	// Entering the key again mends it.
	if err := lost.Update(ctx, "cloud", Input{Kind: "s3", Settings: Settings{Endpoint: "http://127.0.0.1:1", Bucket: "b", AccessKey: "ak"}, SecretKey: "sk"}); err != nil {
		t.Fatal(err)
	}
	if _, err := fs.Backend("cloud"); err != nil {
		t.Errorf("cloud after mending: %v", err)
	}
}
