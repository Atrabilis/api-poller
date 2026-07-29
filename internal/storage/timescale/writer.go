package timescale

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"regexp"
	"sort"
	"strings"

	"github.com/Atrabilis/api-poller/internal/poller"
	"github.com/jackc/pgx/v5/pgxpool"
)

const backendType = "timescaledb"

var nonIdentChars = regexp.MustCompile(`[^a-z0-9_]+`)
var multiUnderscore = regexp.MustCompile(`_+`)

type Writer struct {
	name      string
	available bool
	schema    string
	table     string
	fqn       string
	pool      *pgxpool.Pool
}

func NewWriter(name string, cfg Config) (*Writer, error) {
	if err := cfg.Validate(name); err != nil {
		return nil, err
	}

	host := strings.TrimSpace(os.Getenv(cfg.HostEnv))
	port := strings.TrimSpace(os.Getenv(cfg.PortEnv))
	user := strings.TrimSpace(os.Getenv(cfg.UserEnv))
	password := os.Getenv(cfg.PasswordEnv)
	database := strings.TrimSpace(os.Getenv(cfg.DatabaseEnv))
	sslmode := strings.TrimSpace(cfg.SSLMode)
	if sslmode == "" {
		sslmode = "disable"
	}

	if host == "" {
		return nil, fmt.Errorf("env %s is empty", cfg.HostEnv)
	}
	if port == "" {
		return nil, fmt.Errorf("env %s is empty", cfg.PortEnv)
	}
	if user == "" {
		return nil, fmt.Errorf("env %s is empty", cfg.UserEnv)
	}
	if password == "" {
		return nil, fmt.Errorf("env %s is empty", cfg.PasswordEnv)
	}
	if database == "" {
		return nil, fmt.Errorf("env %s is empty", cfg.DatabaseEnv)
	}

	schema := sanitizeIdentifier(cfg.Schema)
	table := sanitizeIdentifier(cfg.Table)
	connString := fmt.Sprintf(
		"postgres://%s:%s@%s:%s/%s?sslmode=%s",
		url.QueryEscape(user),
		url.QueryEscape(password),
		host,
		port,
		url.QueryEscape(database),
		url.QueryEscape(sslmode),
	)

	pool, err := pgxpool.New(context.Background(), connString)
	if err != nil {
		return nil, fmt.Errorf("creating postgres pool for output %q: %w", name, err)
	}

	w := &Writer{
		name:   name,
		schema: schema,
		table:  table,
		fqn:    schema + "." + table,
		pool:   pool,
	}

	if err := pool.Ping(context.Background()); err != nil {
		fmt.Printf("WARNING: storage output %q (type=%s) is not reachable: %v\n", name, backendType, err)
		w.available = false
		return w, nil
	}

	if err := w.ensureTable(context.Background()); err != nil {
		return nil, fmt.Errorf("ensure table %s for output %q: %w", w.fqn, name, err)
	}

	w.available = true
	fmt.Printf("Storage output %q (type=%s) is reachable and ready at %s\n", name, backendType, w.fqn)
	return w, nil
}

func (w *Writer) Name() string {
	if w == nil {
		return ""
	}
	return w.name
}

func (w *Writer) Available() bool {
	if w == nil {
		return false
	}
	return w.available
}

func (w *Writer) WriteSamples(ctx context.Context, samples []poller.Sample) error {
	if w == nil || !w.available || w.pool == nil || len(samples) == 0 {
		return nil
	}

	tx, err := w.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	query := fmt.Sprintf(`
		INSERT INTO %s (
			observed_at,
			input_name,
			metric_name,
			value_double,
			value_text,
			value_bool,
			value_json,
			value_type,
			labels,
			labels_key,
			source_url
		) VALUES ($1, $2, $3, $4, $5, $6, $7::jsonb, $8, $9::jsonb, $10, $11)
		ON CONFLICT (observed_at, input_name, metric_name, source_url, labels_key)
		DO UPDATE SET
			value_double = EXCLUDED.value_double,
			value_text = EXCLUDED.value_text,
			value_bool = EXCLUDED.value_bool,
			value_json = EXCLUDED.value_json,
			value_type = EXCLUDED.value_type,
			labels = EXCLUDED.labels
	`, w.fqn)

	for _, sample := range samples {
		labelsJSON, labelsKey, err := encodeLabels(sample.Labels)
		if err != nil {
			return fmt.Errorf("encode labels for metric %q: %w", sample.MetricName, err)
		}

		if _, err := tx.Exec(
			ctx,
			query,
			sample.ObservedAt.UTC(),
			sample.InputName,
			sample.MetricName,
			sample.ValueDouble,
			sample.ValueText,
			sample.ValueBool,
			jsonValueOrNull(sample.ValueJSON),
			sample.ValueType,
			string(labelsJSON),
			labelsKey,
			sample.SourceURL,
		); err != nil {
			return fmt.Errorf("insert metric %q: %w", sample.MetricName, err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit tx: %w", err)
	}
	return nil
}

func (w *Writer) Close() {
	if w == nil {
		return
	}
	if w.pool != nil {
		w.pool.Close()
	}
}

func (w *Writer) ensureTable(ctx context.Context) error {
	if _, err := w.pool.Exec(ctx, fmt.Sprintf(`CREATE SCHEMA IF NOT EXISTS %s`, w.schema)); err != nil {
		return err
	}

	ddl := fmt.Sprintf(`
		CREATE TABLE IF NOT EXISTS %s (
			observed_at timestamptz NOT NULL,
			input_name text NOT NULL,
			metric_name text NOT NULL,
			value_double double precision NULL,
			value_text text NULL,
			value_bool boolean NULL,
			value_json jsonb NULL,
			value_type text NOT NULL,
			labels jsonb NOT NULL DEFAULT '{}'::jsonb,
			labels_key text NOT NULL DEFAULT '',
			source_url text NOT NULL,
			PRIMARY KEY (observed_at, input_name, metric_name, source_url, labels_key)
		)
	`, w.fqn)
	if _, err := w.pool.Exec(ctx, ddl); err != nil {
		return err
	}

	if _, err := w.pool.Exec(ctx, fmt.Sprintf(`CREATE INDEX IF NOT EXISTS %s_metric_time_idx ON %s (metric_name, observed_at DESC)`, w.table, w.fqn)); err != nil {
		return err
	}
	if _, err := w.pool.Exec(ctx, fmt.Sprintf(`CREATE INDEX IF NOT EXISTS %s_input_time_idx ON %s (input_name, observed_at DESC)`, w.table, w.fqn)); err != nil {
		return err
	}
	if _, err := w.pool.Exec(ctx, fmt.Sprintf(`CREATE INDEX IF NOT EXISTS %s_labels_gin_idx ON %s USING gin (labels)`, w.table, w.fqn)); err != nil {
		return err
	}
	_, _ = w.pool.Exec(ctx, fmt.Sprintf(`SELECT create_hypertable('%s', 'observed_at', if_not_exists => true)`, w.fqn))
	return nil
}

func jsonValueOrNull(value json.RawMessage) string {
	if len(value) == 0 {
		return "null"
	}
	return string(value)
}

func encodeLabels(labels map[string]string) ([]byte, string, error) {
	if len(labels) == 0 {
		return []byte("{}"), "", nil
	}

	ordered := make(map[string]string, len(labels))
	keys := make([]string, 0, len(labels))
	for key := range labels {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		ordered[key] = labels[key]
	}

	payload, err := json.Marshal(ordered)
	if err != nil {
		return nil, "", err
	}

	sum := sha1.Sum(payload)
	return payload, hex.EncodeToString(sum[:]), nil
}

func sanitizeIdentifier(value string) string {
	lower := strings.ToLower(strings.TrimSpace(value))
	lower = nonIdentChars.ReplaceAllString(lower, "_")
	lower = multiUnderscore.ReplaceAllString(lower, "_")
	lower = strings.Trim(lower, "_")
	if lower == "" {
		return "telemetry"
	}
	return lower
}
