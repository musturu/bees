package sqlstore

import (
	"context"
	"database/sql"
)

// DB represents a generic SQL execution interface that can be either a *sql.DB or *sql.Tx.
// This matches standard patterns used by generators like sqlc, allowing abstract swapping.
type DB interface {
	ExecContext(context.Context, string, ...interface{}) (sql.Result, error)
	PrepareContext(context.Context, string) (*sql.Stmt, error)
	QueryContext(context.Context, string, ...interface{}) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...interface{}) *sql.Row
}

// SQLStore is a base implementation for relational databases.
type SQLStore struct {
	DB *sql.DB
}

// Connect initializes the internal database connection given a driver and dsn.
func (s *SQLStore) Connect(driver, dsn string) error {
	db, err := sql.Open(driver, dsn)
	if err != nil {
		return err
	}
	if err := db.Ping(); err != nil {
		return err
	}
	s.DB = db
	return nil
}

// Close terminates the pool connection.
func (s *SQLStore) Close() error {
	if s.DB != nil {
		return s.DB.Close()
	}
	return nil
}

// RunInTx wraps a function inside a standard database transaction.
func (s *SQLStore) RunInTx(ctx context.Context, fn func(DB) error) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}

	if err := fn(tx); err != nil {
		_ = tx.Rollback()
		return err
	}

	return tx.Commit()
}
