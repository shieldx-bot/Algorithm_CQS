//go:build linux

package memory

import (
	"log"
	"os"
	"path/filepath"
	"runtime"
)

func newMemoryMetricsFileLogger() (*log.Logger, func() error, error) {
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		return nil, nil, os.ErrInvalid
	}
	codeDir := filepath.Dir(thisFile)
	logDir := filepath.Join(codeDir, "memory_logs")
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		return nil, nil, err
	}
	logPath := filepath.Join(logDir, "memory_metrics.jsonl")

	f, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, nil, err
	}
	cleanup := func() error { return f.Close() }

	// JSON-lines sink: one JSON object per line, no prefixes/timestamps.
	logger := log.New(f, "", 0)
	return logger, cleanup, nil
}
