package db

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	mu              sync.Mutex
	pool            *pgxpool.Pool
	lastInitAttempt time.Time
	lastInitErr     error

	tablesEnsured  bool
	lastDDLAttempt time.Time
	lastDDLErr     error
)

func Enabled() bool {
	return metricsDSN() != ""
}

func metricsDSN() string {
	if v := os.Getenv("METRICS_DATABASE_URL"); v != "" {
		return v
	}
	return os.Getenv("DATABASE_URL")
}

func getPool(ctx context.Context) (*pgxpool.Pool, error) {
	mu.Lock()
	defer mu.Unlock()

	if pool != nil {
		return pool, nil
	}

	// dsn := metricsDSN()
	dsn := "postgres://postgres:Vananh12345%40@localhost:5432/cqs_db?sslmode=disable"
	if dsn == "" {
		lastInitErr = errors.New("missing METRICS_DATABASE_URL or DATABASE_URL")
		return nil, lastInitErr
	}
	// dsn := "postgres://postgres:Vananh12345@@localhost:5432/cqs_db?sslmode=disable"
	// Rate-limit reconnect attempts to avoid hammering.
	if !lastInitAttempt.IsZero() && time.Since(lastInitAttempt) < 3*time.Second && lastInitErr != nil {
		return nil, lastInitErr
	}
	lastInitAttempt = time.Now()

	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		lastInitErr = err
		return nil, err
	}
	// Keep this lightweight; we write one row per 2s.
	cfg.MaxConns = 4
	cfg.MinConns = 0
	cfg.MaxConnIdleTime = 30 * time.Second
	cfg.MaxConnLifetime = 5 * time.Minute
	cfg.HealthCheckPeriod = 30 * time.Second

	p, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		lastInitErr = err
		return nil, err
	}

	pool = p
	lastInitErr = nil
	return pool, nil
}

func ensureTables(ctx context.Context) error {
	mu.Lock()
	if tablesEnsured {
		err := lastDDLErr
		mu.Unlock()
		return err
	}
	// Rate-limit DDL attempts too.
	if !lastDDLAttempt.IsZero() && time.Since(lastDDLAttempt) < 5*time.Second && lastDDLErr != nil {
		err := lastDDLErr
		mu.Unlock()
		return err
	}
	lastDDLAttempt = time.Now()
	mu.Unlock()

	p, err := getPool(ctx)
	if err != nil {
		mu.Lock()
		lastDDLErr = err
		mu.Unlock()
		return err
	}

	// Separate tables for CPU and RAM(Memory).
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS cpu_metrics (
			id BIGSERIAL PRIMARY KEY,
			created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			ts_text TEXT,
			time_text TEXT,
			window_sec DOUBLE PRECISION,
			data JSONB NOT NULL
		);`,
		`CREATE INDEX IF NOT EXISTS cpu_metrics_created_at_idx ON cpu_metrics(created_at);`,
		`CREATE TABLE IF NOT EXISTS memory_metrics (
			id BIGSERIAL PRIMARY KEY,
			created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			ts_text TEXT,
			time_text TEXT,
			window_sec DOUBLE PRECISION,
			data JSONB NOT NULL
		);`,
		`CREATE INDEX IF NOT EXISTS memory_metrics_created_at_idx ON memory_metrics(created_at);`,
	}

	for _, q := range stmts {
		_, execErr := p.Exec(ctx, q)
		if execErr != nil {
			mu.Lock()
			lastDDLErr = execErr
			mu.Unlock()
			return execErr
		}
	}

	mu.Lock()
	tablesEnsured = true
	lastDDLErr = nil
	mu.Unlock()
	return nil
}

func InsertCPUMetrics(record map[string]interface{}) error {
	return insert("cpu_metrics", record)
}

func InsertMemoryMetrics(record map[string]interface{}) error {
	return insert("memory_metrics", record)
}

func insert(table string, record map[string]interface{}) error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	p, err := getPool(ctx)
	if err != nil {
		return err
	}
	if err := ensureTables(ctx); err != nil {
		return err
	}

	b, err := json.Marshal(record)
	if err != nil {
		return err
	}

	tsText, _ := record["ts"].(string)
	timeText, _ := record["time"].(string)
	var windowSec *float64
	if v, ok := record["window_sec"].(float64); ok {
		windowSec = &v
	}

	if windowSec == nil {
		_, err = p.Exec(ctx,
			`INSERT INTO `+table+` (ts_text, time_text, data) VALUES ($1, $2, $3::jsonb)`,
			tsText, timeText, string(b),
		)
		return err
	}

	_, err = p.Exec(ctx,
		`INSERT INTO `+table+` (ts_text, time_text, window_sec, data) VALUES ($1, $2, $3, $4::jsonb)`,
		tsText, timeText, *windowSec, string(b),
	)
	return err
}
