//go:build !ee

package main

import (
	"context"
	"database/sql"
	"errors"

	"kept/server/internal/backup"
)

// backupToStorage: the open-source build backs up into a directory only; it
// still restores from a storage (v1.13.2).
func backupToStorage(context.Context, *sql.DB, string, backup.Options) (backup.Result, error) {
	return backup.Result{}, errors.New("这是开源的免费版，不能备份到存储；可以备份到目录（kept backup <目录>），也可以从存储恢复（kept restore --storage）")
}
