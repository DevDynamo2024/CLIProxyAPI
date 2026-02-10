package policy

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestSQLiteDailyLimiter_Consume_Persists(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "limits.sqlite")

	limiter, err := NewSQLiteDailyLimiter(dbPath)
	if err != nil {
		t.Fatalf("NewSQLiteDailyLimiter: %v", err)
	}
	defer limiter.Close()

	dayKey := DayKeyChina(time.Date(2026, 2, 8, 0, 0, 0, 0, time.UTC))
	ctx := context.Background()

	if count, allowed, err := limiter.Consume(ctx, "k1", "claude-opus-4-6", dayKey, 2); err != nil || !allowed || count != 1 {
		t.Fatalf("consume #1: count=%d allowed=%v err=%v", count, allowed, err)
	}
	if count, allowed, err := limiter.Consume(ctx, "k1", "claude-opus-4-6", dayKey, 2); err != nil || !allowed || count != 2 {
		t.Fatalf("consume #2: count=%d allowed=%v err=%v", count, allowed, err)
	}
	if _, allowed, err := limiter.Consume(ctx, "k1", "claude-opus-4-6", dayKey, 2); err != nil || allowed {
		t.Fatalf("consume #3: allowed=%v err=%v", allowed, err)
	}

	// Reopen and ensure the counter is persisted.
	if err := limiter.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	limiter, err = NewSQLiteDailyLimiter(dbPath)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer limiter.Close()

	if _, allowed, err := limiter.Consume(ctx, "k1", "claude-opus-4-6", dayKey, 2); err != nil || allowed {
		t.Fatalf("consume after reopen: allowed=%v err=%v", allowed, err)
	}
}
