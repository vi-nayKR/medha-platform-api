package database

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// DBTX is an interface that allows querying and executing commands.
// Both *pgxpool.Pool and pgx.Tx implement this interface.
type DBTX interface {
	Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

type txKey struct{}

// InjectTx returns a new context with the given transaction injected.
func InjectTx(ctx context.Context, tx pgx.Tx) context.Context {
	return context.WithValue(ctx, txKey{}, tx)
}

// ExtractTx extracts the transaction from the given context, if any.
func ExtractTx(ctx context.Context) pgx.Tx {
	if tx, ok := ctx.Value(txKey{}).(pgx.Tx); ok {
		return tx
	}
	return nil
}

// GetExecutor returns the transaction if present in the context,
// otherwise returns the provided connection pool.
func GetExecutor(ctx context.Context, pool *pgxpool.Pool) DBTX {
	if tx := ExtractTx(ctx); tx != nil {
		return tx
	}
	return pool
}

// WithTx runs a function within a transaction.
// If the function returns an error, the transaction is rolled back.
// Otherwise, it is committed.
func WithTx(ctx context.Context, pool *pgxpool.Pool, fn func(context.Context) error) error {
	// If already in a transaction, just use it
	if ExtractTx(ctx) != nil {
		return fn(ctx)
	}

	tx, err := pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}

	defer func() {
		_ = tx.Rollback(ctx)
	}()

	txCtx := InjectTx(ctx, tx)
	if err := fn(txCtx); err != nil {
		return err
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}
	return nil
}
