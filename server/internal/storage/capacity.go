package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"kept/server/internal/apperr"
)

// Capacity (v1.7.10): a storage may have a most it holds, so a cloud bucket
// does not grow without bound. Its use is the recorded sizes of the
// documents in it (v1.7.4) and their earlier versions; thumbnails and
// covers stay on this machine and do not count.

// capacityOf is the most storage name may hold; 0 is no limit.
func (s *Service) capacityOf(ctx context.Context, name string) (int64, error) {
	for _, e := range s.opt.Env {
		if e.Name == name {
			return e.Capacity, nil
		}
	}
	var n int64
	err := s.db.QueryRowContext(ctx, `SELECT capacity FROM storages WHERE name = ?`, name).Scan(&n)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	return n, err
}

// usedOf adds up what storage name holds.
func (s *Service) usedOf(ctx context.Context, name string) (int64, error) {
	var docs, versions int64
	if err := s.db.QueryRowContext(ctx, `SELECT CAST(COALESCE(SUM(COALESCE(original_size, 0) + COALESCE(derived_size, 0)), 0) AS BIGINT)
		FROM documents WHERE storage = ?`, name).Scan(&docs); err != nil {
		return 0, err
	}
	if err := s.db.QueryRowContext(ctx, `SELECT CAST(COALESCE(SUM(v.size), 0) AS BIGINT) FROM document_versions v
		JOIN documents d ON d.id = v.document_id WHERE d.storage = ? AND v.current = 0`, name).Scan(&versions); err != nil {
		return 0, err
	}
	return docs + versions, nil
}

func errStorageFull(name string, used, capacity int64) error {
	return apperr.WithCode(apperr.KindConflict, "storage_full",
		fmt.Sprintf("存储“%s”已满（已用 %s / 上限 %s），请实例管理员提高上限，或给空间换一个存储", name, human(used), human(capacity)))
}

func human(n int64) string {
	switch {
	case n >= 1<<30:
		return fmt.Sprintf("%.1f GB", float64(n)/(1<<30))
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	}
	return fmt.Sprintf("%d KB", max(1, n>>10))
}

// Room refuses need more bytes in the storage a workspace writes to when
// it would go over that storage's capacity (uploads, new versions).
func (s *Service) Room(ctx context.Context, ws, need int64) error {
	var name string
	if err := s.db.QueryRowContext(ctx, `SELECT storage FROM workspaces WHERE id = ?`, ws).Scan(&name); err != nil {
		return err
	}
	return s.roomIn(ctx, name, need)
}

func (s *Service) roomIn(ctx context.Context, name string, need int64) error {
	capacity, err := s.capacityOf(ctx, name)
	if err != nil || capacity == 0 {
		return err
	}
	used, err := s.usedOf(ctx, name)
	if err != nil {
		return err
	}
	if used+need > capacity {
		return errStorageFull(name, used, capacity)
	}
	return nil
}

// docSize is what moving a document adds to a storage.
func (s *Service) docSize(ctx context.Context, id int64) (int64, error) {
	var n int64
	err := s.db.QueryRowContext(ctx, `SELECT COALESCE(original_size, 0) + COALESCE(derived_size, 0)
		+ (SELECT CAST(COALESCE(SUM(size), 0) AS BIGINT) FROM document_versions WHERE document_id = ? AND current = 0)
		FROM documents WHERE id = ?`, id, id).Scan(&n)
	return n, err
}
