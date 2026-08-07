package poller

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/Atrabilis/api-poller/internal/config"
	"github.com/Atrabilis/api-poller/internal/cursor"
)

func TestRunInputJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"io":{"ai":[{"aiIndex":0,"aiValueScaled":1.5}]}}`))
	}))
	defer server.Close()

	cfg := config.Config{
		Inputs: []config.Input{{
			Name: "json_input",
			Request: config.Request{
				Method:  "GET",
				URL:     server.URL,
				Timeout: 5 * time.Second,
			},
			Response: config.Response{
				Format:    "json",
				ItemsPath: "$.io.ai",
			},
			Metrics: []config.Metric{{
				Name:      "metric_json",
				ValuePath: "$.aiValueScaled",
				Where: []config.Condition{{
					Path:   "$.aiIndex",
					Equals: 0,
				}},
				Transform: config.Transform{
					Scale: 1,
				},
			}},
		}},
	}

	samples, _, err := Run(context.Background(), cfg, "")
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if len(samples) != 1 {
		t.Fatalf("len(samples) = %d, want 1", len(samples))
	}
	if samples[0].ValueDouble == nil || *samples[0].ValueDouble != 1.5 {
		t.Fatalf("value_double = %#v, want 1.5", samples[0].ValueDouble)
	}
}

func TestRunInputXML(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/xml")
		_, _ = w.Write([]byte(`<response><roll>53.2</roll><action>TRACK</action></response>`))
	}))
	defer server.Close()

	cfg := config.Config{
		Inputs: []config.Input{{
			Name: "xml_input",
			Request: config.Request{
				Method:  "GET",
				URL:     server.URL,
				Timeout: 5 * time.Second,
			},
			Response: config.Response{
				Format: "xml",
			},
			Metrics: []config.Metric{
				{
					Name:      "roll",
					ValuePath: "$.roll",
					Transform: config.Transform{
						Scale: 1,
					},
				},
				{
					Name:      "action",
					ValuePath: "$.action",
					Transform: config.Transform{
						Scale: 1,
					},
				},
			},
		}},
	}

	samples, _, err := Run(context.Background(), cfg, "")
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if len(samples) != 2 {
		t.Fatalf("len(samples) = %d, want 2", len(samples))
	}
	if samples[0].ValueDouble == nil || *samples[0].ValueDouble != 53.2 {
		t.Fatalf("roll value_double = %#v, want 53.2", samples[0].ValueDouble)
	}
	if samples[1].ValueText == nil || *samples[1].ValueText != "TRACK" {
		t.Fatalf("action value_text = %#v, want TRACK", samples[1].ValueText)
	}
}

func TestRunInputPOSTWithBody(t *testing.T) {
	const requestBody = `{"filters":[],"order":[{"col":"_id","type":"desc"}]}`

	var gotMethod, gotBody string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		raw, _ := io.ReadAll(r.Body)
		gotBody = string(raw)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"value":1.5}]}`))
	}))
	defer server.Close()

	cfg := config.Config{
		Inputs: []config.Input{{
			Name: "post_input",
			Request: config.Request{
				Method:  "POST",
				URL:     server.URL,
				Timeout: 5 * time.Second,
				Body:    requestBody,
			},
			Response: config.Response{
				Format:    "json",
				ItemsPath: "$.data",
			},
			Metrics: []config.Metric{{
				Name:      "metric_post",
				ValuePath: "$.value",
				Transform: config.Transform{
					Scale: 1,
				},
			}},
		}},
	}

	samples, _, err := Run(context.Background(), cfg, "")
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if gotMethod != http.MethodPost {
		t.Fatalf("method = %q, want POST", gotMethod)
	}
	if gotBody != requestBody {
		t.Fatalf("body = %q, want %q", gotBody, requestBody)
	}
	if len(samples) != 1 {
		t.Fatalf("len(samples) = %d, want 1", len(samples))
	}
	if samples[0].ValueDouble == nil || *samples[0].ValueDouble != 1.5 {
		t.Fatalf("value_double = %#v, want 1.5", samples[0].ValueDouble)
	}
}

func TestRunInputGETWithoutBodySendsNilBody(t *testing.T) {
	var contentLength int64 = -1
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		contentLength = r.ContentLength
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"io":{"ai":[{"aiIndex":0,"aiValueScaled":2.0}]}}`))
	}))
	defer server.Close()

	cfg := config.Config{
		Inputs: []config.Input{{
			Name: "json_input_no_body",
			Request: config.Request{
				Method:  "GET",
				URL:     server.URL,
				Timeout: 5 * time.Second,
			},
			Response: config.Response{
				Format:    "json",
				ItemsPath: "$.io.ai",
			},
			Metrics: []config.Metric{{
				Name:      "metric_json_no_body",
				ValuePath: "$.aiValueScaled",
				Transform: config.Transform{
					Scale: 1,
				},
			}},
		}},
	}

	if _, _, err := Run(context.Background(), cfg, ""); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if contentLength > 0 {
		t.Fatalf("content length = %d, want <= 0 (no body)", contentLength)
	}
}

func TestRunInputWithStateAdvancesCursor(t *testing.T) {
	cursorFile := filepath.Join(t.TempDir(), "input.cursor.json")

	var gotBodies []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		gotBodies = append(gotBodies, string(raw))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"_id":"100","value":1},{"_id":"278474498","value":2}]}`))
	}))
	defer server.Close()

	newCfg := func() config.Config {
		return config.Config{
			Inputs: []config.Input{{
				Name: "stateful_input",
				State: &config.State{
					File:       cursorFile,
					CursorPath: "$._id",
					// Placeholder and InitialValue left blank to also
					// exercise config.applyDefaults.
				},
				Request: config.Request{
					Method:  "POST",
					URL:     server.URL,
					Timeout: 5 * time.Second,
					Body:    `{"min_id":"__CURSOR__"}`,
				},
				Response: config.Response{
					Format:    "json",
					ItemsPath: "$.data",
				},
				Metrics: []config.Metric{{
					Name:      "value",
					ValuePath: "$.value",
					Transform: config.Transform{Scale: 1},
				}},
			}},
		}
	}

	loadCfg := func() config.Config {
		cfg := newCfg()
		cfg.Inputs[0].State.Placeholder = "__CURSOR__"
		cfg.Inputs[0].State.InitialValue = "0"
		return cfg
	}

	// First run: no cursor file yet, so the placeholder must fall back to
	// InitialValue, and the cursor must advance to the max _id seen.
	_, cursorUpdates, err := Run(context.Background(), loadCfg(), "")
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if len(cursorUpdates) != 1 {
		t.Fatalf("len(cursorUpdates) = %d, want 1", len(cursorUpdates))
	}
	if cursorUpdates[0].Value != "278474498" {
		t.Fatalf("cursor value = %q, want 278474498", cursorUpdates[0].Value)
	}
	if err := cursor.Write(cursorUpdates[0].File, cursorUpdates[0].Value); err != nil {
		t.Fatalf("cursor.Write returned error: %v", err)
	}
	if gotBodies[0] != `{"min_id":"0"}` {
		t.Fatalf("first request body = %q, want cursor substituted with initial value 0", gotBodies[0])
	}

	// Second run: cursor file now holds the persisted value, so the next
	// request must be built with it instead of the initial value.
	if _, _, err := Run(context.Background(), loadCfg(), ""); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if gotBodies[1] != `{"min_id":"278474498"}` {
		t.Fatalf("second request body = %q, want cursor substituted with persisted value", gotBodies[1])
	}
}

func TestRunInputExpandsEnvInHeaders(t *testing.T) {
	t.Setenv("TEST_BASIC_AUTH", "Basic YWRtaW46c29sdGVj")

	var gotAuth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"io":{"ai":[{"aiIndex":0,"aiValueScaled":1}]}}`))
	}))
	defer server.Close()

	cfg := config.Config{
		Inputs: []config.Input{{
			Name: "env_header_input",
			Request: config.Request{
				Method:  "GET",
				URL:     server.URL,
				Timeout: 5 * time.Second,
				Headers: map[string]string{
					"Authorization": "${TEST_BASIC_AUTH}",
				},
			},
			Response: config.Response{
				Format:    "json",
				ItemsPath: "$.io.ai",
			},
			Metrics: []config.Metric{{
				Name:      "metric",
				ValuePath: "$.aiValueScaled",
				Transform: config.Transform{Scale: 1},
			}},
		}},
	}

	if _, _, err := Run(context.Background(), cfg, ""); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if gotAuth != "Basic YWRtaW46c29sdGVj" {
		t.Fatalf("Authorization header = %q, want expanded env value", gotAuth)
	}
}
