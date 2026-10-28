package privacy

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"kept/server/internal/files"
	"kept/server/internal/pinyin"
	"kept/server/internal/sealbox"
	"kept/server/internal/store/sqlc"
)

// Restoring a person's private documents from sealed copies (v1.8.1), with
// their recovery code: after a restore from a remote backup (sealed text
// in the database, sealed files next to their places), or when this
// server lost files that remote storages hold sealed copies of.

// sealedColumns are the text columns a remote backup seals; keep in step
// with backup/private.go.
var sealedColumns = []struct{ table, key, cols string }{
	{"documents", "id", "title, original_filename, content"},
	{"doc_chunks", "id", "text"},
	{"notes", "id", "body"},
	{"highlights", "id", "quote, note"},
	{"custom_field_values", "field_id", "value_text"},
	{"review_cards", "id", "front, back"},
	{"book_summaries", "document_id", "summary, chapters"},
	{"alerts", "id", "note"},
}

// RestoreReport is what RestorePerson did.
type RestoreReport struct {
	Documents int      // private documents of the person
	Values    int      // sealed text values opened
	Files     int      // files opened back into place
	Missing   []string // files neither here nor sealed anywhere reachable
}

// RestorePerson opens a person's sealed text and files with their recovery
// code.
func RestorePerson(ctx context.Context, db *sql.DB, store *files.Store, personID int64, code string) (RestoreReport, error) {
	var rep RestoreReport
	q := sqlc.New(db)
	private, err := OpenWithRecoveryCode(ctx, q, personID, code)
	if err != nil {
		return rep, err
	}
	rows, err := db.QueryContext(ctx, `SELECT id, workspace_id, original_ext, storage FROM documents WHERE person_id = ? AND private = 1 ORDER BY id`, personID)
	if err != nil {
		return rep, err
	}
	type doc struct {
		id, ws       int64
		ext, storage string
	}
	var docs []doc
	for rows.Next() {
		var d doc
		if err := rows.Scan(&d.id, &d.ws, &d.ext, &d.storage); err != nil {
			rows.Close()
			return rep, err
		}
		docs = append(docs, d)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return rep, err
	}
	rep.Documents = len(docs)
	for _, d := range docs {
		n, err := openValues(ctx, db, d.id, private)
		rep.Values += n
		if err != nil {
			return rep, fmt.Errorf("文档 %d：%w", d.id, err)
		}
		got, missing, err := openFiles(ctx, db, store, d.id, d.ws, d.ext, d.storage, private)
		rep.Files += got
		rep.Missing = append(rep.Missing, missing...)
		if err != nil {
			return rep, fmt.Errorf("文档 %d：%w", d.id, err)
		}
	}
	return rep, nil
}

func openValues(ctx context.Context, db *sql.DB, id int64, private []byte) (int, error) {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	n := 0
	for _, t := range sealedColumns {
		where := "document_id = ?"
		if t.table == "documents" {
			where = "id = ?"
		}
		cols := splitColumns(t.cols)
		rows, err := tx.QueryContext(ctx, `SELECT `+t.key+`, `+t.cols+` FROM `+t.table+` WHERE `+where, id)
		if err != nil {
			return n, err
		}
		type row struct {
			key  int64
			vals []sql.NullString
		}
		var all []row
		for rows.Next() {
			r := row{vals: make([]sql.NullString, len(cols))}
			dest := []any{&r.key}
			for i := range r.vals {
				dest = append(dest, &r.vals[i])
			}
			if err := rows.Scan(dest...); err != nil {
				rows.Close()
				return n, err
			}
			all = append(all, r)
		}
		rows.Close()
		for _, r := range all {
			for i, c := range cols {
				v := r.vals[i]
				if !v.Valid || !sealbox.IsSealedString(v.String) {
					continue
				}
				plain, err := sealbox.OpenString(private, v.String)
				if err != nil {
					return n, err
				}
				if _, err := tx.ExecContext(ctx, `UPDATE `+t.table+` SET `+c+` = ? WHERE `+t.key+` = ? AND `+where, plain, r.key, id); err != nil {
					return n, err
				}
				n++
			}
		}
	}
	if n > 0 {
		var title string
		if err := tx.QueryRowContext(ctx, `SELECT title FROM documents WHERE id = ?`, id).Scan(&title); err != nil {
			return n, err
		}
		// the search index and the semantic index read it again
		if _, err := tx.ExecContext(ctx, `UPDATE documents SET title_sort = ?, index_rev = index_rev + 1, embedded_rev = 0 WHERE id = ?`, pinyin.TitleKey(title), id); err != nil {
			return n, err
		}
	}
	return n, tx.Commit()
}

// openFiles puts a document's files back in the data directory: from a
// sealed file next to its place, else from a sealed copy on a storage.
func openFiles(ctx context.Context, db *sql.DB, store *files.Store, id, ws int64, ext, storage string, private []byte) (int, []string, error) {
	refs := []files.Ref{files.Original(ws, id, ext), files.Archive(ws, id), files.Base(ws, id)}
	vrows, err := db.QueryContext(ctx, `SELECT id, original_ext FROM document_versions WHERE document_id = ? AND current = 0`, id)
	if err != nil {
		return 0, nil, err
	}
	for vrows.Next() {
		var v int64
		var e string
		if err := vrows.Scan(&v, &e); err != nil {
			vrows.Close()
			return 0, nil, err
		}
		refs = append(refs, files.Version(ws, id, v, e))
	}
	vrows.Close()
	copies := map[string]string{} // plain key → storage holding a sealed copy
	crows, err := db.QueryContext(ctx, `SELECT file, storage FROM sealed_copies WHERE document_id = ?`, id)
	if err != nil {
		return 0, nil, err
	}
	for crows.Next() {
		var f, s string
		if err := crows.Scan(&f, &s); err != nil {
			crows.Close()
			return 0, nil, err
		}
		copies[f] = s
	}
	crows.Close()
	got := 0
	var missing []string
	for i, r := range refs {
		dst := store.Path(r)
		if _, err := os.Stat(dst); err == nil {
			_ = os.Remove(dst + ".kept")
			continue
		}
		var src io.ReadCloser
		if f, err := os.Open(dst + ".kept"); err == nil {
			src = f
		} else if s, ok := copies[r.Key()]; ok {
			f, err := store.Open(ctx, s, files.Sealed(r))
			if err != nil {
				return got, missing, fmt.Errorf("从存储“%s”读取加密副本：%w", s, err)
			}
			src = f
		} else if storage != files.Local && store.Exists(ctx, storage, r) {
			continue // still where the document says it is
		} else {
			if i == 0 {
				missing = append(missing, r.Key())
			}
			continue
		}
		err := openInto(src, dst, private)
		src.Close()
		if err != nil {
			return got, missing, fmt.Errorf("%s：%w", r.Key(), err)
		}
		_ = os.Remove(dst + ".kept")
		got++
	}
	if got > 0 && storage != files.Local {
		if _, err := db.ExecContext(ctx, `UPDATE documents SET storage = ? WHERE id = ?`, files.Local, id); err != nil {
			return got, missing, err
		}
	}
	return got, missing, nil
}

func openInto(src io.Reader, dst string, private []byte) error {
	r, err := sealbox.Decrypt(src, private)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	tmp := dst + ".part"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	if _, err := io.Copy(f, r); err != nil {
		f.Close()
		os.Remove(tmp)
		if errors.Is(err, sealbox.ErrWrongKey) {
			return errors.New("加密副本打不开：钥匙不对，或文件损坏")
		}
		return err
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, dst)
}

func splitColumns(cols string) []string {
	var out []string
	cur := ""
	for _, c := range cols {
		switch c {
		case ',':
			out = append(out, cur)
			cur = ""
		case ' ':
		default:
			cur += string(c)
		}
	}
	return append(out, cur)
}
