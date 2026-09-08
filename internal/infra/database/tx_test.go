package database

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"
)

type mockTx struct {
	pgx.Tx
}

func TestInjectAndExtractTx(t *testing.T) {
	ctx := context.Background()

	// Initial context should have no transaction
	if tx := ExtractTx(ctx); tx != nil {
		t.Fatalf("expected nil tx in empty context, got %v", tx)
	}

	// Injected transaction should be retrievable
	expectedTx := &mockTx{}
	txCtx := InjectTx(ctx, expectedTx)

	extracted := ExtractTx(txCtx)
	if extracted != expectedTx {
		t.Fatalf("expected extracted tx to match injected tx")
	}
}

func TestGetExecutor_ReturnsTxWhenPresent(t *testing.T) {
	ctx := context.Background()
	mock := &mockTx{}
	txCtx := InjectTx(ctx, mock)

	executor := GetExecutor(txCtx, nil)
	if executor != mock {
		t.Fatalf("expected GetExecutor to return injected tx, got %v", executor)
	}
}

func TestGetExecutor_ReturnsPoolWhenNoTx(t *testing.T) {
	ctx := context.Background()
	// With no tx injected and pool=nil, GetExecutor should return nil (the pool)
	executor := GetExecutor(ctx, nil)
	if executor != nil {
		t.Fatalf("expected nil executor when pool is nil and no tx present, got %v", executor)
	}
}

func TestWithTx_ReusesExistingTransaction(t *testing.T) {
	ctx := context.Background()
	existingTx := &mockTx{}
	txCtx := InjectTx(ctx, existingTx)

	executed := false
	err := WithTx(txCtx, nil, func(c context.Context) error {
		executed = true
		extracted := ExtractTx(c)
		if extracted != existingTx {
			t.Errorf("expected inner context to retain existing tx")
		}
		return nil
	})

	if err != nil {
		t.Fatalf("WithTx returned unexpected error: %v", err)
	}
	if !executed {
		t.Fatalf("expected fn to be executed in WithTx")
	}
}

func TestWithTx_PropagatesInnerError(t *testing.T) {
	ctx := context.Background()
	existingTx := &mockTx{}
	txCtx := InjectTx(ctx, existingTx)

	expectedErr := errors.New("simulated transaction failure")
	err := WithTx(txCtx, nil, func(c context.Context) error {
		return expectedErr
	})

	if !errors.Is(err, expectedErr) {
		t.Fatalf("expected error %v, got %v", expectedErr, err)
	}
}
