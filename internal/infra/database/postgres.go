package database

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// NewPool creates a new pgxpool connection pool from the given database URL.
// Always use pgxpool (never raw pgx.Conn) for concurrency-safe connection management.
func NewPool(ctx context.Context, databaseURL string, logger *slog.Logger) (*pgxpool.Pool, error) {
	config, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, fmt.Errorf("parse database config: %w", err)
	}

	// Tune pool settings based on env variables or defaults
	config.MaxConns = getEnvInt("DB_MAX_CONNS", 64)
	config.MinConns = getEnvInt("DB_MIN_CONNS", 16)
	config.MaxConnLifetime = getEnvDuration("DB_MAX_CONN_LIFETIME", time.Hour)
	config.MaxConnIdleTime = getEnvDuration("DB_MAX_CONN_IDLE_TIME", 15*time.Minute)
	config.HealthCheckPeriod = getEnvDuration("DB_HEALTH_CHECK_PERIOD", 30*time.Second)

	if config.ConnConfig.RuntimeParams == nil {
		config.ConnConfig.RuntimeParams = make(map[string]string)
	}
	config.ConnConfig.RuntimeParams["statement_timeout"] = getEnvString("DB_STATEMENT_TIMEOUT", "5000")
	config.ConnConfig.RuntimeParams["lock_timeout"] = getEnvString("DB_LOCK_TIMEOUT", "2000")

	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return nil, fmt.Errorf("create connection pool: %w", err)
	}

	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}

	stat := pool.Stat()
	logger.Info("database connection pool established",
		"total_conns", stat.TotalConns(),
		"max_conns", config.MaxConns,
		"min_conns", config.MinConns,
		"idle_conns", stat.IdleConns(),
	)

	return pool, nil
}

func getEnvInt(key string, fallback int32) int32 {
	if val := os.Getenv(key); val != "" {
		if i, err := strconv.Atoi(val); err == nil {
			return int32(i)
		}
	}
	return fallback
}

func getEnvDuration(key string, fallback time.Duration) time.Duration {
	if val := os.Getenv(key); val != "" {
		if d, err := time.ParseDuration(val); err == nil {
			return d
		}
	}
	return fallback
}

func getEnvString(key string, fallback string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return fallback
}

