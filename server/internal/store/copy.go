package store

import (
	"context"
	"database/sql"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"
)

// Copying a SQLite database into an empty Postgres one (v1.0 stage 39,
// `kept copy-to-postgres`). The tables and columns come from the Postgres
// schema; both databases are migrated to the same version first.

// ErrNotEmpty means the Postgres database already has data.
var ErrNotEmpty = errors.New("目标数据库里已经有数据，请换一个空的数据库")

// CopiedTable is one table's row count, the same on both sides.
type CopiedTable struct {
	Name string
	Rows int
}

type pgColumn struct{ name, typ string } // typ: udt_name (int8, text, citext, float8, bytea, vector)

// CopyToPostgres copies every row of src (SQLite) into dst (Postgres) in
// one transaction, keeping the ids, and checks the row counts after.
func CopyToPostgres(ctx context.Context, src, dst *sql.DB, progress func(table string, rows int)) ([]CopiedTable, error) {
	if IsPostgres(src) || !IsPostgres(dst) {
		return nil, errors.New("copy: the source must be SQLite and the target Postgres")
	}
	var n int
	if err := dst.QueryRowContext(ctx, `SELECT (SELECT count(*) FROM users) + (SELECT count(*) FROM workspaces)`).Scan(&n); err != nil {
		return nil, err
	}
	if n > 0 {
		return nil, ErrNotEmpty
	}
	tables, cols, selfRefs, err := pgTables(ctx, dst)
	if err != nil {
		return nil, err
	}
	// One read transaction: the rows are a snapshot of a single moment.
	stx, err := src.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer stx.Rollback()
	dtx, err := dst.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer dtx.Rollback()
	var out []CopiedTable
	for _, t := range tables {
		rows, err := copyTable(ctx, stx, dtx, t, cols[t], selfRefs[t])
		if err != nil {
			return nil, fmt.Errorf("复制表 %s：%w", t, err)
		}
		out = append(out, CopiedTable{Name: t, Rows: rows})
		if progress != nil {
			progress(t, rows)
		}
	}
	for _, c := range out {
		var got int
		if err := dtx.QueryRowContext(ctx, `SELECT count(*) FROM "`+c.Name+`"`).Scan(&got); err != nil {
			return nil, err
		}
		if got != c.Rows {
			return nil, fmt.Errorf("表 %s：复制了 %d 行，目标里有 %d 行", c.Name, c.Rows, got)
		}
	}
	if err := dtx.Commit(); err != nil {
		return nil, err
	}
	_, _ = dst.ExecContext(ctx, `ANALYZE`)
	return out, nil
}

// pgTables lists the tables in foreign-key order (referenced first), their
// columns, and each table's columns that refer to the table itself.
func pgTables(ctx context.Context, db *sql.DB) ([]string, map[string][]pgColumn, map[string][]string, error) {
	cols := map[string][]pgColumn{}
	rows, err := db.QueryContext(ctx, `SELECT c.table_name, c.column_name, c.udt_name FROM information_schema.columns c
		JOIN information_schema.tables t ON t.table_schema = c.table_schema AND t.table_name = c.table_name
		WHERE c.table_schema = current_schema() AND t.table_type = 'BASE TABLE' AND c.table_name <> 'schema_migrations'
		ORDER BY c.table_name, c.ordinal_position`)
	if err != nil {
		return nil, nil, nil, err
	}
	var names []string
	for rows.Next() {
		var t string
		var c pgColumn
		if err := rows.Scan(&t, &c.name, &c.typ); err != nil {
			rows.Close()
			return nil, nil, nil, err
		}
		if _, ok := cols[t]; !ok {
			names = append(names, t)
		}
		cols[t] = append(cols[t], c)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, nil, nil, err
	}
	deps := map[string][]string{}
	self := map[string][]string{}
	rows, err = db.QueryContext(ctx, `SELECT c.conrelid::regclass::text, c.confrelid::regclass::text, a.attname
		FROM pg_constraint c JOIN pg_attribute a ON a.attrelid = c.conrelid AND a.attnum = c.conkey[1]
		WHERE c.contype = 'f' AND c.connamespace = current_schema()::regnamespace`)
	if err != nil {
		return nil, nil, nil, err
	}
	for rows.Next() {
		var from, to, col string
		if err := rows.Scan(&from, &to, &col); err != nil {
			rows.Close()
			return nil, nil, nil, err
		}
		from, to = strings.Trim(from, `"`), strings.Trim(to, `"`)
		if from == to {
			self[from] = append(self[from], col)
		} else {
			deps[from] = append(deps[from], to)
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, nil, nil, err
	}
	var order []string
	state := map[string]int{} // 1 visiting, 2 done
	var visit func(string) error
	visit = func(t string) error {
		switch state[t] {
		case 1:
			return fmt.Errorf("copy: foreign keys form a cycle at %s", t)
		case 2:
			return nil
		}
		state[t] = 1
		for _, d := range deps[t] {
			if err := visit(d); err != nil {
				return err
			}
		}
		state[t] = 2
		order = append(order, t)
		return nil
	}
	for _, t := range names {
		if err := visit(t); err != nil {
			return nil, nil, nil, err
		}
	}
	return order, cols, self, nil
}

const copyBatch = 500

func copyTable(ctx context.Context, src, dst *sql.Tx, table string, cols []pgColumn, selfRefs []string) (int, error) {
	names := make([]string, len(cols))
	srcExprs := make([]string, len(cols))
	for i, c := range cols {
		names[i] = `"` + c.name + `"`
		srcExprs[i] = `"` + c.name + `"`
		if table == "chunk_vectors" && c.name == "id" {
			srcExprs[i] = "rowid" // vec1 keys vectors by rowid
		}
	}
	rows, err := src.QueryContext(ctx, `SELECT `+strings.Join(srcExprs, ", ")+` FROM "`+table+`"`)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	// Postgres takes at most 65535 parameters a statement.
	batch := min(copyBatch, 65535/len(cols))
	insert := func(vals [][]any) error {
		var b strings.Builder
		b.WriteString(`INSERT INTO "` + table + `" (` + strings.Join(names, ", ") + `) VALUES `)
		args := make([]any, 0, len(vals)*len(cols))
		for r, row := range vals {
			if r > 0 {
				b.WriteString(", ")
			}
			b.WriteByte('(')
			for i, c := range cols {
				if i > 0 {
					b.WriteString(", ")
				}
				if c.typ == "vector" {
					b.WriteString("CAST(? AS text)::vector")
				} else {
					b.WriteByte('?')
				}
			}
			b.WriteByte(')')
			args = append(args, row...)
		}
		_, err := dst.ExecContext(ctx, b.String(), args...)
		return err
	}
	// Rows are inserted in batches as they are read, except in a table that
	// refers to itself: those are all read first and put in order.
	var pending [][]any
	total := 0
	for rows.Next() {
		vals := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return 0, err
		}
		for i, c := range cols {
			if vals[i], err = convert(vals[i], c.typ); err != nil {
				return 0, fmt.Errorf("列 %s：%w", c.name, err)
			}
		}
		pending = append(pending, vals)
		total++
		if len(selfRefs) == 0 && len(pending) == batch {
			if err := insert(pending); err != nil {
				return 0, err
			}
			pending = pending[:0]
		}
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}
	if len(selfRefs) > 0 {
		pending = parentsFirst(pending, cols, selfRefs)
	}
	for start := 0; start < len(pending); start += batch {
		if err := insert(pending[start:min(start+batch, len(pending))]); err != nil {
			return 0, err
		}
	}
	return total, nil
}

// parentsFirst orders rows so that a row comes after the rows of the same
// table it refers to (tags under their parent).
func parentsFirst(rows [][]any, cols []pgColumn, refs []string) [][]any {
	idCol := slices.IndexFunc(cols, func(c pgColumn) bool { return c.name == "id" })
	var refCols []int
	for _, r := range refs {
		refCols = append(refCols, slices.IndexFunc(cols, func(c pgColumn) bool { return c.name == r }))
	}
	if idCol < 0 || slices.Contains(refCols, -1) {
		return rows
	}
	done := map[any]bool{}
	out := make([][]any, 0, len(rows))
	for len(rows) > 0 {
		var rest [][]any
		for _, r := range rows {
			ready := true
			for _, c := range refCols {
				if r[c] != nil && !done[r[c]] {
					ready = false
				}
			}
			if ready {
				out = append(out, r)
				done[r[idCol]] = true
			} else {
				rest = append(rest, r)
			}
		}
		if len(rest) == len(rows) { // refers to a missing row: let the insert report it
			return append(out, rest...)
		}
		rows = rest
	}
	return out
}

// convert turns a SQLite value into one for a Postgres column of type typ.
// SQLite does not enforce column types, so a text column can hold a number.
func convert(v any, typ string) (any, error) {
	if v == nil {
		return nil, nil
	}
	switch typ {
	case "int8":
		switch x := v.(type) {
		case int64:
			return x, nil
		case float64:
			return int64(x), nil
		case string:
			return strconv.ParseInt(strings.TrimSpace(x), 10, 64)
		case []byte:
			return strconv.ParseInt(strings.TrimSpace(string(x)), 10, 64)
		}
	case "float8":
		switch x := v.(type) {
		case int64:
			return float64(x), nil
		case float64:
			return x, nil
		case string:
			return strconv.ParseFloat(strings.TrimSpace(x), 64)
		}
	case "text", "citext":
		switch x := v.(type) {
		case string:
			return x, nil
		case []byte:
			return string(x), nil
		case int64:
			return strconv.FormatInt(x, 10), nil
		case float64:
			return strconv.FormatFloat(x, 'g', -1, 64), nil
		}
	case "bytea":
		switch x := v.(type) {
		case []byte:
			return x, nil
		case string:
			return []byte(x), nil
		}
	case "vector":
		if b, ok := v.([]byte); ok && len(b)%4 == 0 {
			return vectorText(b), nil
		}
	default:
		return v, nil
	}
	return nil, fmt.Errorf("不能把 %T 转为 %s", v, typ)
}

// vectorText turns a vec1 vector (float32s, little-endian) into pgvector's
// text format.
func vectorText(b []byte) string {
	out := make([]byte, 0, len(b)*3+2)
	out = append(out, '[')
	for i := 0; i < len(b); i += 4 {
		if i > 0 {
			out = append(out, ',')
		}
		f := math.Float32frombits(binary.LittleEndian.Uint32(b[i:]))
		out = strconv.AppendFloat(out, float64(f), 'g', -1, 32)
	}
	return string(append(out, ']'))
}
