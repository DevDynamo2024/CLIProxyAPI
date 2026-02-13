package billing

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v6/internal/policy"
	_ "modernc.org/sqlite"
)

// DailyCostReader is the minimal interface needed by request-time middleware.
type DailyCostReader interface {
	GetDailyCostMicroUSD(ctx context.Context, apiKey, dayKey string) (int64, error)
}

type SQLiteStore struct {
	db   *sql.DB
	path string
}

func NewSQLiteStore(path string) (*SQLiteStore, error) {
	trimmed := strings.TrimSpace(path)
	if trimmed == "" {
		return nil, fmt.Errorf("billing sqlite: path is required")
	}
	abs, err := filepath.Abs(trimmed)
	if err != nil {
		return nil, fmt.Errorf("billing sqlite: resolve path: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(abs), 0o700); err != nil {
		return nil, fmt.Errorf("billing sqlite: create directory: %w", err)
	}

	dsn := fmt.Sprintf("file:%s?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(ON)", abs)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("billing sqlite: open database: %w", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("billing sqlite: ping database: %w", err)
	}

	store := &SQLiteStore{db: db, path: abs}
	if err := store.ensureSchema(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	return store, nil
}

func (s *SQLiteStore) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

func (s *SQLiteStore) ensureSchema(ctx context.Context) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("billing sqlite: not initialized")
	}

	stmts := []string{
		`
		CREATE TABLE IF NOT EXISTS model_prices (
			model TEXT NOT NULL PRIMARY KEY,
			prompt_micro_usd_per_1m INTEGER NOT NULL,
			completion_micro_usd_per_1m INTEGER NOT NULL,
			cached_micro_usd_per_1m INTEGER NOT NULL,
			updated_at INTEGER NOT NULL
		)
		`,
		`
		CREATE TABLE IF NOT EXISTS api_key_model_daily_usage (
			api_key TEXT NOT NULL,
			model TEXT NOT NULL,
			day TEXT NOT NULL,
			requests INTEGER NOT NULL,
			failed_requests INTEGER NOT NULL,
			input_tokens INTEGER NOT NULL,
			output_tokens INTEGER NOT NULL,
			reasoning_tokens INTEGER NOT NULL,
			cached_tokens INTEGER NOT NULL,
			total_tokens INTEGER NOT NULL,
			cost_micro_usd INTEGER NOT NULL,
			updated_at INTEGER NOT NULL,
			PRIMARY KEY (api_key, model, day)
		)
		`,
		`CREATE INDEX IF NOT EXISTS idx_api_key_model_daily_usage_api_day ON api_key_model_daily_usage (api_key, day)`,
	}

	for _, stmt := range stmts {
		if _, err := s.db.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("billing sqlite: ensure schema: %w", err)
		}
	}
	return nil
}

func (s *SQLiteStore) UpsertModelPrice(ctx context.Context, model string, price PriceMicroUSDPer1M) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("billing sqlite: not initialized")
	}
	key := policy.NormaliseModelKey(model)
	if key == "" {
		return fmt.Errorf("billing sqlite: model is required")
	}
	if price.Prompt < 0 || price.Completion < 0 || price.Cached < 0 {
		return fmt.Errorf("billing sqlite: invalid price")
	}
	now := nowUnixUTC()
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO model_prices (model, prompt_micro_usd_per_1m, completion_micro_usd_per_1m, cached_micro_usd_per_1m, updated_at)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(model) DO UPDATE SET
			prompt_micro_usd_per_1m = excluded.prompt_micro_usd_per_1m,
			completion_micro_usd_per_1m = excluded.completion_micro_usd_per_1m,
			cached_micro_usd_per_1m = excluded.cached_micro_usd_per_1m,
			updated_at = excluded.updated_at
	`, key, price.Prompt, price.Completion, price.Cached, now)
	if err != nil {
		return fmt.Errorf("billing sqlite: upsert model price: %w", err)
	}
	return nil
}

func (s *SQLiteStore) DeleteModelPrice(ctx context.Context, model string) (bool, error) {
	if s == nil || s.db == nil {
		return false, fmt.Errorf("billing sqlite: not initialized")
	}
	key := policy.NormaliseModelKey(model)
	if key == "" {
		return false, fmt.Errorf("billing sqlite: model is required")
	}
	res, err := s.db.ExecContext(ctx, `DELETE FROM model_prices WHERE model = ?`, key)
	if err != nil {
		return false, fmt.Errorf("billing sqlite: delete model price: %w", err)
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

func (s *SQLiteStore) getSavedPriceMicro(ctx context.Context, modelKey string) (PriceMicroUSDPer1M, bool, int64, error) {
	if s == nil || s.db == nil {
		return PriceMicroUSDPer1M{}, false, 0, fmt.Errorf("billing sqlite: not initialized")
	}
	row := s.db.QueryRowContext(ctx, `
		SELECT prompt_micro_usd_per_1m, completion_micro_usd_per_1m, cached_micro_usd_per_1m, updated_at
		FROM model_prices
		WHERE model = ?
	`, modelKey)
	var p, c, cached, updated int64
	if err := row.Scan(&p, &c, &cached, &updated); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return PriceMicroUSDPer1M{}, false, 0, nil
		}
		return PriceMicroUSDPer1M{}, false, 0, fmt.Errorf("billing sqlite: query price: %w", err)
	}
	return PriceMicroUSDPer1M{Prompt: p, Completion: c, Cached: cached}, true, updated, nil
}

func (s *SQLiteStore) ResolvePriceMicro(ctx context.Context, model string) (price PriceMicroUSDPer1M, source string, updatedAt int64, err error) {
	modelKey := policy.NormaliseModelKey(model)
	if modelKey == "" {
		return PriceMicroUSDPer1M{}, "", 0, fmt.Errorf("billing sqlite: model is required")
	}
	baseKey := policy.StripThinkingVariant(modelKey)
	if s != nil && s.db != nil {
		if saved, ok, updated, errGet := s.getSavedPriceMicro(ctx, modelKey); errGet != nil {
			return PriceMicroUSDPer1M{}, "", 0, errGet
		} else if ok {
			return saved, "saved", updated, nil
		}
		if baseKey != "" && baseKey != modelKey {
			if saved, ok, updated, errGet := s.getSavedPriceMicro(ctx, baseKey); errGet != nil {
				return PriceMicroUSDPer1M{}, "", 0, errGet
			} else if ok {
				return saved, "saved", updated, nil
			}
		}
	}
	if v, ok := DefaultPrices[modelKey]; ok {
		return v, "default", 0, nil
	}
	if baseKey != "" && baseKey != modelKey {
		if v, ok := DefaultPrices[baseKey]; ok {
			return v, "default", 0, nil
		}
	}
	return PriceMicroUSDPer1M{}, "missing", 0, nil
}

func (s *SQLiteStore) ListModelPrices(ctx context.Context) ([]ModelPrice, error) {
	saved := map[string]ModelPrice{}
	if s != nil && s.db != nil {
		rows, err := s.db.QueryContext(ctx, `
			SELECT model, prompt_micro_usd_per_1m, completion_micro_usd_per_1m, cached_micro_usd_per_1m, updated_at
			FROM model_prices
			ORDER BY model ASC
		`)
		if err != nil {
			return nil, fmt.Errorf("billing sqlite: list model prices: %w", err)
		}
		defer rows.Close()
		for rows.Next() {
			var model string
			var p, c, cached, updated int64
			if err := rows.Scan(&model, &p, &c, &cached, &updated); err != nil {
				return nil, fmt.Errorf("billing sqlite: scan model price: %w", err)
			}
			saved[model] = ModelPrice{
				Model:              model,
				PromptUSDPer1M:     microUSDPer1MToUSDPer1M(p),
				CompletionUSDPer1M: microUSDPer1MToUSDPer1M(c),
				CachedUSDPer1M:     microUSDPer1MToUSDPer1M(cached),
				Source:             "saved",
				UpdatedAt:          updated,
			}
		}
		if err := rows.Err(); err != nil {
			return nil, fmt.Errorf("billing sqlite: list model prices rows: %w", err)
		}
	}

	merged := make([]ModelPrice, 0, len(DefaultPrices)+len(saved))
	for k, v := range DefaultPrices {
		if s, ok := saved[k]; ok {
			merged = append(merged, s)
			continue
		}
		merged = append(merged, ModelPrice{
			Model:              k,
			PromptUSDPer1M:     microUSDPer1MToUSDPer1M(v.Prompt),
			CompletionUSDPer1M: microUSDPer1MToUSDPer1M(v.Completion),
			CachedUSDPer1M:     microUSDPer1MToUSDPer1M(v.Cached),
			Source:             "default",
		})
	}
	for k, v := range saved {
		if _, ok := DefaultPrices[k]; ok {
			continue
		}
		merged = append(merged, v)
	}
	sort.Slice(merged, func(i, j int) bool { return merged[i].Model < merged[j].Model })
	return merged, nil
}

func (s *SQLiteStore) AddUsage(ctx context.Context, apiKey, model, dayKey string, delta DailyUsageRow) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("billing sqlite: not initialized")
	}
	apiKey = strings.TrimSpace(apiKey)
	modelKey := policy.NormaliseModelKey(model)
	dayKey = strings.TrimSpace(dayKey)
	if apiKey == "" || modelKey == "" || dayKey == "" {
		return fmt.Errorf("billing sqlite: invalid inputs")
	}
	if delta.Requests < 0 || delta.FailedRequests < 0 {
		return fmt.Errorf("billing sqlite: invalid request deltas")
	}

	now := nowUnixUTC()
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO api_key_model_daily_usage (
			api_key, model, day,
			requests, failed_requests,
			input_tokens, output_tokens, reasoning_tokens, cached_tokens, total_tokens,
			cost_micro_usd, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(api_key, model, day) DO UPDATE SET
			requests = requests + excluded.requests,
			failed_requests = failed_requests + excluded.failed_requests,
			input_tokens = input_tokens + excluded.input_tokens,
			output_tokens = output_tokens + excluded.output_tokens,
			reasoning_tokens = reasoning_tokens + excluded.reasoning_tokens,
			cached_tokens = cached_tokens + excluded.cached_tokens,
			total_tokens = total_tokens + excluded.total_tokens,
			cost_micro_usd = cost_micro_usd + excluded.cost_micro_usd,
			updated_at = excluded.updated_at
	`, apiKey, modelKey, dayKey,
		max64(0, delta.Requests), max64(0, delta.FailedRequests),
		max64(0, delta.InputTokens), max64(0, delta.OutputTokens), max64(0, delta.ReasoningTokens), max64(0, delta.CachedTokens), max64(0, delta.TotalTokens),
		max64(0, delta.CostMicroUSD), now,
	)
	if err != nil {
		return fmt.Errorf("billing sqlite: add usage: %w", err)
	}
	return nil
}

func (s *SQLiteStore) GetDailyCostMicroUSD(ctx context.Context, apiKey, dayKey string) (int64, error) {
	if s == nil || s.db == nil {
		return 0, fmt.Errorf("billing sqlite: not initialized")
	}
	apiKey = strings.TrimSpace(apiKey)
	dayKey = strings.TrimSpace(dayKey)
	if apiKey == "" || dayKey == "" {
		return 0, fmt.Errorf("billing sqlite: invalid inputs")
	}
	row := s.db.QueryRowContext(ctx, `
		SELECT COALESCE(SUM(cost_micro_usd), 0)
		FROM api_key_model_daily_usage
		WHERE api_key = ? AND day = ?
	`, apiKey, dayKey)
	var total int64
	if err := row.Scan(&total); err != nil {
		return 0, fmt.Errorf("billing sqlite: daily cost: %w", err)
	}
	return total, nil
}

func (s *SQLiteStore) GetDailyUsageReport(ctx context.Context, apiKey, dayKey string) (DailyUsageReport, error) {
	report := DailyUsageReport{
		APIKey:          strings.TrimSpace(apiKey),
		Day:             strings.TrimSpace(dayKey),
		GeneratedAtUnix: nowUnixUTC(),
	}
	if report.APIKey == "" || report.Day == "" {
		return report, fmt.Errorf("billing sqlite: api_key and day are required")
	}
	if s == nil || s.db == nil {
		return report, fmt.Errorf("billing sqlite: not initialized")
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT
			api_key, model, day,
			requests, failed_requests,
			input_tokens, output_tokens, reasoning_tokens, cached_tokens, total_tokens,
			cost_micro_usd, updated_at
		FROM api_key_model_daily_usage
		WHERE api_key = ? AND day = ?
		ORDER BY model ASC
	`, report.APIKey, report.Day)
	if err != nil {
		return report, fmt.Errorf("billing sqlite: query daily usage: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var row DailyUsageRow
		if err := rows.Scan(
			&row.APIKey, &row.Model, &row.Day,
			&row.Requests, &row.FailedRequests,
			&row.InputTokens, &row.OutputTokens, &row.ReasoningTokens, &row.CachedTokens, &row.TotalTokens,
			&row.CostMicroUSD, &row.UpdatedAt,
		); err != nil {
			return report, fmt.Errorf("billing sqlite: scan daily usage: %w", err)
		}
		report.TotalCostMicro += row.CostMicroUSD
		report.TotalRequests += row.Requests
		report.TotalFailed += row.FailedRequests
		report.TotalTokens += row.TotalTokens
		report.Models = append(report.Models, row)
	}
	if err := rows.Err(); err != nil {
		return report, fmt.Errorf("billing sqlite: daily usage rows: %w", err)
	}
	report.TotalCostUSD = microUSDToUSD(report.TotalCostMicro)
	return report, nil
}

func max64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}
