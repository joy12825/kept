package backup

import (
	"context"
	"database/sql"
	"encoding/base64"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"

	"kept/server/internal/sealbox"
)

// Private documents in remote backups (v1.8.1). A backup in a storage of
// another machine must not carry family members' private documents in the
// clear: in the database snapshot uploaded there, their titles, file
// names, recognised text, text fields, notes, highlights and semantic text
// are sealed for the person's public key (package sealbox); what an agent
// sent a model about them and the questions answered from them are left
// out, and the activity log says only “私密文档”. Their files go up sealed
// (<key>.kept); thumbnails and covers do not go up at all.
//
// A person without a key yet has no sealed copies: their private documents'
// text is left out of the remote snapshot and their files are not backed
// up there, only locally. `kept private restore` opens the sealed parts
// with the recovery code after a restore.

const privatePlaceholder = "（私密文档）"

// SealedSuffix ends the key of a sealed file in a remote backup.
const SealedSuffix = ".kept"

// sealSnapshot seals the private documents in a snapshot made for a remote
// backup.
func sealSnapshot(ctx context.Context, snap string) error {
	db, err := sql.Open("sqlite3", "file:"+filepath.ToSlash(snap))
	if err != nil {
		return err
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	var has int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM sqlite_master WHERE type = 'table' AND name = 'person_keys'`).Scan(&has); err != nil || has == 0 {
		return err // a database from before private documents
	}
	rows, err := db.QueryContext(ctx, `SELECT d.id, coalesce(k.public_key, '') FROM documents d LEFT JOIN person_keys k ON k.person_id = d.person_id WHERE d.private = 1`)
	if err != nil {
		return err
	}
	keys := map[int64][]byte{}
	for rows.Next() {
		var id int64
		var pub string
		if err := rows.Scan(&id, &pub); err != nil {
			rows.Close()
			return err
		}
		if pub == "" {
			keys[id] = nil
			continue
		}
		b, err := base64.RawStdEncoding.DecodeString(pub)
		if err != nil {
			rows.Close()
			return err
		}
		keys[id] = b
	}
	rows.Close()
	if len(keys) == 0 {
		return nil
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for id, pub := range keys {
		if err := sealDocumentRows(ctx, tx, id, pub); err != nil {
			return fmt.Errorf("加密私密文档 %d：%w", id, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	_, err = db.ExecContext(ctx, `VACUUM`) // no sealed-over text left in free pages
	return err
}

// sealDocumentRows seals one private document's text in the snapshot; a
// nil key leaves it out instead.
func sealDocumentRows(ctx context.Context, tx *sql.Tx, id int64, pub []byte) error {
	seal := func(s string) (string, error) {
		if pub == nil {
			return "", nil
		}
		if s == "" || sealbox.IsSealedString(s) {
			return s, nil
		}
		return sealbox.SealString(pub, s)
	}
	// each table's text columns, sealed row by row
	type target struct{ table, key, cols string }
	for _, t := range []target{
		{"documents", "id", "title, original_filename, content"},
		{"doc_chunks", "id", "text"},
		{"notes", "id", "body"},
		{"highlights", "id", "quote, note"},
		{"custom_field_values", "field_id", "value_text"},
		{"review_cards", "id", "front, back"},
		{"book_summaries", "document_id", "summary, chapters"},
		{"alerts", "id", "note"},
	} {
		where := "document_id = ?"
		if t.table == "documents" {
			where = "id = ?"
		}
		if err := sealColumns(ctx, tx, t.table, t.key, t.cols, where, id, seal); err != nil {
			return err
		}
	}
	if pub == nil {
		if _, err := tx.ExecContext(ctx, `UPDATE documents SET title = ?, title_sort = '' WHERE id = ?`, privatePlaceholder, id); err != nil {
			return err
		}
	} else if _, err := tx.ExecContext(ctx, `UPDATE documents SET title_sort = '' WHERE id = ?`, id); err != nil {
		return err
	}
	ids := strconv.FormatInt(id, 10)
	for _, q := range []string{
		// the vectors go (v1.9.10): they are not the text, but they are about
		// it; the restored instance embeds the document again
		`DELETE FROM chunk_vectors WHERE rowid IN (SELECT id FROM doc_chunks WHERE document_id = ` + ids + `)`,
		`UPDATE documents SET embedded_rev = 0 WHERE id = ` + ids,
		`DELETE FROM agent_steps WHERE run_id IN (SELECT id FROM agent_runs WHERE document_id = ` + ids + `)`,
		`UPDATE agent_runs SET summary = '' WHERE document_id = ` + ids,
		`UPDATE qa_messages SET content = '` + privatePlaceholder + `', sources = '[]' WHERE sources LIKE '%"documentId":` + ids + `,%'`,
		`UPDATE audit_events SET summary = '` + privatePlaceholder + `', changes = NULL WHERE target_type = 'document' AND target_id = '` + ids + `'`,
		`UPDATE jobs SET payload = '{}', result = NULL WHERE document_id = ` + ids,
	} {
		if _, err := tx.ExecContext(ctx, q); err != nil {
			return fmt.Errorf("%s：%w", q, err)
		}
	}
	return nil
}

func sealColumns(ctx context.Context, tx *sql.Tx, table, key, cols, where string, id int64, seal func(string) (string, error)) error {
	rows, err := tx.QueryContext(ctx, `SELECT `+key+`, `+cols+` FROM `+table+` WHERE `+where, id)
	if err != nil {
		return err
	}
	type row struct {
		key  int64
		vals []sql.NullString
	}
	var all []row
	names := splitCols(cols)
	for rows.Next() {
		r := row{vals: make([]sql.NullString, len(names))}
		dest := []any{&r.key}
		for i := range r.vals {
			dest = append(dest, &r.vals[i])
		}
		if err := rows.Scan(dest...); err != nil {
			rows.Close()
			return err
		}
		all = append(all, r)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	for _, r := range all {
		set, args := "", []any{}
		for i, c := range names {
			if !r.vals[i].Valid {
				continue
			}
			v, err := seal(r.vals[i].String)
			if err != nil {
				return err
			}
			if set != "" {
				set += ", "
			}
			set += c + " = ?"
			args = append(args, v)
		}
		if set == "" {
			continue
		}
		if _, err := tx.ExecContext(ctx, `UPDATE `+table+` SET `+set+` WHERE `+key+` = ? AND `+where, append(args, r.key, id)...); err != nil {
			return err
		}
	}
	return nil
}

func splitCols(cols string) []string {
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

// sealTo encrypts a local file for pub into a temporary file of tmp.
func sealTo(src string, pub []byte, tmp string) (string, error) {
	in, err := os.Open(src)
	if err != nil {
		return "", err
	}
	defer in.Close()
	out, err := os.CreateTemp(tmp, "sealed-*")
	if err != nil {
		return "", err
	}
	w, err := sealbox.Encrypt(out, pub)
	if err == nil {
		_, err = io.Copy(w, in)
	}
	if err == nil {
		err = w.Close()
	}
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		os.Remove(out.Name())
		return "", err
	}
	return out.Name(), nil
}

// sealPrivateFiles turns a snapshot's file list into what goes into a
// remote backup: a private document's stored files are sealed for its
// person's key, its thumbnail and cover stay out, and a private document
// whose person has no key yet has no files there at all.
func sealPrivateFiles(ctx context.Context, snap string, list []docFile) ([]docFile, int, error) {
	db, err := sql.Open("sqlite3", "file:"+filepath.ToSlash(snap)+"?mode=ro")
	if err != nil {
		return nil, 0, err
	}
	defer db.Close()
	var has int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM sqlite_master WHERE type = 'table' AND name = 'person_keys'`).Scan(&has); err != nil || has == 0 {
		return list, 0, err
	}
	rows, err := db.QueryContext(ctx, `SELECT d.id, coalesce(k.public_key, '') FROM documents d LEFT JOIN person_keys k ON k.person_id = d.person_id WHERE d.private = 1`)
	if err != nil {
		return nil, 0, err
	}
	keys := map[int64][]byte{}
	for rows.Next() {
		var id int64
		var pub string
		if err := rows.Scan(&id, &pub); err != nil {
			rows.Close()
			return nil, 0, err
		}
		b, _ := base64.RawStdEncoding.DecodeString(pub)
		keys[id] = b
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	out := list[:0:0]
	left := 0
	for _, f := range list {
		doc := f.doc
		if f.ref.Kind != "" {
			doc = f.ref.Doc
		}
		pub, private := keys[doc]
		switch {
		case !private:
			out = append(out, f)
		case len(pub) == 0 || f.ref.Kind == "": // no key yet; thumbnails, covers
			left++
		default:
			f.rel += SealedSuffix
			f.checksum = "" // of the plaintext: checked when opened
			f.sealed = pub
			out = append(out, f)
		}
	}
	return out, left, nil
}
