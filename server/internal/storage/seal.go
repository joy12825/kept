package storage

import (
	"context"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"kept/server/internal/files"
	"kept/server/internal/i18n"
	"kept/server/internal/privacy"
	"kept/server/internal/sealbox"
)

// Private documents (v1.8.1): a family member's private document keeps its
// files on this server, and the storage it would be on otherwise — when
// that is not this server — gets a sealed copy of each file, encrypted
// with the person's public key (package sealbox). The sealed copies are
// kept in step by the move job: made when missing or out of date, removed
// when the document is no longer private, deleted, or moved to another
// person whose key is different.

// sealTarget is where a document's sealed copies go: where it would be if
// it were not private, unless that is this server.
func (p plan) sealTarget(date string) string {
	if t := p.want(date, false); t != files.Local {
		return t
	}
	return ""
}

// pendingSealedOf counts the private documents with a key whose sealed
// copy of the original is missing or out of date (the daily check queues
// the move job for them).
func (s *Service) pendingSealedOf(ctx context.Context, q interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, ws int64, p plan) (int, error) {
	var n int
	err := q.QueryRowContext(ctx, `SELECT count(*) FROM documents d JOIN person_keys k ON k.person_id = d.person_id
		WHERE d.workspace_id = ? AND d.private = 1 AND d.deleted_at IS NULL
		  AND (CASE WHEN ? <> '' AND d.created_date < ? THEN ? ELSE ? END) <> 'local'
		  AND NOT EXISTS (SELECT 1 FROM sealed_copies c WHERE c.document_id = d.id AND c.public_key = k.public_key
		      AND c.file LIKE 'originals/%' AND c.checksum = d.checksum
		      AND c.storage = (CASE WHEN ? <> '' AND d.created_date < ? THEN ? ELSE ? END))`,
		ws, p.cutoff, p.cutoff, p.cold, p.hot, p.cutoff, p.cutoff, p.cold, p.hot).Scan(&n)
	if err != nil {
		return 0, err
	}
	// copies to remove: of documents no longer private, or elsewhere
	var stale int
	err = q.QueryRowContext(ctx, `SELECT count(*) FROM sealed_copies c JOIN documents d ON d.id = c.document_id
		WHERE d.workspace_id = ? AND (d.private = 0 OR d.deleted_at IS NOT NULL
		  OR c.public_key NOT IN (SELECT k.public_key FROM person_keys k WHERE k.person_id = d.person_id))`, ws).Scan(&stale)
	return n + stale, err
}

type sealRow struct {
	id, person int64
	ext, date  string
	private    bool
	deleted    bool
}

// sealAll brings the workspace's sealed copies in step; it reports how
// many files it sealed and how many failed.
func (s *Service) sealAll(ctx context.Context, ws int64, p plan, tmp string) (sealed, failed int, lastErr error) {
	rows, err := s.db.QueryContext(ctx, `SELECT d.id, coalesce(d.person_id, 0), d.original_ext, d.created_date, d.private, d.deleted_at IS NOT NULL
		FROM documents d WHERE d.workspace_id = ? AND (d.private = 1 OR d.id IN (SELECT document_id FROM sealed_copies)) ORDER BY d.id`, ws)
	if err != nil {
		return 0, 0, err
	}
	var docs []sealRow
	for rows.Next() {
		var d sealRow
		if err := rows.Scan(&d.id, &d.person, &d.ext, &d.date, &d.private, &d.deleted); err != nil {
			rows.Close()
			return 0, 0, err
		}
		docs = append(docs, d)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, 0, err
	}
	for _, d := range docs {
		if err := ctx.Err(); err != nil {
			return sealed, failed, err
		}
		n, err := s.sealDocument(ctx, ws, d, p, tmp)
		sealed += n
		if err != nil {
			failed++
			lastErr = err
			s.opt.Log.Warn("seal a private document's files", "workspace", ws, "document", d.id, "err", err)
		}
	}
	return sealed, failed, lastErr
}

type sealedCopy struct{ storage, file, checksum, publicKey string }

// sealDocument makes, refreshes or removes one document's sealed copies.
func (s *Service) sealDocument(ctx context.Context, ws int64, d sealRow, p plan, tmp string) (int, error) {
	have, err := s.sealedCopies(ctx, d.id)
	if err != nil {
		return 0, err
	}
	var pub []byte
	target := ""
	if d.private && !d.deleted && d.person != 0 {
		if pub, _, err = privacy.PublicKey(ctx, s.q, d.person); err != nil {
			return 0, err
		}
		if pub != nil {
			target = p.sealTarget(d.date)
		}
	}
	want := map[string]bool{} // plain keys sealed on target
	n := 0
	if target != "" {
		if _, err := s.files.Backend(target); err != nil {
			return 0, fmt.Errorf(i18n.N("存储“%s”没有配置"), target)
		}
		refs := []files.Ref{files.Original(ws, d.id, d.ext), files.Archive(ws, d.id), files.Base(ws, d.id)}
		versions, err := s.files.Versions(ctx, files.Local, ws, d.id)
		if err != nil {
			return 0, err
		}
		key := base64.RawStdEncoding.EncodeToString(pub)
		for i, r := range append(refs, versions...) {
			src := s.files.Path(r)
			if _, err := os.Stat(src); err != nil {
				if i == 0 {
					return n, fmt.Errorf(i18n.N("原件不在本机：%s"), r.Key())
				}
				continue
			}
			want[r.Key()] = true
			sum, err := fileSum(src)
			if err != nil {
				return n, err
			}
			if c, ok := have[target+"\x00"+r.Key()]; ok && c.checksum == sum && c.publicKey == key {
				continue
			}
			if err := s.sealFile(ctx, target, r, src, pub, tmp); err != nil {
				return n, err
			}
			if _, err := s.db.ExecContext(ctx, `INSERT INTO sealed_copies (document_id, storage, file, checksum, public_key, created_at) VALUES (?, ?, ?, ?, ?, ?)
				ON CONFLICT (document_id, storage, file) DO UPDATE SET checksum = excluded.checksum, public_key = excluded.public_key, created_at = excluded.created_at`,
				d.id, target, r.Key(), sum, key, s.opt.Now().UnixMilli()); err != nil {
				return n, err
			}
			n++
		}
	}
	// everything else goes: other storages, files that are gone, documents
	// no longer private
	for k, c := range have {
		if c.storage == target && want[c.file] {
			continue
		}
		plain := refOfKey(ws, d.id, c.file)
		if err := s.files.Remove(context.WithoutCancel(ctx), c.storage, files.Sealed(plain)); err != nil && !errors.Is(err, os.ErrNotExist) {
			s.opt.Log.Warn("remove a sealed copy", "storage", c.storage, "file", c.file, "err", err)
		}
		if _, err := s.db.ExecContext(ctx, `DELETE FROM sealed_copies WHERE document_id = ? AND storage = ? AND file = ?`, d.id, c.storage, c.file); err != nil {
			return n, err
		}
		_ = k
	}
	return n, nil
}

func (s *Service) sealedCopies(ctx context.Context, doc int64) (map[string]sealedCopy, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT storage, file, checksum, public_key FROM sealed_copies WHERE document_id = ?`, doc)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]sealedCopy{}
	for rows.Next() {
		var c sealedCopy
		if err := rows.Scan(&c.storage, &c.file, &c.checksum, &c.publicKey); err != nil {
			return nil, err
		}
		out[c.storage+"\x00"+c.file] = c
	}
	return out, rows.Err()
}

// sealFile encrypts a local file into a temporary file and puts it on the
// storage as the sealed copy of r.
func (s *Service) sealFile(ctx context.Context, storage string, r files.Ref, src string, pub []byte, tmp string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.CreateTemp(tmp, "seal-*")
	if err != nil {
		return err
	}
	defer os.Remove(out.Name())
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
		return fmt.Errorf(i18n.N("加密 %s：%w"), r.Key(), err)
	}
	if err := s.files.Put(ctx, storage, files.Sealed(r), out.Name(), false); err != nil {
		return fmt.Errorf(i18n.N("写入 %s 的加密副本：%w"), r.Key(), err)
	}
	return nil
}

// refOfKey turns a plain key back into its Ref.
func refOfKey(ws, doc int64, key string) files.Ref {
	parts := strings.SplitN(key, "/", 3)
	if len(parts) != 3 {
		return files.Ref{WS: ws, Doc: doc, Kind: "originals", Name: filepath.Base(key)}
	}
	w, _ := strconv.ParseInt(parts[1], 10, 64)
	return files.Ref{WS: w, Doc: doc, Kind: parts[0], Name: parts[2]}
}
