// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

package sqlite

import (
	"context"
	"database/sql/driver"
	"errors"
	"fmt"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"sync"
)

// Sealer seals the values of the sealed column (see OpenSealed).
type Sealer interface {
	Seal(plain []byte) string
	Open(sealed string) ([]byte, error)
	IsSealed(value string) bool
}

// The sealed column: the metadata of bridgev2's logins, which holds each
// account's session (a Telegram authorization key, a Matrix access token).
// bridgev2 writes it as JSON through its own queries, so it is sealed here,
// under database/sql, where every query passes (docs/ADR/0019).
const (
	sealedTable  = "user_login"
	sealedColumn = "metadata"
)

var (
	// writeToSealedTable finds the statements that write to the table.
	writeToSealedTable = regexp.MustCompile(`(?is)\b(?:insert(?:\s+or\s+\w+)?\s+into|replace\s+into|update(?:\s+or\s+\w+)?)\s+"?` + sealedTable + `"?(?:\s|\(|$)`)
	// columnMention finds the column's name, as a whole word.
	columnMention = regexp.MustCompile(`(?i)\b` + sealedColumn + `\b`)
	// fromExcluded is an upsert's copy of the inserted value, already sealed.
	fromExcluded = regexp.MustCompile(`(?i)\b` + sealedColumn + `\s*=\s*excluded\.` + sealedColumn + `\b`)
	// updateAssignment is "metadata=$N" in an UPDATE. dbutil rewrites $N as
	// ?N for SQLite before the query reaches the driver; both are numbered.
	updateAssignment = regexp.MustCompile(`(?i)\b` + sealedColumn + `\s*=\s*[$?](\d+)\b`)
	// insertLists are the column and value lists of an INSERT.
	insertLists = regexp.MustCompile(`(?is)` + sealedTable + `"?\s*\(([^)]*)\)\s*values\s*\(([^)]*)\)`)
	placeholder = regexp.MustCompile(`^[$?](\d+)$`)
)

// sealedArgument returns the ordinal (1-based) of the argument that a query
// writes to the sealed column, or 0 if it writes none. A write to the table
// that it cannot read is an error, so that a change of bridgev2's queries
// fails instead of writing the session in clear.
func sealedArgument(query string) (int, error) {
	writes := writeToSealedTable.FindAllStringIndex(query, -1)
	if len(writes) == 0 {
		return 0, nil
	}
	unsupported := fmt.Errorf("unsupported write to %s.%s: it would not be sealed", sealedTable, sealedColumn)
	trimmed := strings.TrimSpace(query)
	if len(writes) > 1 || strings.TrimSpace(query[:writes[0][0]]) != "" || strings.Contains(strings.TrimSuffix(trimmed, ";"), ";") {
		return 0, unsupported
	}
	mentions := len(columnMention.FindAllStringIndex(fromExcluded.ReplaceAllString(query, ""), -1))
	isUpdate := strings.HasPrefix(strings.ToLower(trimmed), "update")
	if mentions == 0 && isUpdate {
		return 0, nil
	}
	// An INSERT always writes the column (it is NOT NULL), so it must name it.
	if mentions != 1 {
		return 0, unsupported
	}
	if isUpdate {
		m := updateAssignment.FindStringSubmatch(query)
		if m == nil {
			return 0, unsupported
		}
		return strconv.Atoi(m[1])
	}
	m := insertLists.FindStringSubmatch(query)
	if m == nil {
		return 0, unsupported
	}
	columns, values := strings.Split(m[1], ","), strings.Split(m[2], ",")
	if len(columns) != len(values) {
		return 0, unsupported
	}
	for i, column := range columns {
		if strings.EqualFold(strings.Trim(strings.TrimSpace(column), `"`), sealedColumn) {
			p := placeholder.FindStringSubmatch(strings.TrimSpace(values[i]))
			if p == nil {
				return 0, unsupported
			}
			return strconv.Atoi(p[1])
		}
	}
	return 0, unsupported
}

// sealing holds the sealer and the analysis of the queries, cached: bridgev2
// runs a small, fixed set of queries.
type sealing struct {
	sealer Sealer
	cache  sync.Map // query -> sealedQuery
}

type sealedQuery struct {
	ordinal int
	err     error
}

func (s *sealing) analyze(query string) (int, error) {
	if v, ok := s.cache.Load(query); ok {
		q := v.(sealedQuery)
		return q.ordinal, q.err
	}
	ordinal, err := sealedArgument(query)
	s.cache.Store(query, sealedQuery{ordinal, err})
	return ordinal, err
}

// sealArgs returns the arguments with the sealed column's value sealed.
func (s *sealing) sealArgs(ordinal int, args []driver.NamedValue) ([]driver.NamedValue, error) {
	if ordinal == 0 {
		return args, nil
	}
	out := append([]driver.NamedValue(nil), args...)
	for i := range out {
		if out[i].Ordinal != ordinal || out[i].Name != "" {
			continue
		}
		switch v := out[i].Value.(type) {
		case []byte:
			out[i].Value = s.sealer.Seal(v)
		case string:
			out[i].Value = s.sealer.Seal([]byte(v))
		default:
			return nil, fmt.Errorf("%s.%s: cannot seal a %T", sealedTable, sealedColumn, v)
		}
		return out, nil
	}
	return nil, fmt.Errorf("%s.%s: the value to seal is missing", sealedTable, sealedColumn)
}

// openRow opens the sealed values of a row; plain values pass.
func (s *sealing) openRow(columns []int, dest []driver.Value) error {
	for _, i := range columns {
		var value string
		switch v := dest[i].(type) {
		case string:
			value = v
		case []byte:
			value = string(v)
		default:
			continue
		}
		if !s.sealer.IsSealed(value) {
			continue
		}
		plain, err := s.sealer.Open(value)
		if err != nil {
			return fmt.Errorf("%s.%s: %w", sealedTable, sealedColumn, err)
		}
		if _, isString := dest[i].(string); isString {
			dest[i] = string(plain)
		} else {
			dest[i] = plain
		}
	}
	return nil
}

// sealingConnector opens connections that seal the column.
type sealingConnector struct {
	base driver.Connector
	*sealing
}

func (c *sealingConnector) Connect(ctx context.Context) (driver.Conn, error) {
	conn, err := c.base.Connect(ctx)
	if err != nil {
		return nil, err
	}
	return &sealingConn{base: conn, sealing: c.sealing}, nil
}

func (c *sealingConnector) Driver() driver.Driver {
	return &sealingDriver{base: c.base.Driver(), sealing: c.sealing}
}

type sealingDriver struct {
	base driver.Driver
	*sealing
}

func (d *sealingDriver) Open(name string) (driver.Conn, error) {
	conn, err := d.base.Open(name)
	if err != nil {
		return nil, err
	}
	return &sealingConn{base: conn, sealing: d.sealing}, nil
}

// dsnConnector is the connector of a driver that has none of its own.
type dsnConnector struct {
	driver driver.Driver
	dsn    string
}

func (c dsnConnector) Connect(context.Context) (driver.Conn, error) { return c.driver.Open(c.dsn) }
func (c dsnConnector) Driver() driver.Driver                        { return c.driver }

// sealingConn passes everything to the driver's connection, sealing the
// written values and opening the read ones. The optional interfaces of
// database/sql are passed through when the driver has them.
type sealingConn struct {
	base driver.Conn
	*sealing
}

var (
	_ driver.ConnBeginTx            = (*sealingConn)(nil)
	_ driver.ConnPrepareContext     = (*sealingConn)(nil)
	_ driver.ExecerContext          = (*sealingConn)(nil)
	_ driver.QueryerContext         = (*sealingConn)(nil)
	_ driver.Pinger                 = (*sealingConn)(nil)
	_ driver.SessionResetter        = (*sealingConn)(nil)
	_ driver.Validator              = (*sealingConn)(nil)
	_ driver.NamedValueChecker      = (*sealingConn)(nil)
	_ driver.StmtExecContext        = (*sealingStmt)(nil)
	_ driver.StmtQueryContext       = (*sealingStmt)(nil)
	_ driver.NamedValueChecker      = (*sealingStmt)(nil)
	_ driver.RowsNextResultSet      = (*sealingRows)(nil)
	_ driver.RowsColumnTypeScanType = (*sealingRows)(nil)
)

func (c *sealingConn) Prepare(query string) (driver.Stmt, error) {
	return c.PrepareContext(context.Background(), query)
}

func (c *sealingConn) PrepareContext(ctx context.Context, query string) (driver.Stmt, error) {
	ordinal, err := c.analyze(query)
	if err != nil {
		return nil, err
	}
	var stmt driver.Stmt
	if p, ok := c.base.(driver.ConnPrepareContext); ok {
		stmt, err = p.PrepareContext(ctx, query)
	} else {
		stmt, err = c.base.Prepare(query)
	}
	if err != nil {
		return nil, err
	}
	return &sealingStmt{base: stmt, conn: c, ordinal: ordinal}, nil
}

func (c *sealingConn) Close() error { return c.base.Close() }

//nolint:staticcheck // Begin is part of driver.Conn; BeginTx is preferred when the driver has it.
func (c *sealingConn) Begin() (driver.Tx, error) { return c.base.Begin() }

func (c *sealingConn) BeginTx(ctx context.Context, opts driver.TxOptions) (driver.Tx, error) {
	if b, ok := c.base.(driver.ConnBeginTx); ok {
		return b.BeginTx(ctx, opts)
	}
	//nolint:staticcheck // The fallback of drivers without BeginTx.
	return c.base.Begin()
}

func (c *sealingConn) ExecContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	e, ok := c.base.(driver.ExecerContext)
	if !ok {
		return nil, driver.ErrSkip
	}
	ordinal, err := c.analyze(query)
	if err != nil {
		return nil, err
	}
	if args, err = c.sealArgs(ordinal, args); err != nil {
		return nil, err
	}
	return e.ExecContext(ctx, query, args)
}

func (c *sealingConn) QueryContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	q, ok := c.base.(driver.QueryerContext)
	if !ok {
		return nil, driver.ErrSkip
	}
	ordinal, err := c.analyze(query)
	if err != nil {
		return nil, err
	}
	if args, err = c.sealArgs(ordinal, args); err != nil {
		return nil, err
	}
	rows, err := q.QueryContext(ctx, query, args)
	if err != nil {
		return nil, err
	}
	return c.wrapRows(rows), nil
}

func (c *sealingConn) Ping(ctx context.Context) error {
	if p, ok := c.base.(driver.Pinger); ok {
		return p.Ping(ctx)
	}
	return nil
}

func (c *sealingConn) ResetSession(ctx context.Context) error {
	if r, ok := c.base.(driver.SessionResetter); ok {
		return r.ResetSession(ctx)
	}
	return nil
}

func (c *sealingConn) IsValid() bool {
	if v, ok := c.base.(driver.Validator); ok {
		return v.IsValid()
	}
	return true
}

func (c *sealingConn) CheckNamedValue(nv *driver.NamedValue) error {
	if n, ok := c.base.(driver.NamedValueChecker); ok {
		return n.CheckNamedValue(nv)
	}
	return driver.ErrSkip
}

func (c *sealingConn) wrapRows(rows driver.Rows) driver.Rows {
	columns := sealedColumns(rows.Columns())
	if len(columns) == 0 {
		if _, ok := rows.(driver.RowsNextResultSet); !ok {
			return rows
		}
	}
	return &sealingRows{base: rows, sealing: c.sealing, columns: columns}
}

// sealedColumns returns the indexes of the columns that may hold sealed
// values.
func sealedColumns(names []string) []int {
	var columns []int
	for i, name := range names {
		if strings.EqualFold(name, sealedColumn) {
			columns = append(columns, i)
		}
	}
	return columns
}

type sealingStmt struct {
	base    driver.Stmt
	conn    *sealingConn
	ordinal int
}

func (s *sealingStmt) Close() error  { return s.base.Close() }
func (s *sealingStmt) NumInput() int { return s.base.NumInput() }

func (s *sealingStmt) Exec(args []driver.Value) (driver.Result, error) {
	return s.ExecContext(context.Background(), named(args))
}

func (s *sealingStmt) Query(args []driver.Value) (driver.Rows, error) {
	return s.QueryContext(context.Background(), named(args))
}

func (s *sealingStmt) ExecContext(ctx context.Context, args []driver.NamedValue) (driver.Result, error) {
	args, err := s.conn.sealArgs(s.ordinal, args)
	if err != nil {
		return nil, err
	}
	if e, ok := s.base.(driver.StmtExecContext); ok {
		return e.ExecContext(ctx, args)
	}
	values, err := unnamed(args)
	if err != nil {
		return nil, err
	}
	//nolint:staticcheck // The fallback of drivers without ExecContext.
	return s.base.Exec(values)
}

func (s *sealingStmt) QueryContext(ctx context.Context, args []driver.NamedValue) (driver.Rows, error) {
	args, err := s.conn.sealArgs(s.ordinal, args)
	if err != nil {
		return nil, err
	}
	var rows driver.Rows
	if q, ok := s.base.(driver.StmtQueryContext); ok {
		rows, err = q.QueryContext(ctx, args)
	} else {
		var values []driver.Value
		if values, err = unnamed(args); err == nil {
			//nolint:staticcheck // The fallback of drivers without QueryContext.
			rows, err = s.base.Query(values)
		}
	}
	if err != nil {
		return nil, err
	}
	return s.conn.wrapRows(rows), nil
}

func (s *sealingStmt) CheckNamedValue(nv *driver.NamedValue) error {
	if n, ok := s.base.(driver.NamedValueChecker); ok {
		return n.CheckNamedValue(nv)
	}
	return s.conn.CheckNamedValue(nv)
}

func named(args []driver.Value) []driver.NamedValue {
	out := make([]driver.NamedValue, len(args))
	for i, v := range args {
		out[i] = driver.NamedValue{Ordinal: i + 1, Value: v}
	}
	return out
}

func unnamed(args []driver.NamedValue) ([]driver.Value, error) {
	out := make([]driver.Value, len(args))
	for i, a := range args {
		if a.Name != "" {
			return nil, errors.New("named arguments are not supported by the driver")
		}
		out[i] = a.Value
	}
	return out, nil
}

type sealingRows struct {
	base driver.Rows
	*sealing
	columns []int
}

func (r *sealingRows) Columns() []string { return r.base.Columns() }
func (r *sealingRows) Close() error      { return r.base.Close() }

func (r *sealingRows) Next(dest []driver.Value) error {
	if err := r.base.Next(dest); err != nil {
		return err
	}
	return r.openRow(r.columns, dest)
}

func (r *sealingRows) HasNextResultSet() bool {
	n, ok := r.base.(driver.RowsNextResultSet)
	return ok && n.HasNextResultSet()
}

func (r *sealingRows) NextResultSet() error {
	if n, ok := r.base.(driver.RowsNextResultSet); ok {
		if err := n.NextResultSet(); err != nil {
			return err
		}
		r.columns = sealedColumns(r.base.Columns())
		return nil
	}
	return errors.New("no next result set")
}

func (r *sealingRows) ColumnTypeScanType(index int) reflect.Type {
	if t, ok := r.base.(driver.RowsColumnTypeScanType); ok {
		return t.ColumnTypeScanType(index)
	}
	return reflect.TypeFor[any]()
}
