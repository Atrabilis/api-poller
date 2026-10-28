package main

// End-to-end tests: build the real binary and run it the way the systemd timer
// does (-config <file> -write-only), against a fake HTTP device and a real
// PostgreSQL table with the layout of psda.api_samples, then check the rows.
// The inputs follow the production configs soltec_atamo_*.yml (Soltec XML
// status with basic auth from the environment) and minimodules.yml (Moxa
// E1240 JSON with items_path, where and transform).
//
// They need a PostgreSQL server and are skipped unless API_POLLER_E2E_PG is set
// to a connection URL, e.g. postgres://postgres:postgres@127.0.0.1:5432/postgres
// (CI provides one as a service container). If API_POLLER_E2E_COVERDIR is set,
// the binary is built with -cover and writes its coverage data there.

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

// Same layout as psda.api_samples on vps_01 (2026-10-27).
const apiSamplesDDL = `CREATE TABLE %s (
  ts TIMESTAMPTZ NOT NULL,
  input_name TEXT NOT NULL,
  metric_name TEXT NOT NULL,
  value_double DOUBLE PRECISION,
  value_text TEXT,
  value_bool BOOLEAN,
  value_json JSONB,
  value_type TEXT NOT NULL,
  labels JSONB NOT NULL DEFAULT '{}'::jsonb,
  labels_key TEXT NOT NULL DEFAULT '',
  source_url TEXT NOT NULL,
  PRIMARY KEY (ts, input_name, metric_name, source_url, labels_key)
)`

const soltecAuth = "Basic YWRtaW46c29sdGVj"

// fakeDevices serves a Soltec gateway status page (basic auth required) and a
// Moxa E1240 analog input list.
func fakeDevices(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/status.xml", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != soltecAuth {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "text/xml")
		_, _ = fmt.Fprintf(w, `<response><roll>12.5</roll><pitch>-0.4</pitch><action>TRACK</action><batt_charge>87</batt_charge><dev>%s</dev></response>`, r.URL.Query().Get("dev"))
	})
	mux.HandleFunc("/api/slot/0/io/ai", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"io":{"ai":[{"aiIndex":0,"aiValueScaled":5.2},{"aiIndex":1,"aiValueScaled":31.5},{"aiIndex":2,"aiValueScaled":0}]}}`)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

// closedURL is an address where nothing listens, like a device that is off.
func closedURL(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	_ = ln.Close()
	return "http://" + addr
}

type apiE2E struct {
	conn  *pgx.Conn
	bin   string
	table string
	env   []string
}

func setupAPIE2E(t *testing.T) *apiE2E {
	t.Helper()
	dsn := os.Getenv("API_POLLER_E2E_PG")
	if dsn == "" {
		t.Skip("API_POLLER_E2E_PG not set; skipping end-to-end test")
	}
	u, err := url.Parse(dsn)
	if err != nil {
		t.Fatalf("parse API_POLLER_E2E_PG: %v", err)
	}
	password, _ := u.User.Password()
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close(context.Background()) })

	schema := fmt.Sprintf("e2e_%d", time.Now().UnixNano())
	if _, err := conn.Exec(ctx, "CREATE SCHEMA "+schema); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = conn.Exec(context.Background(), "DROP SCHEMA "+schema+" CASCADE") })
	table := schema + ".api_samples"
	if _, err := conn.Exec(ctx, fmt.Sprintf(apiSamplesDDL, table)); err != nil {
		t.Fatal(err)
	}

	bin := filepath.Join(t.TempDir(), "api-poller")
	args := []string{"build", "-o", bin}
	if os.Getenv("API_POLLER_E2E_COVERDIR") != "" {
		args = append(args, "-cover", "-coverpkg=github.com/Atrabilis/api-poller/...")
	}
	if out, err := exec.Command("go", append(args, ".")...).CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}

	env := append(os.Environ(),
		"E2E_PG_HOST="+u.Hostname(),
		"E2E_PG_PORT="+u.Port(),
		"E2E_PG_USER="+u.User.Username(),
		"E2E_PG_PASSWORD="+password,
		"E2E_PG_DB="+strings.TrimPrefix(u.Path, "/"),
		"SOLTEC_BASIC_AUTH="+soltecAuth,
	)
	if dir := os.Getenv("API_POLLER_E2E_COVERDIR"); dir != "" {
		env = append(env, "GOCOVERDIR="+dir)
	}
	return &apiE2E{conn: conn, bin: bin, table: table, env: env}
}

func (e *apiE2E) config(t *testing.T, inputs string) string {
	t.Helper()
	schema, table, _ := strings.Cut(e.table, ".")
	cfg := fmt.Sprintf(`storage:
  outputs:
    - name: psda_timescale
      type: timescaledb
      enabled: true
      timescaledb:
        host_env: E2E_PG_HOST
        port_env: E2E_PG_PORT
        user_env: E2E_PG_USER
        password_env: E2E_PG_PASSWORD
        database_env: E2E_PG_DB
        schema: %s
        table: %s
        sslmode: disable
inputs:
%s`, schema, table, inputs)
	path := filepath.Join(t.TempDir(), "config.yml")
	if err := os.WriteFile(path, []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func soltecInput(name, baseURL string) string {
	return fmt.Sprintf(`  - name: %s
    request:
      method: GET
      url: %s/status.xml?dev=1
      timeout: 2s
      headers:
        Authorization: "${SOLTEC_BASIC_AUTH}"
    response:
      format: xml
    metrics:
      - name: "roll(deg)"
        value_path: "$.roll"
        labels: {plant: psda, device: soltec_atamo_1_tracker_1, unit: deg}
      - name: "pitch(deg)"
        value_path: "$.pitch"
        labels: {plant: psda, device: soltec_atamo_1_tracker_1, unit: deg}
      - name: action
        value_path: "$.action"
        labels: {plant: psda, device: soltec_atamo_1_tracker_1}
`, name, baseURL)
}

func moxaInput(name, baseURL string) string {
	return fmt.Sprintf(`  - name: %s
    request:
      method: GET
      url: %s/api/slot/0/io/ai
      timeout: 2s
      headers:
        Accept: "vdn.dac.v1"
    response:
      format: json
      items_path: "$.io.ai"
    metrics:
      - name: "1RC201(w.m-2)"
        value_path: "$.aiValueScaled"
        where:
          - path: "$.aiIndex"
            equals: 0
        transform: {scale: 150.0, offset: 0.0}
        labels: {plant: psda, device: 1MX201, unit: w.m-2}
      - name: "1RC201(C)"
        value_path: "$.aiValueScaled"
        where:
          - path: "$.aiIndex"
            equals: 1
        labels: {plant: psda, device: 1MX201, unit: C}
`, name, baseURL)
}

func (e *apiE2E) run(t *testing.T, configPath string) (string, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, e.bin, "-config", configPath, "-write-only")
	cmd.Env = e.env
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func TestE2EWritesSoltecAndMoxaSamples(t *testing.T) {
	e := setupAPIE2E(t)
	devices := fakeDevices(t)
	configPath := e.config(t, soltecInput("psda_soltec_atamo_1_dev_1", devices.URL)+moxaInput("psda_1mx201", devices.URL))

	if out, err := e.run(t, configPath); err != nil {
		t.Fatalf("api-poller failed: %v\n%s", err, out)
	}

	type sample struct {
		valueDouble *float64
		valueText   *string
		valueType   string
		device      string
		sourceURL   string
	}
	got := map[string]sample{}
	rows, err := e.conn.Query(context.Background(), "SELECT metric_name, value_double, value_text, value_type, labels->>'device', source_url FROM "+e.table)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var name string
		var s sample
		if err := rows.Scan(&name, &s.valueDouble, &s.valueText, &s.valueType, &s.device, &s.sourceURL); err != nil {
			t.Fatal(err)
		}
		got[name] = s
	}
	rows.Close()
	if len(got) != 5 {
		t.Fatalf("expected 5 metrics (3 Soltec, 2 Moxa), got %d: %v", len(got), got)
	}

	numbers := map[string]float64{
		"roll(deg)":     12.5,
		"pitch(deg)":    -0.4,
		"1RC201(w.m-2)": 5.2 * 150, // scale applied
		"1RC201(C)":     31.5,
	}
	for name, want := range numbers {
		s := got[name]
		if s.valueDouble == nil {
			t.Errorf("%s has no numeric value, want %v", name, want)
		} else if *s.valueDouble-want > 1e-9 || want-*s.valueDouble > 1e-9 {
			t.Errorf("%s = %v, want %v", name, *s.valueDouble, want)
		}
	}
	if a := got["action"]; a.valueText == nil || *a.valueText != "TRACK" || a.valueDouble != nil {
		t.Errorf("action should be stored as text TRACK, got %+v", a)
	}
	if got["roll(deg)"].device != "soltec_atamo_1_tracker_1" || !strings.HasSuffix(got["roll(deg)"].sourceURL, "/status.xml?dev=1") {
		t.Errorf("unexpected labels or source_url: %+v", got["roll(deg)"])
	}

	// Each run is a new observation (ts is taken per input when it is
	// polled): a second run adds a new set of rows, one new ts per input.
	if out, err := e.run(t, configPath); err != nil {
		t.Fatalf("second run failed: %v\n%s", err, out)
	}
	var n, inputs, perInputMax, perInputMin int
	if err := e.conn.QueryRow(context.Background(), `SELECT sum(rows), count(*), max(ts_count), min(ts_count) FROM (
		SELECT input_name, count(*) AS rows, count(DISTINCT ts) AS ts_count FROM `+e.table+` GROUP BY input_name) x`).Scan(&n, &inputs, &perInputMax, &perInputMin); err != nil {
		t.Fatal(err)
	}
	if n != 10 || inputs != 2 || perInputMax != 2 || perInputMin != 2 {
		t.Fatalf("expected 10 rows, 2 inputs with 2 timestamps each; got %d rows, %d inputs, %d..%d timestamps", n, inputs, perInputMin, perInputMax)
	}
}

func TestE2EMissingAuthIsRejectedByTheDevice(t *testing.T) {
	e := setupAPIE2E(t)
	devices := fakeDevices(t)
	configPath := e.config(t, soltecInput("psda_soltec_atamo_1_dev_1", devices.URL))

	env := []string{}
	for _, kv := range e.env {
		if !strings.HasPrefix(kv, "SOLTEC_BASIC_AUTH=") {
			env = append(env, kv)
		}
	}
	e.env = env
	out, err := e.run(t, configPath)
	if err == nil {
		t.Fatalf("expected the run to fail without credentials\n%s", out)
	}
	if !strings.Contains(out, "401") {
		t.Fatalf("expected the HTTP 401 to be reported\n%s", out)
	}
}

// One unreachable device must not lose the devices that answered in the same
// config: they are written, the failed input is named, and the run still
// exits non-zero so systemd marks it as failed.
func TestE2EOneUnreachableDeviceKeepsTheOthers(t *testing.T) {
	e := setupAPIE2E(t)
	devices := fakeDevices(t)
	configPath := e.config(t, moxaInput("psda_2mx401", closedURL(t))+moxaInput("psda_2mx402", devices.URL))

	out, err := e.run(t, configPath)
	if err == nil {
		t.Fatalf("expected a non-zero exit when a device is unreachable\n%s", out)
	}
	if !strings.Contains(out, `run poller: input "psda_2mx401"`) || strings.Contains(out, `input "psda_2mx402":`) {
		t.Fatalf("expected only the failing input to be reported\n%s", out)
	}
	var n int
	if err := e.conn.QueryRow(context.Background(), "SELECT count(*) FROM "+e.table+" WHERE input_name = 'psda_2mx402'").Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("expected the 2 metrics of the device that answered, got %d rows\n%s", n, out)
	}
}
