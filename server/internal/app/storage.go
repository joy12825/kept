package app

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"kept/server/internal/config"
	"kept/server/internal/files"
	"kept/server/internal/secret"
	"kept/server/internal/storage"
)

// NewFileStore is the data directory's file store with the storages of
// KEPT_STORAGES (v1.7.0). A storage that is not reachable now (a NAS not
// mounted yet, a service down) is still registered; using it fails until
// it is back.
func NewFileStore(cfg config.Config) (*files.Store, error) {
	fs := files.NewStore(cfg.DataDir)
	for _, s := range cfg.Storages {
		b, err := storage.Build(s)
		if err != nil {
			return nil, fmt.Errorf("storage %s: %w", s.Name, err)
		}
		fs.AddStorage(s.Name, b)
	}
	return fs, nil
}

// OpenFileStore is NewFileStore with the storages added on the page too
// (v1.7.2) and the keys of encrypted storages (v1.11.0), for the
// command-line tools.
func OpenFileStore(ctx context.Context, cfg config.Config, db *sql.DB) (*files.Store, error) {
	fs, err := NewFileStore(cfg)
	if err != nil {
		return nil, err
	}
	box, err := secret.Load(cfg.DataDir, cfg.SecretKey)
	if err != nil {
		return nil, fmt.Errorf("the master key: %w", err)
	}
	svc := storage.NewService(db, fs, storage.Options{Env: cfg.Storages, Box: box, DataDir: cfg.DataDir})
	if err := svc.LoadSaved(ctx); err != nil {
		return nil, err
	}
	if err := svc.LoadKeys(ctx); err != nil { // v1.11.0
		return nil, err
	}
	return fs, nil
}

// storageNames lists the configured storages for the system page.
func storageNames(list []config.Storage) string {
	names := make([]string, len(list))
	for i, s := range list {
		names[i] = s.Name
	}
	return strings.Join(names, ", ")
}
