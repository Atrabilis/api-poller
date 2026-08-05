package poller

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Atrabilis/api-poller/internal/config"
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

	samples, err := Run(context.Background(), cfg, "")
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

	samples, err := Run(context.Background(), cfg, "")
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

	samples, err := Run(context.Background(), cfg, "")
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

	if _, err := Run(context.Background(), cfg, ""); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if contentLength > 0 {
		t.Fatalf("content length = %d, want <= 0 (no body)", contentLength)
	}
}
