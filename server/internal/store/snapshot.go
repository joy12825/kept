package store

import (
	"context"
	"database/sql"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// A Postgres database as a SQLite file (v1.9.10). Backups are SQLite
// snapshots: on Postgres, `kept backup` writes the rows into a fresh SQLite
// database migrated to the same version, so the same backup, sealing of
// private documents for remote storages and restore work on both. A
// restored snapshot goes back to Postgres with `kept copy-to-postgres`.

// SnapshotPostgres copies every row of src (Postgres) into a new SQLite
// database at path, read in one transaction so the copy is of one moment.
func SnapshotPostgres(ctx context.Context, src *sql.DB, path string) error {
	if !IsPostgres(src) {
		return errors.New("snapshot: the source must be Postgres")
	}
	if _, err := os.Stat(path); err == nil {
		return fmt.Errorf("snapshot: %s already exists", path)
	}
	dst, err := sql.Open("sqlite3", "file:"+filepath.ToSlash(path)+"?_pragma=journal_mode(delete)")
	if err != nil {
		return err
	}
	defer dst.Close()
	dst.SetMaxOpenConns(1)
	if _, err := Migrate(ctx, dst); err != nil {
		return err
	}
	// rows go in as they are, in any order
	if _, err := dst.ExecContext(ctx, `PRAGMA foreign_keys=OFF`); err != nil {
		return err
	}
	if err := sameVersion(ctx, src, dst); err != nil {
		return err
	}
	tables, cols, _, err := pgTables(ctx, src)
	if err != nil {
		return err
	}
	stx, err := src.BeginTx(ctx, &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead})
	if err != nil {
		return err
	}
	defer stx.Rollback()
	dtx, err := dst.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer dtx.Rollback()
	for _, t := range tables {
		if err := snapshotTable(ctx, stx, dtx, t, cols[t]); err != nil {
			return fmt.Errorf("复制表 %s：%w", t, err)
		}
	}
	return dtx.Commit()
}

// sameVersion: both databases have applied the same migrations.
func sameVersion(ctx context.Context, pg, lite *sql.DB) error {
	var a, b int64
	q := `SELECT coalesce(max(version), 0) FROM schema_migrations`
	if err := pg.QueryRowContext(ctx, q).Scan(&a); err != nil {
		return err
	}
	if err := lite.QueryRowContext(ctx, q).Scan(&b); err != nil {
		return err
	}
	if a != b {
		return fmt.Errorf("数据库版本不同：Postgres 到 %d，这个程序到 %d", a, b)
	}
	return nil
}

func snapshotTable(ctx context.Context, src, dst *sql.Tx, table string, cols []pgColumn) error {
	// A migration may have put rows in; the copy replaces them.
	if _, err := dst.ExecContext(ctx, `DELETE FROM "`+table+`"`); err != nil {
		return err
	}
	srcExprs := make([]string, len(cols))
	names := make([]string, len(cols))
	for i, c := range cols {
		srcExprs[i] = `"` + c.name + `"`
		names[i] = `"` + c.name + `"`
		if c.typ == "vector" {
			srcExprs[i] += "::text"
		}
		if table == "chunk_vectors" && c.name == "id" {
			names[i] = "rowid" // vec1 keys vectors by rowid
		}
	}
	rows, err := src.QueryContext(ctx, `SELECT `+strings.Join(srcExprs, ", ")+` FROM "`+table+`"`)
	if err != nil {
		return err
	}
	defer rows.Close()
	ins, err := dst.PrepareContext(ctx, `INSERT INTO "`+table+`" (`+strings.Join(names, ", ")+`) VALUES (`+
		strings.TrimSuffix(strings.Repeat("?, ", len(cols)), ", ")+`)`)
	if err != nil {
		return err
	}
	defer ins.Close()
	vals := make([]any, len(cols))
	ptrs := make([]any, len(cols))
	for i := range vals {
		ptrs[i] = &vals[i]
	}
	for rows.Next() {
		if err := rows.Scan(ptrs...); err != nil {
			return err
		}
		for i, c := range cols {
			if c.typ == "vector" && vals[i] != nil {
				if vals[i], err = vectorBlob(vals[i]); err != nil {
					return fmt.Errorf("列 %s：%w", c.name, err)
				}
			}
		}
		if _, err := ins.ExecContext(ctx, vals...); err != nil {
			return err
		}
	}
	return rows.Err()
}

// vectorBlob turns pgvector's text format ([1,2,3]) into vec1's: float32s,
// little-endian.
func vectorBlob(v any) ([]byte, error) {
	var s string
	switch x := v.(type) {
	case string:
		s = x
	case []byte:
		s = string(x)
	default:
		return nil, fmt.Errorf("不能把 %T 转为向量", v)
	}
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(s, "[") || !strings.HasSuffix(s, "]") {
		return nil, fmt.Errorf("向量格式不对：%.20s", s)
	}
	s = s[1 : len(s)-1]
	if s == "" {
		return []byte{}, nil
	}
	parts := strings.Split(s, ",")
	b := make([]byte, 4*len(parts))
	for i, p := range parts {
		f, err := strconv.ParseFloat(strings.TrimSpace(p), 32)
		if err != nil {
			return nil, err
		}
		binary.LittleEndian.PutUint32(b[4*i:], math.Float32bits(float32(f)))
	}
	return b, nil
}
