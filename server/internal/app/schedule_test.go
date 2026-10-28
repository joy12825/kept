package app

import (
	"context"
	"path/filepath"
	"testing"

	"kept/server/internal/classify"
	"kept/server/internal/store"
)

// TestOnlyWhere (v1.7.16): mail is fetched only for workspaces with an
// enabled mailbox, scheduled workflows checked only where there are some.
func TestOnlyWhere(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(ctx, filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := st.DB.ExecContext(ctx, q, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec(`INSERT INTO workspaces (id, name, kind, created_at) VALUES (1, 'a', 'family', 1), (2, 'b', 'family', 1), (3, 'c', 'family', 1)`)
	exec(`INSERT INTO mail_accounts (workspace_id, name, host, port, username, password, created_at, updated_at) VALUES (1, 'm', 'imap.example.com', 993, 'u', 'p', 1, 1)`)
	exec(`INSERT INTO mail_accounts (workspace_id, name, host, port, username, password, enabled, created_at, updated_at) VALUES (2, 'm', 'imap.example.com', 993, 'u', 'p', 0, 1, 1)`)
	exec(`INSERT INTO workflows (workspace_id, name, triggers, created_at, updated_at) VALUES (2, 'w', '[{"type":"scheduled","field":"created","offsetDays":30}]', 1, 1)`)
	exec(`INSERT INTO workflows (workspace_id, name, triggers, created_at, updated_at) VALUES (3, 'w', '[{"type":"added"}]', 1, 1)`)
	want := map[string][3]bool{"mail-fetch": {true, false, false}, "workflow-scheduled": {false, true, false}}
	for name, w := range want {
		for i, ws := range []int64{1, 2, 3} {
			var yes bool
			if err := st.DB.QueryRowContext(ctx, onlyWhere[name], ws).Scan(&yes); err != nil {
				t.Fatalf("%s: %v", name, err)
			}
			if yes != w[i] {
				t.Errorf("%s for workspace %d = %v", name, ws, yes)
			}
		}
	}
}

// TestClassifierTrainOnlyWhenChanged (v1.7.19): the hourly training runs
// only without a model of this format, or after something changed.
func TestClassifierTrainOnlyWhenChanged(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(ctx, filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := st.DB.ExecContext(ctx, q, args...); err != nil {
			t.Fatal(err)
		}
	}
	due := sqlYes(st.DB, onlyWhere["classifier-train"])
	exec(`INSERT INTO workspaces (id, name, kind, created_at) VALUES (1, 'a', 'family', 1)`)
	if !due(ctx, 1) {
		t.Fatal("no model yet: should train")
	}
	exec(`INSERT INTO classifier_models (workspace_id, format_version, trained_at, data_hash, doc_count) VALUES (1, ?, 1000, 'h', 0)`, classify.FormatVersion)
	if due(ctx, 1) {
		t.Fatal("nothing changed: should not train")
	}
	exec(`INSERT INTO tags (workspace_id, name, sort_key, color, created_at, updated_at) VALUES (1, '发票', 'fapiao', 'blue', 2000, 2000)`)
	if !due(ctx, 1) {
		t.Fatal("a tag changed: should train")
	}
	exec(`UPDATE classifier_models SET trained_at = 3000`)
	if due(ctx, 1) {
		t.Fatal("trained since: should not train")
	}
	exec(`UPDATE classifier_models SET format_version = ?`, classify.FormatVersion-1)
	if !due(ctx, 1) {
		t.Fatal("old format: should train")
	}
}
