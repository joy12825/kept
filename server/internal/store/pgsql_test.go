package store

import "testing"

func TestRewriteForPostgres(t *testing.T) {
	for in, want := range map[string]string{
		"SELECT * FROM t WHERE a = ? AND b IN (?, ?)":                      "SELECT * FROM t WHERE a = $1 AND b IN ($2, $3)",
		"SELECT '?' , \"what?\", ? -- why?\n, ?":                           "SELECT '?' , \"what?\", $1 -- why?\n, $2",
		"SELECT 'it''s ?', ? /* ? */":                                      "SELECT 'it''s ?', $1 /* ? */",
		"INSERT OR IGNORE INTO document_tags (a, b) VALUES (?, ?);":        "INSERT INTO document_tags (a, b) VALUES ($1, $2) ON CONFLICT DO NOTHING;",
		"insert or ignore into t (a) values (?) returning id":              "insert into t (a) values ($1) ON CONFLICT DO NOTHING returning id",
		"INSERT INTO t (a) SELECT x FROM u RETURNING id":                   "INSERT INTO t (a) SELECT x FROM u RETURNING id",
		"SELECT value FROM json_each(?)":                                   "SELECT value FROM kept_json_each($1)",
		"SELECT my_json_each(x), char(10), varchar(3), group_concat(a)":    "SELECT my_json_each(x), chr(10), varchar(3), group_concat(a)",
		"WHERE title LIKE ? AND name NOT LIKE 'LIKE' AND liked = 1":        "WHERE title ILIKE $1 AND name NOT ILIKE 'LIKE' AND liked = 1",
		"SELECT CAST(x AS INTEGER) WHERE workspace_id IS ?1 AND y IS NULL": "SELECT CAST(x AS BIGINT) WHERE workspace_id IS NOT DISTINCT FROM $1 AND y IS NULL",
		"SELECT ?": "SELECT $1",
	} {
		if got := RewriteForPostgres(in); got != want {
			t.Errorf("rewrite(%q)\n got %q\nwant %q", in, got, want)
		}
	}
}
