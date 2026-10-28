package store

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
)

// A database/sql driver for Postgres (pgx) that rewrites each statement
// from the SQLite form the code is written in (RewriteForPostgres).

type pgDriver struct{ base driver.Driver }

func (d *pgDriver) Open(name string) (driver.Conn, error) {
	c, err := d.base.Open(name)
	if err != nil {
		return nil, err
	}
	return &pgConn{c}, nil
}

type pgConnector struct {
	base   driver.Connector
	driver *pgDriver
}

func (c *pgConnector) Connect(ctx context.Context) (driver.Conn, error) {
	conn, err := c.base.Connect(ctx)
	if err != nil {
		return nil, err
	}
	return &pgConn{conn}, nil
}

func (c *pgConnector) Driver() driver.Driver { return c.driver }

// openPostgres opens a Postgres database through the rewriting driver.
func openPostgres(dsn string) (*sql.DB, error) {
	cfg, err := pgx.ParseConfig(dsn)
	if err != nil {
		return nil, err
	}
	base := stdlib.GetConnector(*cfg)
	return sql.OpenDB(&pgConnector{base: base, driver: &pgDriver{base: stdlib.GetDefaultDriver()}}), nil
}

// IsPostgres reports whether db is a Postgres database opened by this package.
func IsPostgres(db *sql.DB) bool {
	_, ok := db.Driver().(*pgDriver)
	return ok
}

type pgConn struct{ c driver.Conn }

var errNotSupported = errors.New("store: not supported by the Postgres driver")

func (p *pgConn) Prepare(query string) (driver.Stmt, error) {
	return p.c.Prepare(RewriteForPostgres(query))
}

func (p *pgConn) Close() error { return p.c.Close() }

func (p *pgConn) Begin() (driver.Tx, error) { return p.c.Begin() } //nolint:staticcheck // required by driver.Conn

func (p *pgConn) PrepareContext(ctx context.Context, query string) (driver.Stmt, error) {
	if pc, ok := p.c.(driver.ConnPrepareContext); ok {
		return pc.PrepareContext(ctx, RewriteForPostgres(query))
	}
	return p.Prepare(query)
}

// writeLock is the advisory lock every read-write transaction takes first:
// write transactions run one at a time, as on SQLite (_txlock=immediate),
// so code that reads and then writes in a transaction stays correct.
const writeLock = 7320

func (p *pgConn) BeginTx(ctx context.Context, opts driver.TxOptions) (driver.Tx, error) {
	bc, ok := p.c.(driver.ConnBeginTx)
	if !ok {
		return nil, errNotSupported
	}
	tx, err := bc.BeginTx(ctx, opts)
	if err != nil || opts.ReadOnly {
		return tx, err
	}
	ec, ok := p.c.(driver.ExecerContext)
	if !ok {
		tx.Rollback()
		return nil, errNotSupported
	}
	if _, err := ec.ExecContext(ctx, `SELECT pg_advisory_xact_lock($1)`, []driver.NamedValue{{Ordinal: 1, Value: int64(writeLock)}}); err != nil {
		tx.Rollback()
		return nil, err
	}
	return tx, nil
}

func (p *pgConn) ExecContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	if ec, ok := p.c.(driver.ExecerContext); ok {
		return ec.ExecContext(ctx, RewriteForPostgres(query), args)
	}
	return nil, driver.ErrSkip
}

func (p *pgConn) QueryContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	if qc, ok := p.c.(driver.QueryerContext); ok {
		return qc.QueryContext(ctx, RewriteForPostgres(query), args)
	}
	return nil, driver.ErrSkip
}

func (p *pgConn) Ping(ctx context.Context) error {
	if pc, ok := p.c.(driver.Pinger); ok {
		return pc.Ping(ctx)
	}
	return nil
}

func (p *pgConn) ResetSession(ctx context.Context) error {
	if r, ok := p.c.(driver.SessionResetter); ok {
		return r.ResetSession(ctx)
	}
	return nil
}

func (p *pgConn) IsValid() bool {
	if v, ok := p.c.(driver.Validator); ok {
		return v.IsValid()
	}
	return true
}

func (p *pgConn) CheckNamedValue(nv *driver.NamedValue) error {
	if c, ok := p.c.(driver.NamedValueChecker); ok {
		return c.CheckNamedValue(nv)
	}
	return driver.ErrSkip
}
