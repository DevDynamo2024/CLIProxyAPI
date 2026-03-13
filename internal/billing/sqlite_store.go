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
	internalusage "github.com/router-for-me/CLIProxyAPI/v6/internal/usage"
	_ "modernc.org/sqlite"
)

// DailyCostReader is the minimal interface needed by request-time middleware.
type DailyCostReader interface {
	GetDailyCostMicroUSD(ctx context.Context, apiKey, dayKey string) (int64, error)
	GetCostMicroUSDByDayRange(ctx context.Context, apiKey, startDay, endDayExclusive string) (int64, error)
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
	if err := store.syncDefaultModelPrices(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := store.recalculateUsageCosts(ctx, ""); err != nil {
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
		`
		CREATE TABLE IF NOT EXISTS usage_events (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			requested_at INTEGER NOT NULL,
			api_key TEXT NOT NULL,
			source TEXT NOT NULL,
			auth_index TEXT NOT NULL,
			model TEXT NOT NULL,
			failed INTEGER NOT NULL,
			input_tokens INTEGER NOT NULL,
			output_tokens INTEGER NOT NULL,
			reasoning_tokens INTEGER NOT NULL,
			cached_tokens INTEGER NOT NULL,
			total_tokens INTEGER NOT NULL,
			cost_micro_usd INTEGER NOT NULL,
			updated_at INTEGER NOT NULL
		)
		`,
		`CREATE INDEX IF NOT EXISTS idx_api_key_model_daily_usage_api_day ON api_key_model_daily_usage (api_key, day)`,
		`CREATE INDEX IF NOT EXISTS idx_usage_events_requested_at ON usage_events (requested_at)`,
		`CREATE INDEX IF NOT EXISTS idx_usage_events_source_requested_at ON usage_events (source, requested_at)`,
		`CREATE INDEX IF NOT EXISTS idx_usage_events_auth_index_requested_at ON usage_events (auth_index, requested_at)`,
		`CREATE INDEX IF NOT EXISTS idx_usage_events_api_key_requested_at ON usage_events (api_key, requested_at)`,
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
	if err := s.recalculateUsageCosts(ctx, key); err != nil {
		return err
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
	if n > 0 {
		if err := s.recalculateUsageCosts(ctx, key); err != nil {
			return false, err
		}
	}
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

func (s *SQLiteStore) GetCostMicroUSDByDayRange(ctx context.Context, apiKey, startDay, endDayExclusive string) (int64, error) {
	if s == nil || s.db == nil {
		return 0, fmt.Errorf("billing sqlite: not initialized")
	}
	apiKey = strings.TrimSpace(apiKey)
	startDay = strings.TrimSpace(startDay)
	endDayExclusive = strings.TrimSpace(endDayExclusive)
	if apiKey == "" || startDay == "" || endDayExclusive == "" {
		return 0, fmt.Errorf("billing sqlite: invalid inputs")
	}
	row := s.db.QueryRowContext(ctx, `
		SELECT COALESCE(SUM(cost_micro_usd), 0)
		FROM api_key_model_daily_usage
		WHERE api_key = ? AND day >= ? AND day < ?
	`, apiKey, startDay, endDayExclusive)
	var total int64
	if err := row.Scan(&total); err != nil {
		return 0, fmt.Errorf("billing sqlite: range cost: %w", err)
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

func (s *SQLiteStore) AddUsageEvent(ctx context.Context, event UsageEventRow) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("billing sqlite: not initialized")
	}
	if event.RequestedAt <= 0 {
		return fmt.Errorf("billing sqlite: requested_at is required")
	}
	if strings.TrimSpace(event.APIKey) == "" {
		return fmt.Errorf("billing sqlite: api_key is required")
	}
	modelKey := policy.NormaliseModelKey(event.Model)
	if modelKey == "" {
		return fmt.Errorf("billing sqlite: model is required")
	}
	now := nowUnixUTC()
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO usage_events (
			requested_at, api_key, source, auth_index, model, failed,
			input_tokens, output_tokens, reasoning_tokens, cached_tokens, total_tokens,
			cost_micro_usd, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`,
		event.RequestedAt,
		strings.TrimSpace(event.APIKey),
		strings.TrimSpace(event.Source),
		strings.TrimSpace(event.AuthIndex),
		modelKey,
		boolToSQLiteInt(event.Failed),
		max64(0, event.InputTokens),
		max64(0, event.OutputTokens),
		max64(0, event.ReasoningTokens),
		max64(0, event.CachedTokens),
		max64(0, event.TotalTokens),
		max64(0, event.CostMicroUSD),
		now,
	)
	if err != nil {
		return fmt.Errorf("billing sqlite: add usage event: %w", err)
	}
	return nil
}

func (s *SQLiteStore) ListDailyUsageRows(ctx context.Context, startDay, endDay string) ([]DailyUsageRow, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("billing sqlite: not initialized")
	}

	query := `
		SELECT
			api_key, model, day,
			requests, failed_requests,
			input_tokens, output_tokens, reasoning_tokens, cached_tokens, total_tokens,
			cost_micro_usd, updated_at
		FROM api_key_model_daily_usage
	`
	conditions := make([]string, 0, 2)
	args := make([]any, 0, 2)
	if trimmed := strings.TrimSpace(startDay); trimmed != "" {
		conditions = append(conditions, "day >= ?")
		args = append(args, trimmed)
	}
	if trimmed := strings.TrimSpace(endDay); trimmed != "" {
		conditions = append(conditions, "day <= ?")
		args = append(args, trimmed)
	}
	if len(conditions) > 0 {
		query += " WHERE " + strings.Join(conditions, " AND ")
	}
	query += " ORDER BY day ASC, api_key ASC, model ASC"

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("billing sqlite: list daily usage rows: %w", err)
	}
	defer rows.Close()

	result := make([]DailyUsageRow, 0)
	for rows.Next() {
		var row DailyUsageRow
		if err := rows.Scan(
			&row.APIKey, &row.Model, &row.Day,
			&row.Requests, &row.FailedRequests,
			&row.InputTokens, &row.OutputTokens, &row.ReasoningTokens, &row.CachedTokens, &row.TotalTokens,
			&row.CostMicroUSD, &row.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("billing sqlite: scan daily usage row: %w", err)
		}
		result = append(result, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("billing sqlite: daily usage rows: %w", err)
	}
	return result, nil
}

func (s *SQLiteStore) ListUsageEvents(ctx context.Context, startAt, endAt time.Time) ([]UsageEventRow, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("billing sqlite: not initialized")
	}

	query := `
		SELECT
			id, requested_at, api_key, source, auth_index, model, failed,
			input_tokens, output_tokens, reasoning_tokens, cached_tokens, total_tokens,
			cost_micro_usd, updated_at
		FROM usage_events
	`
	conditions := make([]string, 0, 2)
	args := make([]any, 0, 2)
	if !startAt.IsZero() {
		conditions = append(conditions, "requested_at >= ?")
		args = append(args, startAt.Unix())
	}
	if !endAt.IsZero() {
		conditions = append(conditions, "requested_at <= ?")
		args = append(args, endAt.Unix())
	}
	if len(conditions) > 0 {
		query += " WHERE " + strings.Join(conditions, " AND ")
	}
	query += " ORDER BY requested_at ASC, id ASC"

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("billing sqlite: list usage events: %w", err)
	}
	defer rows.Close()

	result := make([]UsageEventRow, 0)
	for rows.Next() {
		var row UsageEventRow
		var failed int64
		if err := rows.Scan(
			&row.ID,
			&row.RequestedAt,
			&row.APIKey,
			&row.Source,
			&row.AuthIndex,
			&row.Model,
			&failed,
			&row.InputTokens,
			&row.OutputTokens,
			&row.ReasoningTokens,
			&row.CachedTokens,
			&row.TotalTokens,
			&row.CostMicroUSD,
			&row.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("billing sqlite: scan usage event: %w", err)
		}
		row.Failed = failed > 0
		result = append(result, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("billing sqlite: usage event rows: %w", err)
	}
	return result, nil
}

func (s *SQLiteStore) ListUsageEventAggregateRows(ctx context.Context) ([]UsageEventAggregateRow, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("billing sqlite: not initialized")
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT
			source,
			auth_index,
			COALESCE(SUM(CASE WHEN failed = 0 THEN 1 ELSE 0 END), 0) AS success_count,
			COALESCE(SUM(CASE WHEN failed != 0 THEN 1 ELSE 0 END), 0) AS failure_count
		FROM usage_events
		GROUP BY source, auth_index
		ORDER BY source ASC, auth_index ASC
	`)
	if err != nil {
		return nil, fmt.Errorf("billing sqlite: list usage event aggregates: %w", err)
	}
	defer rows.Close()

	result := make([]UsageEventAggregateRow, 0)
	for rows.Next() {
		var row UsageEventAggregateRow
		if err := rows.Scan(&row.Source, &row.AuthIndex, &row.SuccessCount, &row.FailureCount); err != nil {
			return nil, fmt.Errorf("billing sqlite: scan usage event aggregate: %w", err)
		}
		result = append(result, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("billing sqlite: usage event aggregate rows: %w", err)
	}
	return result, nil
}

func max64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}

func (s *SQLiteStore) syncDefaultModelPrices(ctx context.Context) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("billing sqlite: not initialized")
	}
	if len(DefaultPrices) == 0 {
		return nil
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("billing sqlite: begin default price sync: %w", err)
	}
	defer func() {
		_ = tx.Rollback()
	}()

	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO model_prices (
			model,
			prompt_micro_usd_per_1m,
			completion_micro_usd_per_1m,
			cached_micro_usd_per_1m,
			updated_at
		) VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(model) DO NOTHING
	`)
	if err != nil {
		return fmt.Errorf("billing sqlite: prepare default price sync: %w", err)
	}
	defer stmt.Close()

	now := nowUnixUTC()
	for model, price := range DefaultPrices {
		if _, err := stmt.ExecContext(ctx, model, price.Prompt, price.Completion, price.Cached, now); err != nil {
			return fmt.Errorf("billing sqlite: sync default price %q: %w", model, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("billing sqlite: commit default price sync: %w", err)
	}
	return nil
}

type usageCostRecalcRow struct {
	APIKey          string
	Model           string
	Day             string
	InputTokens     int64
	OutputTokens    int64
	ReasoningTokens int64
	CachedTokens    int64
}

func (s *SQLiteStore) BuildHistoricalUsageSnapshot(ctx context.Context, beforeDay string) (internalusage.StatisticsSnapshot, error) {
	snapshot := internalusage.StatisticsSnapshot{
		APIs:           map[string]internalusage.APISnapshot{},
		RequestsByDay:  map[string]int64{},
		RequestsByHour: map[string]int64{},
		TokensByDay:    map[string]int64{},
		TokensByHour:   map[string]int64{},
	}
	if s == nil || s.db == nil {
		return snapshot, fmt.Errorf("billing sqlite: not initialized")
	}

	query := `
		SELECT
			api_key,
			model,
			day,
			requests,
			failed_requests,
			input_tokens,
			output_tokens,
			reasoning_tokens,
			cached_tokens,
			total_tokens
		FROM api_key_model_daily_usage
	`
	args := []any{}
	if strings.TrimSpace(beforeDay) != "" {
		query += ` WHERE day < ?`
		args = append(args, strings.TrimSpace(beforeDay))
	}
	query += ` ORDER BY day ASC, api_key ASC, model ASC`

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return snapshot, fmt.Errorf("billing sqlite: query historical usage snapshot: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var (
			apiKey          string
			model           string
			day             string
			requests        int64
			failedRequests  int64
			inputTokens     int64
			outputTokens    int64
			reasoningTokens int64
			cachedTokens    int64
			totalTokens     int64
		)
		if err := rows.Scan(
			&apiKey,
			&model,
			&day,
			&requests,
			&failedRequests,
			&inputTokens,
			&outputTokens,
			&reasoningTokens,
			&cachedTokens,
			&totalTokens,
		); err != nil {
			return snapshot, fmt.Errorf("billing sqlite: scan historical usage snapshot: %w", err)
		}

		successCount := max64(requests-failedRequests, 0)
		detail := internalusage.RequestDetail{
			Timestamp: historicalDetailTimestamp(day),
			Source:    apiKey,
			Tokens: internalusage.TokenStats{
				InputTokens:     max64(0, inputTokens),
				OutputTokens:    max64(0, outputTokens),
				ReasoningTokens: max64(0, reasoningTokens),
				CachedTokens:    max64(0, cachedTokens),
				TotalTokens:     max64(0, totalTokens),
			},
			Failed:       requests > 0 && failedRequests >= requests,
			RequestCount: max64(0, requests),
			SuccessCount: successCount,
			FailureCount: max64(0, failedRequests),
		}

		apiSnapshot := snapshot.APIs[apiKey]
		if apiSnapshot.Models == nil {
			apiSnapshot.Models = map[string]internalusage.ModelSnapshot{}
		}
		apiSnapshot.TotalRequests += max64(0, requests)
		apiSnapshot.SuccessCount += successCount
		apiSnapshot.FailureCount += max64(0, failedRequests)
		apiSnapshot.TotalTokens += max64(0, totalTokens)

		modelSnapshot := apiSnapshot.Models[model]
		modelSnapshot.TotalRequests += max64(0, requests)
		modelSnapshot.SuccessCount += successCount
		modelSnapshot.FailureCount += max64(0, failedRequests)
		modelSnapshot.TotalTokens += max64(0, totalTokens)
		modelSnapshot.Details = append(modelSnapshot.Details, detail)
		apiSnapshot.Models[model] = modelSnapshot
		snapshot.APIs[apiKey] = apiSnapshot

		snapshot.TotalRequests += max64(0, requests)
		snapshot.SuccessCount += successCount
		snapshot.FailureCount += max64(0, failedRequests)
		snapshot.TotalTokens += max64(0, totalTokens)
		snapshot.RequestsByDay[day] += max64(0, requests)
		snapshot.TokensByDay[day] += max64(0, totalTokens)
	}
	if err := rows.Err(); err != nil {
		return snapshot, fmt.Errorf("billing sqlite: historical usage snapshot rows: %w", err)
	}

	return snapshot, nil
}

func historicalDetailTimestamp(day string) time.Time {
	loc := time.FixedZone("CST", 8*60*60)
	parsed, err := time.ParseInLocation("2006-01-02 15:04:05", strings.TrimSpace(day)+" 23:59:59", loc)
	if err != nil {
		return time.Now().In(loc)
	}
	return parsed
}

func (s *SQLiteStore) recalculateUsageCosts(ctx context.Context, model string) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("billing sqlite: not initialized")
	}

	modelKey := strings.TrimSpace(model)
	query := `
		SELECT api_key, model, day, input_tokens, output_tokens, reasoning_tokens, cached_tokens
		FROM api_key_model_daily_usage
	`
	args := []any{}
	if modelKey != "" {
		query += ` WHERE model = ?`
		args = append(args, modelKey)
	}

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("billing sqlite: query usage for cost repair: %w", err)
	}
	defer rows.Close()

	items := make([]usageCostRecalcRow, 0)
	models := map[string]struct{}{}
	for rows.Next() {
		var item usageCostRecalcRow
		if err := rows.Scan(
			&item.APIKey,
			&item.Model,
			&item.Day,
			&item.InputTokens,
			&item.OutputTokens,
			&item.ReasoningTokens,
			&item.CachedTokens,
		); err != nil {
			return fmt.Errorf("billing sqlite: scan usage for cost repair: %w", err)
		}
		items = append(items, item)
		models[item.Model] = struct{}{}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("billing sqlite: usage rows for cost repair: %w", err)
	}
	if len(items) == 0 {
		return nil
	}

	priceByModel := make(map[string]PriceMicroUSDPer1M, len(models))
	for modelKey := range models {
		price, _, _, err := s.ResolvePriceMicro(ctx, modelKey)
		if err != nil {
			return err
		}
		priceByModel[modelKey] = price
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("billing sqlite: begin cost repair: %w", err)
	}
	defer func() {
		_ = tx.Rollback()
	}()

	stmt, err := tx.PrepareContext(ctx, `
		UPDATE api_key_model_daily_usage
		SET cost_micro_usd = ?, updated_at = ?
		WHERE api_key = ? AND model = ? AND day = ?
	`)
	if err != nil {
		return fmt.Errorf("billing sqlite: prepare cost repair: %w", err)
	}
	defer stmt.Close()

	now := nowUnixUTC()
	for _, item := range items {
		price := priceByModel[item.Model]
		cost := calculateUsageCostMicro(item.InputTokens, item.OutputTokens, item.ReasoningTokens, item.CachedTokens, price)
		if _, err := stmt.ExecContext(ctx, cost, now, item.APIKey, item.Model, item.Day); err != nil {
			return fmt.Errorf("billing sqlite: update repaired cost: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("billing sqlite: commit cost repair: %w", err)
	}
	return nil
}
